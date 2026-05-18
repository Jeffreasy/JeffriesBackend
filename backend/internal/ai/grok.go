package ai

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"time"

	"github.com/sony/gobreaker"
)

const (
	GrokAPIURL    = "https://api.x.ai/v1/chat/completions"
	GrokModel     = "grok-4-1-fast"
	MaxToolRounds = 5
	MaxTokens     = 2500
	Temperature   = 0.3
)

// GrokClient handles communication with the xAI Grok API.
type GrokClient struct {
	apiKey     string
	httpClient *http.Client
	cb         *gobreaker.CircuitBreaker
}

func NewGrokClient(apiKey string) *GrokClient {
	// Configure Circuit Breaker: 3 consecutive failures -> open state for 30s
	st := gobreaker.Settings{
		Name:        "GrokAPI",
		MaxRequests: 1, // When half-open, allow 1 request to test
		Interval:    0,
		Timeout:     30 * time.Second, // Time before transitioning from Open to Half-Open
		ReadyToTrip: func(counts gobreaker.Counts) bool {
			return counts.ConsecutiveFailures >= 3
		},
		OnStateChange: func(name string, from gobreaker.State, to gobreaker.State) {
			slog.Warn("CircuitBreaker state changed", "name", name, "from", from.String(), "to", to.String())
		},
	}

	return &GrokClient{
		apiKey:     apiKey,
		httpClient: &http.Client{Timeout: 60 * time.Second},
		cb:         gobreaker.NewCircuitBreaker(st),
	}
}

// Message represents a chat message in the Grok API format.
type Message struct {
	Role       string     `json:"role"`
	Content    *string    `json:"content,omitempty"`
	ToolCalls  []ToolCall `json:"tool_calls,omitempty"`
	ToolCallID string     `json:"tool_call_id,omitempty"`
}

type ToolCall struct {
	ID       string       `json:"id"`
	Type     string       `json:"type"`
	Function FunctionCall `json:"function"`
}

type FunctionCall struct {
	Name      string `json:"name"`
	Arguments string `json:"arguments"`
}

type GrokResponse struct {
	Choices []Choice `json:"choices"`
	Usage   Usage    `json:"usage"`
}

type Choice struct {
	Message      Message `json:"message"`
	FinishReason string  `json:"finish_reason"`
}

type Usage struct {
	PromptTokens     int `json:"prompt_tokens"`
	CompletionTokens int `json:"completion_tokens"`
	TotalTokens      int `json:"total_tokens"`
}

// ChatResult is the final result of a chat interaction.
type ChatResult struct {
	OK       bool    `json:"ok"`
	Agent    *Agent  `json:"agent,omitempty"`
	Antwoord string  `json:"antwoord,omitempty"`
	Error    string  `json:"error,omitempty"`
	Tokens   *Usage  `json:"tokens,omitempty"`
}

// Chat runs a full chat interaction with tool calling.
func (c *GrokClient) Chat(
	ctx context.Context,
	systemPrompt string,
	userMessage string,
	history []Message,
	tools []ToolDefinition,
	executor ToolExecutor,
) ChatResult {
	start := time.Now()

	messages := make([]Message, 0, len(history)+3)
	messages = append(messages, Message{Role: "system", Content: strPtr(systemPrompt)})
	for _, m := range history {
		messages = append(messages, m)
	}
	messages = append(messages, Message{Role: "user", Content: strPtr(userMessage)})

	var totalUsage *Usage
	var toolsUsed []string

	for round := 0; round < MaxToolRounds; round++ {
		reqBody := map[string]any{
			"model":       GrokModel,
			"messages":    messages,
			"stream":      false,
			"temperature": Temperature,
			"max_tokens":  MaxTokens,
		}
		if len(tools) > 0 {
			reqBody["tools"] = tools
		}

		data, err := json.Marshal(reqBody)
		if err != nil {
			return ChatResult{Error: fmt.Sprintf("marshal error: %v", err)}
		}

		req, err := http.NewRequestWithContext(ctx, http.MethodPost, GrokAPIURL, bytes.NewReader(data))
		if err != nil {
			return ChatResult{Error: fmt.Sprintf("request error: %v", err)}
		}
		req.Header.Set("Content-Type", "application/json")
		req.Header.Set("Authorization", "Bearer "+c.apiKey)

		// Execute request through Circuit Breaker
		respInterface, cbErr := c.cb.Execute(func() (interface{}, error) {
			resp, err := c.httpClient.Do(req)
			if err != nil {
				return nil, err
			}
			if resp.StatusCode >= 500 { // Only trip on server errors, not 4xx client errors
				defer resp.Body.Close()
				return nil, fmt.Errorf("server error: %d", resp.StatusCode)
			}
			return resp, nil
		})

		if cbErr != nil {
			// Check if Circuit Breaker is open
			if cbErr == gobreaker.ErrOpenState {
				return ChatResult{Error: "De AI server is tijdelijk onbereikbaar wegens overbelasting. Probeer het later opnieuw."}
			}
			return ChatResult{Error: fmt.Sprintf("API error: %v", cbErr)}
		}

		resp := respInterface.(*http.Response)
		body, _ := io.ReadAll(resp.Body)
		resp.Body.Close()

		if resp.StatusCode != http.StatusOK {
			return ChatResult{Error: fmt.Sprintf("Grok %d: %s", resp.StatusCode, truncate(string(body), 200))}
		}

		var grokResp GrokResponse
		if err := json.Unmarshal(body, &grokResp); err != nil {
			return ChatResult{Error: fmt.Sprintf("parse error: %v", err)}
		}
		totalUsage = &grokResp.Usage

		if len(grokResp.Choices) == 0 {
			return ChatResult{Error: "Geen response van Grok"}
		}

		choice := grokResp.Choices[0]
		msg := choice.Message
		messages = append(messages, msg)

		// No tool calls → return final answer
		if choice.FinishReason != "tool_calls" || len(msg.ToolCalls) == 0 {
			duration := time.Since(start)
			slog.Info("[Grok] OK",
				"duration", duration.Round(time.Millisecond),
				"rounds", round+1,
				"tools", toolsUsed,
				"tokens", totalUsage.TotalTokens,
			)
			content := ""
			if msg.Content != nil {
				content = *msg.Content
			}
			return ChatResult{
				OK:       true,
				Antwoord: content,
				Tokens:   totalUsage,
			}
		}

		// Execute tool calls
		for _, tc := range msg.ToolCalls {
			toolStart := time.Now()
			result := executor.Execute(ctx, tc.Function.Name, tc.Function.Arguments)
			toolDur := time.Since(toolStart)
			toolsUsed = append(toolsUsed, fmt.Sprintf("%s(%dms)", tc.Function.Name, toolDur.Milliseconds()))

			messages = append(messages, Message{
				Role:       "tool",
				Content:    strPtr(result),
				ToolCallID: tc.ID,
			})
		}
	}

	// Max rounds reached
	duration := time.Since(start)
	slog.Warn("[Grok] MAX_ROUNDS",
		"duration", duration.Round(time.Millisecond),
		"tools", toolsUsed,
		"tokens", totalUsage.TotalTokens,
	)

	last := messages[len(messages)-1]
	content := "Ik heb te veel data moeten ophalen. Probeer een specifiekere vraag."
	if last.Content != nil {
		content = *last.Content
	}
	return ChatResult{
		OK:       true,
		Antwoord: content,
		Tokens:   totalUsage,
	}
}

// ToolExecutor is called by the chat loop to execute tool calls.
type ToolExecutor interface {
	Execute(ctx context.Context, toolName string, argsJSON string) string
}

// ToolDefinition is the OpenAI-compatible function definition for Grok.
type ToolDefinition struct {
	Type     string         `json:"type"`
	Function ToolFunction   `json:"function"`
}

type ToolFunction struct {
	Name        string          `json:"name"`
	Description string          `json:"description"`
	Parameters  json.RawMessage `json:"parameters"`
}

func strPtr(s string) *string { return &s }

func truncate(s string, max int) string {
	if len(s) <= max {
		return s
	}
	return s[:max] + "..."
}

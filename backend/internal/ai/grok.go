package ai

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"strings"
	"time"

	"github.com/sony/gobreaker"
)

const (
	GrokChatAPIURL         = "https://api.x.ai/v1/chat/completions"
	GrokResponsesAPIURL    = "https://api.x.ai/v1/responses"
	DefaultGrokModel       = "grok-4.3"
	DefaultReasoningEffort = "low"
	MaxToolRounds          = 5
	MaxTokens              = 2500
	Temperature            = 0.3
)

// GrokClient handles communication with the xAI Grok API.
type GrokClient struct {
	apiKey          string
	model           string
	reasoningEffort string
	httpClient      *http.Client

	// Separate breakers per endpoint: Chat (tool-calling, the primary path
	// for nearly every daily interaction) and SearchWeb (Responses API,
	// only used for news/actuality intent) hit different xAI endpoints and
	// can fail independently. A single shared breaker would let an isolated
	// SearchWeb outage lock Jeffrey out of the primary assistant for 30s.
	chatBreaker   *gobreaker.CircuitBreaker
	searchBreaker *gobreaker.CircuitBreaker
}

func NewGrokClient(apiKey string) *GrokClient {
	return NewGrokClientWithOptions(apiKey, DefaultGrokModel, DefaultReasoningEffort)
}

func NewGrokClientWithOptions(apiKey, model, reasoningEffort string) *GrokClient {
	if model == "" {
		model = DefaultGrokModel
	}
	if reasoningEffort == "" {
		reasoningEffort = DefaultReasoningEffort
	}

	newBreaker := func(name string) *gobreaker.CircuitBreaker {
		// 3 consecutive failures -> open state for 30s
		return gobreaker.NewCircuitBreaker(gobreaker.Settings{
			Name:        name,
			MaxRequests: 1, // When half-open, allow 1 request to test
			Interval:    0,
			Timeout:     30 * time.Second, // Time before transitioning from Open to Half-Open
			ReadyToTrip: func(counts gobreaker.Counts) bool {
				return counts.ConsecutiveFailures >= 3
			},
			OnStateChange: func(name string, from gobreaker.State, to gobreaker.State) {
				slog.Warn("CircuitBreaker state changed", "name", name, "from", from.String(), "to", to.String())
			},
		})
	}

	return &GrokClient{
		apiKey:          apiKey,
		model:           model,
		reasoningEffort: reasoningEffort,
		httpClient:      &http.Client{Timeout: 60 * time.Second},
		chatBreaker:     newBreaker("GrokChatAPI"),
		searchBreaker:   newBreaker("GrokSearchAPI"),
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

type ResponsesAPIResponse struct {
	OutputText string `json:"output_text"`
	Output     []struct {
		Type    string `json:"type"`
		Role    string `json:"role"`
		Content []struct {
			Type string `json:"type"`
			Text string `json:"text"`
		} `json:"content"`
	} `json:"output"`
	Citations []string `json:"citations"`
	Error     *struct {
		Message string `json:"message"`
		Type    string `json:"type"`
	} `json:"error"`
	Usage Usage `json:"usage"`
}

// ChatResult is the final result of a chat interaction.
type ChatResult struct {
	OK       bool   `json:"ok"`
	Agent    *Agent `json:"agent,omitempty"`
	Antwoord string `json:"antwoord,omitempty"`
	Error    string `json:"error,omitempty"`
	Tokens   *Usage `json:"tokens,omitempty"`

	// Telemetry for observability/logging.
	Rounds       int      `json:"rounds,omitempty"`
	ToolsUsed    []string `json:"toolsUsed,omitempty"`
	FinishReason string   `json:"finishReason,omitempty"`
	DurationMs   int64    `json:"durationMs,omitempty"`
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
			"model":       c.model,
			"messages":    messages,
			"stream":      false,
			"temperature": Temperature,
			"max_tokens":  MaxTokens,
		}
		if c.reasoningEffort != "" {
			reqBody["reasoning_effort"] = c.reasoningEffort
		}
		if len(tools) > 0 {
			reqBody["tools"] = tools
		}

		data, err := json.Marshal(reqBody)
		if err != nil {
			return ChatResult{Error: fmt.Sprintf("marshal error: %v", err)}
		}

		req, err := http.NewRequestWithContext(ctx, http.MethodPost, GrokChatAPIURL, bytes.NewReader(data))
		if err != nil {
			return ChatResult{Error: fmt.Sprintf("request error: %v", err)}
		}
		req.Header.Set("Content-Type", "application/json")
		req.Header.Set("Authorization", "Bearer "+c.apiKey)

		// Execute request through Circuit Breaker
		respInterface, cbErr := c.chatBreaker.Execute(func() (interface{}, error) {
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
			if choice.FinishReason == "length" {
				// The completion was cut off mid-generation (token budget hit),
				// not a clean stop. Without this the reply just trails off with
				// no indication anything was truncated.
				content = strings.TrimSpace(content) + "\n\n(antwoord ingekort door lengte — vraag door voor meer detail)"
			}
			return ChatResult{
				OK:           true,
				Antwoord:     content,
				Tokens:       totalUsage,
				Rounds:       round + 1,
				ToolsUsed:    toolsUsed,
				FinishReason: choice.FinishReason,
				DurationMs:   duration.Milliseconds(),
			}
		}

		// Execute tool calls
		for _, tc := range msg.ToolCalls {
			toolStart := time.Now()
			var result string
			func() {
				defer func() {
					if r := recover(); r != nil {
						slog.Error("tool execution panic", "tool", tc.Function.Name, "recover", r)
						result = fmt.Sprintf(`{"error": "Fout bij uitvoeren van tool: interne panic: %v"}`, r)
					}
				}()
				result = executor.Execute(ctx, tc.Function.Name, tc.Function.Arguments)
			}()
			toolDur := time.Since(toolStart)
			toolsUsed = append(toolsUsed, fmt.Sprintf("%s(%dms)", tc.Function.Name, toolDur.Milliseconds()))

			framedResult := result
			if !IsMutatingTool(tc.Function.Name) {
				framedResult = fmt.Sprintf("[UNTRUSTED TOOL DATA START]\n%s\n[UNTRUSTED TOOL DATA END]", result)
			}

			messages = append(messages, Message{
				Role:       "tool",
				Content:    strPtr(framedResult),
				ToolCallID: tc.ID,
			})

		}
	}

	// Max rounds reached without Grok returning a natural-language finish.
	// The last appended message here is always a {Role:"tool"} result (raw
	// JSON from the executor, e.g. {"ok":true,"emails":[...]}), never
	// assistant text — shipping it directly used to leak a raw tool-result
	// blob straight into Telegram. Force one final no-tools completion so
	// Grok must synthesize everything gathered so far into a real answer.
	duration := time.Since(start)
	slog.Warn("[Grok] MAX_ROUNDS, forcing final synthesis",
		"duration", duration.Round(time.Millisecond),
		"tools", toolsUsed,
	)

	content, synthUsage, synthErr := c.finalSynthesis(ctx, messages)
	if synthErr != nil {
		slog.Warn("[Grok] MAX_ROUNDS synthesis failed", "error", synthErr)
		content = "Ik heb te veel data moeten ophalen om dit in één keer te verwerken. Probeer een specifiekere vraag."
	} else if synthUsage != nil {
		totalUsage = synthUsage
	}

	return ChatResult{
		OK:           true,
		Antwoord:     content,
		Tokens:       totalUsage,
		Rounds:       MaxToolRounds,
		ToolsUsed:    toolsUsed,
		FinishReason: "max_rounds",
		DurationMs:   time.Since(start).Milliseconds(),
	}
}

// finalSynthesis issues one last completion call with tools omitted, forcing
// Grok to answer in natural language using everything gathered so far
// instead of leaving the caller holding a raw tool-result message.
func (c *GrokClient) finalSynthesis(ctx context.Context, messages []Message) (string, *Usage, error) {
	msgs := make([]Message, 0, len(messages)+1)
	msgs = append(msgs, messages...)
	msgs = append(msgs, Message{
		Role:    "user",
		Content: strPtr("Je hebt genoeg informatie verzameld. Geef nu een definitief, samenvattend antwoord in het Nederlands voor Telegram plain text, zonder nog een tool aan te roepen."),
	})

	reqBody := map[string]any{
		"model":       c.model,
		"messages":    msgs,
		"stream":      false,
		"temperature": Temperature,
		"max_tokens":  MaxTokens,
	}
	if c.reasoningEffort != "" {
		reqBody["reasoning_effort"] = c.reasoningEffort
	}
	// Deliberately omit "tools" so the model cannot request another round.

	data, err := json.Marshal(reqBody)
	if err != nil {
		return "", nil, fmt.Errorf("marshal error: %w", err)
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, GrokChatAPIURL, bytes.NewReader(data))
	if err != nil {
		return "", nil, fmt.Errorf("request error: %w", err)
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Authorization", "Bearer "+c.apiKey)

	respInterface, cbErr := c.chatBreaker.Execute(func() (interface{}, error) {
		resp, err := c.httpClient.Do(req)
		if err != nil {
			return nil, err
		}
		if resp.StatusCode >= 500 {
			defer resp.Body.Close()
			return nil, fmt.Errorf("server error: %d", resp.StatusCode)
		}
		return resp, nil
	})
	if cbErr != nil {
		return "", nil, cbErr
	}

	resp := respInterface.(*http.Response)
	body, _ := io.ReadAll(resp.Body)
	resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return "", nil, fmt.Errorf("grok %d: %s", resp.StatusCode, truncate(string(body), 200))
	}

	var grokResp GrokResponse
	if err := json.Unmarshal(body, &grokResp); err != nil {
		return "", nil, fmt.Errorf("parse error: %w", err)
	}
	if len(grokResp.Choices) == 0 || grokResp.Choices[0].Message.Content == nil {
		return "", nil, fmt.Errorf("empty synthesis response")
	}
	return *grokResp.Choices[0].Message.Content, &grokResp.Usage, nil
}

// SearchWeb answers current-events questions through xAI Responses API web_search.
func (c *GrokClient) SearchWeb(ctx context.Context, userMessage string) ChatResult {
	start := time.Now()
	input := fmt.Sprintf(`Beantwoord Jeffrey in het Nederlands voor Telegram plain text.
Gebruik web_search voor actuele informatie.
Vraag: %s

Regels:
- Focus op de laatste 24 uur als de vraag dat vraagt.
- Geef een compacte top 5 met bronnaam of domein per punt.
- Noem expliciet als iets onzeker is of als bronnen verschillende details geven.
- Geen markdown-opmaak, geen codeblokken.`, userMessage)

	reqBody := map[string]any{
		"model": c.model,
		"input": []map[string]string{
			{
				"role":    "user",
				"content": input,
			},
		},
		"tools": []map[string]any{
			{"type": "web_search"},
		},
		"max_output_tokens": 1400,
	}

	data, err := json.Marshal(reqBody)
	if err != nil {
		return ChatResult{Error: fmt.Sprintf("marshal error: %v", err)}
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, GrokResponsesAPIURL, bytes.NewReader(data))
	if err != nil {
		return ChatResult{Error: fmt.Sprintf("request error: %v", err)}
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Authorization", "Bearer "+c.apiKey)

	respInterface, cbErr := c.searchBreaker.Execute(func() (interface{}, error) {
		resp, err := c.httpClient.Do(req)
		if err != nil {
			return nil, err
		}
		if resp.StatusCode >= 500 {
			defer resp.Body.Close()
			return nil, fmt.Errorf("server error: %d", resp.StatusCode)
		}
		return resp, nil
	})

	if cbErr != nil {
		if cbErr == gobreaker.ErrOpenState {
			return ChatResult{Error: "De AI zoekfunctie is tijdelijk onbereikbaar wegens overbelasting. Probeer het later opnieuw."}
		}
		return ChatResult{Error: fmt.Sprintf("API error: %v", cbErr)}
	}

	resp := respInterface.(*http.Response)
	body, _ := io.ReadAll(resp.Body)
	resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return ChatResult{Error: fmt.Sprintf("Grok search %d: %s", resp.StatusCode, truncate(string(body), 300))}
	}

	var grokResp ResponsesAPIResponse
	if err := json.Unmarshal(body, &grokResp); err != nil {
		return ChatResult{Error: fmt.Sprintf("parse error: %v", err)}
	}
	if grokResp.Error != nil {
		return ChatResult{Error: grokResp.Error.Message}
	}

	answer := strings.TrimSpace(grokResp.OutputText)
	if answer == "" {
		answer = extractResponsesText(grokResp)
	}
	if answer == "" {
		return ChatResult{Error: "Geen web-search antwoord van Grok"}
	}
	if len(grokResp.Citations) > 0 {
		answer += "\n\nBronnen: " + strings.Join(grokResp.Citations, ", ")
	}

	slog.Info("[Grok Web Search] OK",
		"duration", time.Since(start).Round(time.Millisecond),
		"tokens", grokResp.Usage.TotalTokens,
	)
	return ChatResult{
		OK:           true,
		Antwoord:     answer,
		Tokens:       &grokResp.Usage,
		Rounds:       1,
		FinishReason: "web_search",
		DurationMs:   time.Since(start).Milliseconds(),
	}
}

func extractResponsesText(resp ResponsesAPIResponse) string {
	var parts []string
	for _, output := range resp.Output {
		if output.Type != "message" && output.Type != "" {
			continue
		}
		for _, content := range output.Content {
			if content.Text != "" {
				parts = append(parts, content.Text)
			}
		}
	}
	return strings.TrimSpace(strings.Join(parts, "\n"))
}

// ToolExecutor is called by the chat loop to execute tool calls.
type ToolExecutor interface {
	Execute(ctx context.Context, toolName string, argsJSON string) string
}

// ToolDefinition is the OpenAI-compatible function definition for Grok.
type ToolDefinition struct {
	Type     string       `json:"type"`
	Function ToolFunction `json:"function"`
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

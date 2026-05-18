package telegram

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"mime/multipart"
	"net/http"
	"time"
)

const groqWhisperURL = "https://api.groq.com/openai/v1/audio/transcriptions"

// TranscribeVoice sends audio to Groq Whisper and returns the transcript.
func TranscribeVoice(groqKey string, audio []byte, filename string) (string, error) {
	var buf bytes.Buffer
	w := multipart.NewWriter(&buf)

	part, err := w.CreateFormFile("file", filename)
	if err != nil {
		return "", err
	}
	if _, err := part.Write(audio); err != nil {
		return "", err
	}
	_ = w.WriteField("model", "whisper-large-v3")
	_ = w.WriteField("language", "nl")
	w.Close()

	req, err := http.NewRequest(http.MethodPost, groqWhisperURL, &buf)
	if err != nil {
		return "", err
	}
	req.Header.Set("Authorization", "Bearer "+groqKey)
	req.Header.Set("Content-Type", w.FormDataContentType())

	client := &http.Client{Timeout: 30 * time.Second}
	resp, err := client.Do(req)
	if err != nil {
		return "", err
	}
	defer resp.Body.Close()

	body, _ := io.ReadAll(resp.Body)
	if resp.StatusCode != http.StatusOK {
		return "", fmt.Errorf("Groq Whisper %d: %s", resp.StatusCode, truncStr(string(body), 200))
	}

	var result struct {
		Text string `json:"text"`
	}
	if err := json.Unmarshal(body, &result); err != nil {
		return "", err
	}
	return result.Text, nil
}

func truncStr(s string, max int) string {
	if len(s) <= max {
		return s
	}
	return s[:max]
}

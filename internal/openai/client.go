package openai

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"
)

type Client struct {
	APIKey          string
	Model           string
	TTSModel        string
	TTSVoice        string
	TTSInstructions string
	SystemPrompt    string
	HTTPClient      *http.Client
}

type ReplyInput struct {
	MessageText       string
	SenderID          int64
	SenderUsername    string
	SenderDisplayName string
	ReplyContext      string
	ImageURL          string
}

func NewClient(apiKey, model, ttsModel, ttsVoice, ttsInstructions, systemPrompt string) *Client {
	return &Client{
		APIKey:          apiKey,
		Model:           model,
		TTSModel:        ttsModel,
		TTSVoice:        ttsVoice,
		TTSInstructions: ttsInstructions,
		SystemPrompt:    systemPrompt,
	}
}

func (c *Client) GenerateSpeech(ctx context.Context, text string) ([]byte, error) {
	ctx, cancel := context.WithTimeout(ctx, 25*time.Second)
	defer cancel()

	reqBody := map[string]any{
		"model":           c.TTSModel,
		"voice":           c.TTSVoice,
		"input":           text,
		"response_format": "opus",
	}
	if strings.TrimSpace(c.TTSInstructions) != "" {
		reqBody["instructions"] = c.TTSInstructions
	}

	payload, err := json.Marshal(reqBody)
	if err != nil {
		return nil, fmt.Errorf("marshal speech request: %w", err)
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, "https://api.openai.com/v1/audio/speech", bytes.NewReader(payload))
	if err != nil {
		return nil, fmt.Errorf("build speech request: %w", err)
	}
	req.Header.Set("Authorization", "Bearer "+c.APIKey)
	req.Header.Set("Content-Type", "application/json")

	httpClient := c.HTTPClient
	if httpClient == nil {
		httpClient = http.DefaultClient
	}

	resp, err := httpClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("speech http request: %w", err)
	}
	defer resp.Body.Close()

	respBytes, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, fmt.Errorf("read speech response: %w", err)
	}
	if resp.StatusCode >= 300 {
		return nil, fmt.Errorf("speech api status %d: %s", resp.StatusCode, strings.TrimSpace(string(respBytes)))
	}
	return respBytes, nil
}

func (c *Client) GenerateReply(ctx context.Context, in ReplyInput) (string, error) {
	return c.generateWithContent(ctx, buildContentInput(in))
}

func (c *Client) GeneratePromptReply(ctx context.Context, prompt string) (string, error) {
	content := []map[string]any{
		{
			"type": "input_text",
			"text": strings.TrimSpace(prompt),
		},
	}
	return c.generateWithContent(ctx, content)
}

func (c *Client) generateWithContent(ctx context.Context, content []map[string]any) (string, error) {
	ctx, cancel := context.WithTimeout(ctx, 20*time.Second)
	defer cancel()

	reqBody := map[string]any{
		"model":        c.Model,
		"instructions": c.SystemPrompt,
		"input": []map[string]any{
			{
				"role":    "user",
				"content": content,
			},
		},
		"max_output_tokens": 300,
	}

	payload, err := json.Marshal(reqBody)
	if err != nil {
		return "", fmt.Errorf("marshal request: %w", err)
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, "https://api.openai.com/v1/responses", bytes.NewReader(payload))
	if err != nil {
		return "", fmt.Errorf("build request: %w", err)
	}
	req.Header.Set("Authorization", "Bearer "+c.APIKey)
	req.Header.Set("Content-Type", "application/json")

	httpClient := c.HTTPClient
	if httpClient == nil {
		httpClient = http.DefaultClient
	}

	resp, err := httpClient.Do(req)
	if err != nil {
		return "", fmt.Errorf("http request: %w", err)
	}
	defer resp.Body.Close()

	respBytes, err := io.ReadAll(resp.Body)
	if err != nil {
		return "", fmt.Errorf("read response: %w", err)
	}

	if resp.StatusCode >= 300 {
		return "", fmt.Errorf("api status %d: %s", resp.StatusCode, strings.TrimSpace(string(respBytes)))
	}

	var parsed struct {
		Output []struct {
			Type    string `json:"type"`
			Content []struct {
				Type string `json:"type"`
				Text string `json:"text"`
			} `json:"content"`
		} `json:"output"`
	}
	if err := json.Unmarshal(respBytes, &parsed); err != nil {
		return "", fmt.Errorf("unmarshal response: %w", err)
	}

	for _, item := range parsed.Output {
		if item.Type != "message" {
			continue
		}
		for _, content := range item.Content {
			if content.Type == "output_text" && strings.TrimSpace(content.Text) != "" {
				return strings.TrimSpace(content.Text), nil
			}
		}
	}

	return "", fmt.Errorf("no output_text in response")
}

func buildContextPrompt(in ReplyInput) string {
	userHandle := "unknown"
	if in.SenderUsername != "" {
		userHandle = "@" + in.SenderUsername
	}
	displayName := in.SenderDisplayName
	if displayName == "" {
		displayName = "unknown"
	}
	contextPrompt := fmt.Sprintf(
		"Telegram sender context: id=%d, handle=%s, display_name=%q.\n",
		in.SenderID,
		userHandle,
		displayName,
	)
	if in.ReplyContext != "" {
		contextPrompt += "Replied message context:\n" + in.ReplyContext + "\n"
	}
	contextPrompt += "Current message:\n" + in.MessageText
	return contextPrompt
}

func buildContentInput(in ReplyInput) []map[string]any {
	content := []map[string]any{
		{
			"type": "input_text",
			"text": buildContextPrompt(in),
		},
	}
	if in.ImageURL != "" {
		content = append(content, map[string]any{
			"type":      "input_image",
			"image_url": in.ImageURL,
		})
	}
	return content
}

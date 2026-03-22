package openai

import (
	"encoding/json"
	"errors"
	"strings"
	"testing"
)

func TestBuildInstructions(t *testing.T) {
	t.Parallel()

	client := &Client{SystemPrompt: "base prompt"}

	got := client.buildInstructions("  act like a pirate ")
	if !strings.HasPrefix(got, "act like a pirate") {
		t.Fatalf("buildInstructions(persona) = %q", got)
	}
	if !strings.Contains(got, messageFormatGuidelines) {
		t.Fatalf("buildInstructions(persona) missing guidelines: %q", got)
	}

	got = client.buildInstructions("")
	if !strings.HasPrefix(got, "base prompt") {
		t.Fatalf("buildInstructions(base) = %q", got)
	}

	client.SystemPrompt = ""
	got = client.buildInstructions("")
	if !strings.HasPrefix(got, fallbackSystemPrompt) {
		t.Fatalf("buildInstructions(fallback) = %q", got)
	}
}

func TestBuildContextPromptAndContentInput(t *testing.T) {
	t.Parallel()

	in := ReplyInput{
		MessageText:         "hello",
		SenderID:            7,
		SenderUsername:      "alice",
		SenderDisplayName:   "Alice",
		ReplyContext:        "replied text",
		ConversationContext: "user: hi",
		ImageURL:            "https://example.com/image.png",
	}

	prompt := buildContextPrompt(in)
	if !strings.Contains(prompt, "handle=@alice") {
		t.Fatalf("buildContextPrompt() = %q", prompt)
	}
	if !strings.Contains(prompt, "Recent conversation history:\nuser: hi") {
		t.Fatalf("buildContextPrompt() missing history: %q", prompt)
	}
	if !strings.Contains(prompt, "Current message:\nhello") {
		t.Fatalf("buildContextPrompt() missing message: %q", prompt)
	}

	content := buildContentInput(in)
	if len(content) != 2 {
		t.Fatalf("buildContentInput() len = %d, want 2", len(content))
	}
	if content[1]["type"] != "input_image" {
		t.Fatalf("buildContentInput()[1] = %#v", content[1])
	}
}

func TestBuildResponsePayload(t *testing.T) {
	t.Parallel()

	client := &Client{Model: "gpt-test", SystemPrompt: "system"}
	payload, err := client.buildResponsePayload(
		[]map[string]any{{"type": "input_text", "text": "hello"}},
		"",
	)
	if err != nil {
		t.Fatalf("buildResponsePayload() error = %v", err)
	}

	var parsed map[string]any
	if err := json.Unmarshal(payload, &parsed); err != nil {
		t.Fatalf("json.Unmarshal() error = %v", err)
	}
	if parsed["model"] != "gpt-test" {
		t.Fatalf("model = %#v", parsed["model"])
	}
	if parsed["instructions"] != "system" {
		t.Fatalf("instructions = %#v", parsed["instructions"])
	}
}

func TestParseOutputText(t *testing.T) {
	t.Parallel()

	resp := []byte(`{"output":[{"type":"message","content":[{"type":"output_text","text":"  hello world  "}]}]}`)
	got, err := parseOutputText(resp)
	if err != nil {
		t.Fatalf("parseOutputText() error = %v", err)
	}
	if got != "hello world" {
		t.Fatalf("parseOutputText() = %q", got)
	}

	_, err = parseOutputText([]byte(`{"output":[{"type":"message","content":[{"type":"other","text":"x"}]}]}`))
	if !errors.Is(err, errMissingOutputText) {
		t.Fatalf("parseOutputText() error = %v, want %v", err, errMissingOutputText)
	}
}

func TestFallbackInstructionsAndFirstOutputText(t *testing.T) {
	t.Parallel()

	if got := fallbackInstructions(" custom ", "system"); got != " custom " {
		t.Fatalf("fallbackInstructions(custom) = %q", got)
	}
	if got := fallbackInstructions("", "system"); got != "system" {
		t.Fatalf("fallbackInstructions(system) = %q", got)
	}

	output := []struct {
		Type    string `json:"type"`
		Content []struct {
			Type string `json:"type"`
			Text string `json:"text"`
		} `json:"content"`
	}{
		{
			Type: "message",
			Content: []struct {
				Type string `json:"type"`
				Text string `json:"text"`
			}{
				{Type: "output_text", Text: " hi "},
			},
		},
	}
	if got := firstOutputText(output); got != "hi" {
		t.Fatalf("firstOutputText() = %q", got)
	}
}

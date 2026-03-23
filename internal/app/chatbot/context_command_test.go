package chatbot

import "testing"

func TestParseContextCommand(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name      string
		text      string
		wantOK    bool
		wantToken string
	}{
		{name: "slash command", text: "/context_clear", wantOK: true, wantToken: "/context_clear"},
		{name: "bang command", text: "!context_clear", wantOK: true, wantToken: "!context_clear"},
		{name: "mention suffix", text: "/context_clear@MyBot", wantOK: true, wantToken: "/context_clear"},
		{name: "show command", text: "/context_show", wantOK: true, wantToken: "/context_show"},
		{name: "show bang command", text: "!context_show", wantOK: true, wantToken: "!context_show"},
		{name: "extra args rejected", text: "/context_clear now", wantOK: false},
		{name: "plain text rejected", text: "clear context", wantOK: false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			gotToken, gotOK := parseContextCommand(tt.text)
			if gotOK != tt.wantOK {
				t.Fatalf("parseContextCommand(%q) ok = %v, want %v", tt.text, gotOK, tt.wantOK)
			}
			if gotToken != tt.wantToken {
				t.Fatalf("parseContextCommand(%q) token = %q, want %q", tt.text, gotToken, tt.wantToken)
			}
		})
	}
}

func TestExecuteShowContextEmpty(t *testing.T) {
	t.Parallel()

	got := formatContextShowMessage("")
	if got != "Context is empty for this chat." {
		t.Fatalf("executeShowContext() = %q", got)
	}
}

func TestExecuteShowContextReturnsStoredHistory(t *testing.T) {
	t.Parallel()

	got := formatContextShowMessage("user: @alice: hi\nassistant: hello")
	want := "Current context:\nuser: @alice: hi\nassistant: hello"
	if got != want {
		t.Fatalf("formatContextShowMessage() = %q, want %q", got, want)
	}
}

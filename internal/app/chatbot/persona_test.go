package chatbot

import "testing"

func TestParsePersonaCommandRecognizesUpdatedCommands(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name       string
		text       string
		wantOK     bool
		wantToken  string
		wantArgs   string
		wantAction personaCommandAction
	}{
		{name: "set command", text: "/persona be concise", wantOK: true, wantToken: "/persona", wantArgs: "be concise"},
		{name: "show command", text: "/persona_show", wantOK: true, wantToken: "/persona_show", wantAction: personaCommandShow},
		{name: "clear command", text: "/persona_clear", wantOK: true, wantToken: "/persona_clear", wantAction: personaCommandClear},
		{name: "show command bang", text: "!persona_show", wantOK: true, wantToken: "!persona_show", wantAction: personaCommandShow},
		{name: "legacy show becomes regular persona text", text: "/persona show", wantOK: true, wantToken: "/persona", wantArgs: "show"},
		{name: "legacy clear becomes regular persona text", text: "/persona clear", wantOK: true, wantToken: "/persona", wantArgs: "clear"},
		{name: "bot mention suffix", text: "/persona_show@MyBot", wantOK: true, wantToken: "/persona_show", wantAction: personaCommandShow},
		{name: "unknown command", text: "/persona_reset", wantOK: false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			gotToken, gotArgs, gotAction, gotOK := parsePersonaCommand(tt.text)
			if gotOK != tt.wantOK {
				t.Fatalf("parsePersonaCommand(%q) ok = %v, want %v", tt.text, gotOK, tt.wantOK)
			}
			if !tt.wantOK {
				return
			}
			if gotToken != tt.wantToken {
				t.Fatalf("parsePersonaCommand(%q) token = %q, want %q", tt.text, gotToken, tt.wantToken)
			}
			if gotArgs != tt.wantArgs {
				t.Fatalf("parsePersonaCommand(%q) args = %q, want %q", tt.text, gotArgs, tt.wantArgs)
			}
			if gotAction != tt.wantAction {
				t.Fatalf("parsePersonaCommand(%q) action = %q, want %q", tt.text, gotAction, tt.wantAction)
			}
		})
	}
}

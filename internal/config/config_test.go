package config

import (
	"errors"
	"os"
	"path/filepath"
	"testing"
)

func TestLoadBotAppliesDefaultsAndCleansLists(t *testing.T) {
	t.Parallel()

	configPath := filepath.Join(t.TempDir(), "bot.yaml")
	content := []byte(`
bot_reactions: [" 👍 ", "", "🤣"]
bot_random_reply_chance: 0.25
bot_reaction_chance: 0.5
bot_random_sticker_chance: 0.1
bot_tts_reply_chance: 0.2
bot_sticker_file_ids: [" one ", "", "two"]
`)
	if err := os.WriteFile(configPath, content, 0o600); err != nil {
		t.Fatalf("WriteFile() error = %v", err)
	}

	cfg, err := LoadBot(configPath)
	if err != nil {
		t.Fatalf("LoadBot() error = %v", err)
	}

	assertBotDefaults(t, cfg, filepath.Dir(configPath))
	assertCleanedBotLists(t, cfg)
}

func TestLoadBotReturnsValidationError(t *testing.T) {
	t.Parallel()

	configPath := filepath.Join(t.TempDir(), "bot.yaml")
	content := []byte(`
bot_reactions: []
bot_random_reply_chance: 0.25
bot_reaction_chance: 1.5
bot_random_sticker_chance: 0.1
bot_tts_reply_chance: 0.2
conversation_db_path: "data/conversations.db"
`)
	if err := os.WriteFile(configPath, content, 0o600); err != nil {
		t.Fatalf("WriteFile() error = %v", err)
	}

	_, err := LoadBot(configPath)
	if !errors.Is(err, errInvalidReactionChance) {
		t.Fatalf("LoadBot() error = %v, want %v", err, errInvalidReactionChance)
	}
}

func TestLoadBotResolvesRelativePathsFromConfigDir(t *testing.T) {
	t.Parallel()

	configDir := filepath.Join(t.TempDir(), "deploy")
	configPath := filepath.Join(configDir, "bot.yaml")
	content := []byte(`
log_file_path: "logs/bot.log"
conversation_db_path: "data/conversations.db"
bot_reactions: ["👍"]
bot_random_reply_chance: 0.25
bot_reaction_chance: 0.5
bot_random_sticker_chance: 0.1
bot_tts_reply_chance: 0.2
`)
	if err := os.MkdirAll(configDir, 0o750); err != nil {
		t.Fatalf("MkdirAll() error = %v", err)
	}
	if err := os.WriteFile(configPath, content, 0o600); err != nil {
		t.Fatalf("WriteFile() error = %v", err)
	}

	cfg, err := LoadBot(configPath)
	if err != nil {
		t.Fatalf("LoadBot() error = %v", err)
	}

	if cfg.Log.FilePath != filepath.Join(configDir, "logs", "bot.log") {
		t.Fatalf("Log.FilePath = %q", cfg.Log.FilePath)
	}
	if cfg.DBPath != filepath.Join(configDir, "data", "conversations.db") {
		t.Fatalf("DBPath = %q", cfg.DBPath)
	}
}

func assertBotDefaults(t *testing.T, cfg Bot, baseDir string) {
	t.Helper()

	if cfg.DBPath != filepath.Join(baseDir, "data", "conversations.db") {
		t.Fatalf("DBPath = %q, want default", cfg.DBPath)
	}
	if cfg.Log.FilePath != filepath.Join(baseDir, "logs", "bot.log") {
		t.Fatalf("Log.FilePath = %q, want default", cfg.Log.FilePath)
	}
	if cfg.OpenAI.Model != "gpt-4.1-mini" {
		t.Fatalf("OpenAI.Model = %q, want default", cfg.OpenAI.Model)
	}
}

func assertCleanedBotLists(t *testing.T, cfg Bot) {
	t.Helper()

	if len(cfg.Reactions) != 2 || cfg.Reactions[0] != "👍" || cfg.Reactions[1] != "🤣" {
		t.Fatalf("Reactions = %#v, want cleaned list", cfg.Reactions)
	}
	if len(cfg.StickerFileIDs) != 2 || cfg.StickerFileIDs[0] != "one" || cfg.StickerFileIDs[1] != "two" {
		t.Fatalf("StickerFileIDs = %#v, want cleaned list", cfg.StickerFileIDs)
	}
}

func TestLoadRuntimeFromEnv(t *testing.T) {
	t.Setenv(envTelegramBotToken, " telegram-token ")
	t.Setenv(envOpenAIAPIKey, " openai-key ")

	runtime, err := LoadRuntimeFromEnv()
	if err != nil {
		t.Fatalf("LoadRuntimeFromEnv() error = %v", err)
	}
	if runtime.TelegramToken != "telegram-token" {
		t.Fatalf("TelegramToken = %q", runtime.TelegramToken)
	}
	if runtime.OpenAIAPIKey != "openai-key" {
		t.Fatalf("OpenAIAPIKey = %q", runtime.OpenAIAPIKey)
	}
}

func TestLoadRuntimeFromEnvMissingVars(t *testing.T) {
	t.Setenv(envTelegramBotToken, "")
	t.Setenv(envOpenAIAPIKey, "")

	_, err := LoadRuntimeFromEnv()
	if !errors.Is(err, errMissingTelegramBotToken) {
		t.Fatalf("LoadRuntimeFromEnv() error = %v, want %v", err, errMissingTelegramBotToken)
	}
}

func TestLoadDotEnvSetsOnlyMissingVars(t *testing.T) {
	path := filepath.Join(t.TempDir(), ".env")
	content := []byte("FOO=bar\nBAR=\"baz qux\"\n# comment\nEMPTY=\n")
	if err := os.WriteFile(path, content, 0o600); err != nil {
		t.Fatalf("WriteFile() error = %v", err)
	}

	t.Setenv("BAR", "keep")
	if err := LoadDotEnv(path); err != nil {
		t.Fatalf("LoadDotEnv() error = %v", err)
	}

	if got := os.Getenv("FOO"); got != "bar" {
		t.Fatalf("FOO = %q, want bar", got)
	}
	if got := os.Getenv("BAR"); got != "keep" {
		t.Fatalf("BAR = %q, want keep", got)
	}
	if got := os.Getenv("EMPTY"); got != "" {
		t.Fatalf("EMPTY = %q, want empty string", got)
	}
}

func TestParseDotEnvLine(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name      string
		input     string
		wantKey   string
		wantValue string
		wantOK    bool
	}{
		{name: "valid", input: "KEY=value", wantKey: "KEY", wantValue: "value", wantOK: true},
		{name: "quoted", input: "KEY='value here'", wantKey: "KEY", wantValue: "value here", wantOK: true},
		{name: "comment", input: "# hi", wantOK: false},
		{name: "missing eq", input: "KEY", wantOK: false},
		{name: "empty key", input: "=value", wantOK: false},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			key, value, ok := parseDotEnvLine(tc.input)
			if key != tc.wantKey || value != tc.wantValue || ok != tc.wantOK {
				t.Fatalf("parseDotEnvLine(%q) = (%q, %q, %t)", tc.input, key, value, ok)
			}
		})
	}
}

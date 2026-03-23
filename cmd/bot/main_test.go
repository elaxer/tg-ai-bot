package main

import (
	"os"
	"path/filepath"
	"testing"
)

func TestResolveDotEnvPathPrefersWorkingDir(t *testing.T) {
	t.Parallel()

	workDir := t.TempDir()
	execDir := t.TempDir()

	writeTestFile(t, filepath.Join(workDir, ".env"))
	writeTestFile(t, filepath.Join(execDir, ".env"))

	got := resolveDotEnvPath(workDir, execDir)
	want := filepath.Join(workDir, ".env")
	if got != want {
		t.Fatalf("resolveDotEnvPath() = %q, want %q", got, want)
	}
}

func TestResolveDotEnvPathFallsBackToExecutableDir(t *testing.T) {
	t.Parallel()

	workDir := t.TempDir()
	execDir := t.TempDir()
	writeTestFile(t, filepath.Join(execDir, ".env"))

	got := resolveDotEnvPath(workDir, execDir)
	want := filepath.Join(execDir, ".env")
	if got != want {
		t.Fatalf("resolveDotEnvPath() = %q, want %q", got, want)
	}
}

func TestResolveConfigPathPrefersWorkingDirDefault(t *testing.T) {
	t.Parallel()

	workDir := t.TempDir()
	execDir := t.TempDir()

	writeTestFile(t, filepath.Join(workDir, defaultBotConfigPath))
	writeTestFile(t, filepath.Join(execDir, defaultBotConfigPath))

	got := resolveConfigPath(workDir, execDir, "")
	want := filepath.Join(workDir, defaultBotConfigPath)
	if got != want {
		t.Fatalf("resolveConfigPath() = %q, want %q", got, want)
	}
}

func TestResolveConfigPathFallsBackToExecutableDirDefault(t *testing.T) {
	t.Parallel()

	workDir := t.TempDir()
	execDir := t.TempDir()
	writeTestFile(t, filepath.Join(execDir, defaultBotConfigPath))

	got := resolveConfigPath(workDir, execDir, "")
	want := filepath.Join(execDir, defaultBotConfigPath)
	if got != want {
		t.Fatalf("resolveConfigPath() = %q, want %q", got, want)
	}
}

func TestResolveConfigPathUsesWorkingDirForRelativeEnvPath(t *testing.T) {
	t.Parallel()

	workDir := t.TempDir()
	execDir := t.TempDir()

	got := resolveConfigPath(workDir, execDir, "deploy/bot.config.yaml")
	want := filepath.Join(workDir, "deploy", "bot.config.yaml")
	if got != want {
		t.Fatalf("resolveConfigPath() = %q, want %q", got, want)
	}
}

func TestResolveConfigPathKeepsAbsoluteEnvPath(t *testing.T) {
	t.Parallel()

	workDir := t.TempDir()
	execDir := t.TempDir()
	absPath := filepath.Join(t.TempDir(), "bot.config.yaml")

	got := resolveConfigPath(workDir, execDir, absPath)
	if got != absPath {
		t.Fatalf("resolveConfigPath() = %q, want %q", got, absPath)
	}
}

func writeTestFile(t *testing.T, path string) {
	t.Helper()

	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatalf("MkdirAll() error = %v", err)
	}
	if err := os.WriteFile(path, []byte("test"), 0o600); err != nil {
		t.Fatalf("WriteFile() error = %v", err)
	}
}

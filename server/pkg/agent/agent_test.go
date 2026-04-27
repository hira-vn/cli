package agent

import (
	"context"
	"os"
	"strings"
	"testing"
)

func TestNewReturnsClaudeBackend(t *testing.T) {
	t.Parallel()
	b, err := New("claude", Config{ExecutablePath: "/nonexistent/claude"})
	if err != nil {
		t.Fatalf("New(claude) error: %v", err)
	}
	if _, ok := b.(*claudeBackend); !ok {
		t.Fatalf("expected *claudeBackend, got %T", b)
	}
}

func TestNewReturnsCodexBackend(t *testing.T) {
	t.Parallel()
	b, err := New("codex", Config{ExecutablePath: "/nonexistent/codex"})
	if err != nil {
		t.Fatalf("New(codex) error: %v", err)
	}
	if _, ok := b.(*codexBackend); !ok {
		t.Fatalf("expected *codexBackend, got %T", b)
	}
}

func TestNewReturnsCopilotBackend(t *testing.T) {
	t.Parallel()
	b, err := New("copilot", Config{ExecutablePath: "/nonexistent/copilot"})
	if err != nil {
		t.Fatalf("New(copilot) error: %v", err)
	}
	if _, ok := b.(*copilotBackend); !ok {
		t.Fatalf("expected *copilotBackend, got %T", b)
	}
}

func TestNewRejectsUnknownType(t *testing.T) {
	t.Parallel()
	_, err := New("gpt", Config{})
	if err == nil {
		t.Fatal("expected error for unknown agent type")
	}
}

func TestNewDefaultsLogger(t *testing.T) {
	t.Parallel()
	b, _ := New("claude", Config{})
	cb := b.(*claudeBackend)
	if cb.cfg.Logger == nil {
		t.Fatal("expected non-nil logger")
	}
}

func TestDetectVersionFailsForMissingBinary(t *testing.T) {
	t.Parallel()
	_, err := DetectVersion(context.Background(), "/nonexistent/binary")
	if err == nil {
		t.Fatal("expected error for missing binary")
	}
}

func TestLaunchHeaderCoversAllSupportedBackends(t *testing.T) {
	t.Parallel()

	// The factory in New() enumerates every supported agent type; LaunchHeader
	// must stay in sync so the UI preview never shows an empty skeleton for a
	// runtime the daemon actually spawns. If a new backend is added, add an
	// entry to launchHeaders in agent.go and extend this list.
	supported := []string{
		"claude", "codex", "copilot", "cursor", "gemini",
		"hermes", "openclaw", "opencode", "pi",
	}
	for _, t_ := range supported {
		if header := LaunchHeader(t_); header == "" {
			t.Errorf("LaunchHeader(%q) returned empty string — add it to launchHeaders", t_)
		}
	}
}

func TestLaunchHeaderReturnsEmptyForUnknownType(t *testing.T) {
	t.Parallel()
	if header := LaunchHeader("made-up-agent"); header != "" {
		t.Errorf("expected empty header for unknown type, got %q", header)
	}
}

func TestEnsureExecutable_PassesForRegularFile(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	binPath := dir + "/fake-cli"
	if err := os.WriteFile(binPath, []byte("#!/bin/sh\necho ok\n"), 0o755); err != nil {
		t.Fatalf("write fake binary: %v", err)
	}
	if err := EnsureExecutable(binPath); err != nil {
		t.Errorf("expected nil for an existing regular file, got %v", err)
	}
}

func TestEnsureExecutable_FailsClearlyForMissingPath(t *testing.T) {
	t.Parallel()
	missing := t.TempDir() + "/never-existed/claude"
	err := EnsureExecutable(missing)
	if err == nil {
		t.Fatal("expected an error for a missing binary, got nil")
	}
	// Operator should be able to copy-paste the path AND know what to do.
	if !strings.Contains(err.Error(), missing) {
		t.Errorf("error must include the path that was tried; got %q", err.Error())
	}
	if !strings.Contains(err.Error(), "restart `hira daemon`") {
		t.Errorf("error must hint at the recovery action; got %q", err.Error())
	}
}

func TestEnsureExecutable_RejectsEmptyPath(t *testing.T) {
	t.Parallel()
	if err := EnsureExecutable(""); err == nil {
		t.Error("expected error for empty path, got nil")
	}
	if err := EnsureExecutable("   "); err == nil {
		t.Error("expected error for whitespace-only path, got nil")
	}
}

func TestEnsureExecutable_RejectsDirectory(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	err := EnsureExecutable(dir)
	if err == nil {
		t.Fatal("expected error when given a directory, got nil")
	}
	if !strings.Contains(err.Error(), "directory") {
		t.Errorf("error must explain the directory mismatch; got %q", err.Error())
	}
}

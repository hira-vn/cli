package daemon

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// TestEnsureAgentDiscoveryPATH_PrependsExistingDirs verifies the helper
// prepends user-local install dirs that exist on disk while leaving the
// rest of $PATH untouched. This is the safety net that lets a daemon
// launched from a stripped environment (launchctl, nohup) still find
// claude / codex / copilot in their conventional install locations.
func TestEnsureAgentDiscoveryPATH_PrependsExistingDirs(t *testing.T) {
	tmpHome := t.TempDir()
	localBin := filepath.Join(tmpHome, ".local", "bin")
	if err := os.MkdirAll(localBin, 0o755); err != nil {
		t.Fatalf("create local bin: %v", err)
	}

	// Sandbox HOME and PATH so the test does not mutate the developer's shell.
	t.Setenv("HOME", tmpHome)
	originalPATH := "/sandbox/only:/usr/bin"
	t.Setenv("PATH", originalPATH)

	ensureAgentDiscoveryPATH()

	got := os.Getenv("PATH")
	if !strings.HasPrefix(got, localBin+string(os.PathListSeparator)) {
		t.Errorf("expected PATH to start with %q, got %q", localBin, got)
	}
	if !strings.HasSuffix(got, originalPATH) {
		t.Errorf("expected original PATH suffix preserved, got %q", got)
	}
}

// TestEnsureAgentDiscoveryPATH_IsIdempotent guards against runaway PATH
// growth if anything calls the helper twice (re-exec, hot reload, etc.).
func TestEnsureAgentDiscoveryPATH_IsIdempotent(t *testing.T) {
	tmpHome := t.TempDir()
	localBin := filepath.Join(tmpHome, ".local", "bin")
	if err := os.MkdirAll(localBin, 0o755); err != nil {
		t.Fatalf("create local bin: %v", err)
	}
	t.Setenv("HOME", tmpHome)
	t.Setenv("PATH", "/sandbox/only:/usr/bin")

	ensureAgentDiscoveryPATH()
	first := os.Getenv("PATH")
	ensureAgentDiscoveryPATH()
	second := os.Getenv("PATH")

	if first != second {
		t.Errorf("PATH grew on second call:\n  first:  %q\n  second: %q", first, second)
	}
	if strings.Count(second, localBin) != 1 {
		t.Errorf("expected %q to appear exactly once, got %d times in %q",
			localBin, strings.Count(second, localBin), second)
	}
}

// TestEnsureAgentDiscoveryPATH_SkipsMissingDirs makes sure we don't add
// noise to PATH for candidate dirs the host happens not to use.
func TestEnsureAgentDiscoveryPATH_SkipsMissingDirs(t *testing.T) {
	// HOME points at a real (empty) tempdir — none of the candidate dirs
	// (~/.local/bin, ~/bin, /opt/homebrew/bin, etc.) need to exist underneath
	// it for the test, but if /opt/homebrew/bin happens to exist on the host
	// it will still be added; that's fine — we just assert no garbage.
	tmpHome := t.TempDir()
	t.Setenv("HOME", tmpHome)
	t.Setenv("PATH", "/sandbox/only")

	ensureAgentDiscoveryPATH()
	got := os.Getenv("PATH")

	// The empty home means ~/.local/bin and ~/bin don't exist; ensure they
	// were not added.
	missing := filepath.Join(tmpHome, ".local", "bin")
	if strings.Contains(got, missing) {
		t.Errorf("PATH should not contain non-existent dir %q, got %q", missing, got)
	}
}

package handler

import (
	"encoding/json"
	"testing"
)

// TestMergeKnowledgeMCPConfig_EmptyInputCreatesEnvelope verifies that a
// nil/empty mcp_config input produces a fresh envelope with the
// hira-knowledge entry wired to the given workspace.
func TestMergeKnowledgeMCPConfig_EmptyInputCreatesEnvelope(t *testing.T) {
	out, err := mergeKnowledgeMCPConfig(nil, "ws-abc")
	if err != nil {
		t.Fatalf("merge: %v", err)
	}
	var parsed map[string]any
	if err := json.Unmarshal(out, &parsed); err != nil {
		t.Fatalf("parse output: %v", err)
	}
	servers, ok := parsed["mcpServers"].(map[string]any)
	if !ok {
		t.Fatalf("mcpServers not a map: %T", parsed["mcpServers"])
	}
	entry, ok := servers["hira-knowledge"].(map[string]any)
	if !ok {
		t.Fatalf("hira-knowledge entry missing: %v", servers)
	}
	if entry["command"] != "hira" {
		t.Errorf("command not 'hira': %v", entry["command"])
	}
	args, _ := entry["args"].([]any)
	if len(args) < 4 || args[3] != "ws-abc" {
		t.Errorf("workspace id not wired into args: %v", args)
	}
}

// TestMergeKnowledgeMCPConfig_PreservesExistingServers guarantees that an
// existing agent mcp_config that already defines custom servers keeps them
// after the knowledge block is injected.
func TestMergeKnowledgeMCPConfig_PreservesExistingServers(t *testing.T) {
	existing := json.RawMessage(`{"mcpServers":{"legacy":{"command":"foo","args":["bar"]}}}`)
	out, err := mergeKnowledgeMCPConfig(existing, "ws-1")
	if err != nil {
		t.Fatalf("merge: %v", err)
	}
	var parsed map[string]any
	_ = json.Unmarshal(out, &parsed)
	servers, _ := parsed["mcpServers"].(map[string]any)
	if _, ok := servers["legacy"]; !ok {
		t.Errorf("legacy server dropped: %v", servers)
	}
	if _, ok := servers["hira-knowledge"]; !ok {
		t.Errorf("hira-knowledge not added: %v", servers)
	}
}

// TestMergeKnowledgeMCPConfig_HandlesMalformedInput ensures malformed input
// does not corrupt the response — it resets to a fresh envelope instead of
// propagating parse errors back to the daemon claim path.
func TestMergeKnowledgeMCPConfig_HandlesMalformedInput(t *testing.T) {
	out, err := mergeKnowledgeMCPConfig(json.RawMessage("not-json-at-all"), "ws-2")
	if err != nil {
		t.Fatalf("merge returned error on malformed input: %v", err)
	}
	var parsed map[string]any
	if err := json.Unmarshal(out, &parsed); err != nil {
		t.Fatalf("parse output: %v", err)
	}
	if _, ok := parsed["mcpServers"].(map[string]any); !ok {
		t.Errorf("mcpServers not rebuilt on malformed input: %v", parsed)
	}
}

package util

import (
	"strings"
	"testing"
)

func TestBuildToolCallInstructions_ExecCommandUsesCmdExample(t *testing.T) {
	out := BuildToolCallInstructions([]string{"exec_command"})
	if !strings.Contains(out, `<tool_name>exec_command</tool_name>`) {
		t.Fatalf("expected exec_command in examples, got: %s", out)
	}
	if !strings.Contains(out, `<parameters>{"cmd":"pwd"}</parameters>`) {
		t.Fatalf("expected cmd parameter example for exec_command, got: %s", out)
	}
}

func TestBuildToolCallInstructions_ExecuteCommandUsesCommandExample(t *testing.T) {
	out := BuildToolCallInstructions([]string{"execute_command"})
	if !strings.Contains(out, `<tool_name>execute_command</tool_name>`) {
		t.Fatalf("expected execute_command in examples, got: %s", out)
	}
	if !strings.Contains(out, `<parameters>{"command":"pwd"}</parameters>`) {
		t.Fatalf("expected command parameter example for execute_command, got: %s", out)
	}
}

func TestFormatToolSchemaAttentionBlockPrioritizesRequiredFields(t *testing.T) {
	schema := map[string]any{
		"type": "object",
		"required": []any{
			"command",
		},
		"properties": map[string]any{
			"command": map[string]any{"type": "string"},
			"cwd":     map[string]any{"type": "string"},
			"timeout": map[string]any{"type": "integer"},
		},
	}

	out := FormatToolSchemaAttentionBlock("execute_command", "Run a command", schema)
	if !strings.Contains(out, "Tool: execute_command") {
		t.Fatalf("expected tool name in summary, got: %s", out)
	}
	if !strings.Contains(out, "MUST INCLUDE: command") {
		t.Fatalf("expected required field summary, got: %s", out)
	}
	if !strings.Contains(out, "OPTIONAL: cwd, timeout") {
		t.Fatalf("expected optional field summary, got: %s", out)
	}
}

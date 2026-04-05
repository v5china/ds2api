package util

import (
	"encoding/json"
	"fmt"
	"sort"
	"strings"
)

// BuildToolCallInstructions generates the unified tool-calling instruction block
// used by all adapters (OpenAI, Claude, Gemini). It uses attention-optimized
// structure: rules → negative examples → positive examples → anchor.
//
// The toolNames slice should contain the actual tool names available in the
// current request; the function picks real names for examples.
func BuildToolCallInstructions(toolNames []string) string {
	// Pick real tool names for examples; fall back to generic names.
	ex1 := "read_file"
	ex2 := "write_to_file"
	ex3 := "ask_followup_question"
	used := map[string]bool{}
	for _, n := range toolNames {
		switch {
		// Read/query-type tools
		case !used["ex1"] && matchAny(n, "read_file", "list_files", "search_files", "Read", "Glob"):
			ex1 = n
			used["ex1"] = true
		// Write/execute-type tools
		case !used["ex2"] && matchAny(n, "write_to_file", "apply_diff", "execute_command", "exec_command", "Write", "Edit", "MultiEdit", "Bash"):
			ex2 = n
			used["ex2"] = true
		// Interactive/meta tools
		case !used["ex3"] && matchAny(n, "ask_followup_question", "attempt_completion", "update_todo_list", "Task"):
			ex3 = n
			used["ex3"] = true
		}
	}
	ex1Params := exampleReadParams(ex1)
	ex2Params := exampleWriteOrExecParams(ex2)
	ex3Params := exampleInteractiveParams(ex3)

	return `TOOL CALL FORMAT — FOLLOW EXACTLY:

When calling tools, emit ONLY raw XML at the very end of your response. No text before, no text after, no markdown fences.

<tool_calls>
  <tool_call>
    <tool_name>TOOL_NAME_HERE</tool_name>
    <parameters>{"key":"value"}</parameters>
  </tool_call>
</tool_calls>

RULES:
1) Output ONLY the XML above when calling tools. Do NOT mix tool XML with regular text.
2) <parameters> MUST contain a strict JSON object. All JSON keys and strings use double quotes.
3) Multiple tools → multiple <tool_call> blocks inside ONE <tool_calls> root.
4) Do NOT wrap the XML in markdown code fences (no triple backticks).
5) After receiving a tool result, use it directly. Only call another tool if the result is insufficient.
6) Parameters MUST use the exact field names from the selected tool schema.
7) CRITICAL: Do NOT invent or add any extra fields (such as "_raw", "_xml"). Use ONLY the fields strictly defined in the schema. Extra fields will cause execution failure.

ATTENTION CHECKLIST BEFORE YOU EMIT A TOOL CALL:
- Read the tool block above first.
- If the tool block says MUST INCLUDE, every such field must be present.
- If any required field is missing or uncertain, ask a clarifying question instead of guessing.

❌ WRONG — Do NOT do these:
Wrong 1 — mixed text and XML:
  I'll read the file for you. <tool_calls><tool_call>...
Wrong 2 — describing tool calls in text:
  [调用 Bash] {"command": "ls"}
Wrong 3 — empty or missing required parameters:
  <tool_call><tool_name>` + ex1 + `</tool_name><parameters>{}</parameters></tool_call>
Wrong 4 — extra/invented fields:
  <parameters>{"_raw": "...", "command": "ls"}</parameters>


✅ CORRECT EXAMPLES:

Example A — Single tool:
<tool_calls>
  <tool_call>
    <tool_name>` + ex1 + `</tool_name>
    <parameters>` + ex1Params + `</parameters>
  </tool_call>
</tool_calls>

Example B — Two tools in parallel:
<tool_calls>
  <tool_call>
    <tool_name>` + ex1 + `</tool_name>
    <parameters>` + ex1Params + `</parameters>
  </tool_call>
  <tool_call>
    <tool_name>` + ex2 + `</tool_name>
    <parameters>` + ex2Params + `</parameters>
  </tool_call>
</tool_calls>

Example C — Tool with complex nested JSON parameters:
<tool_calls>
  <tool_call>
    <tool_name>` + ex3 + `</tool_name>
    <parameters>` + ex3Params + `</parameters>
  </tool_call>
</tool_calls>

Remember: Output ONLY the <tool_calls>...</tool_calls> XML block when calling tools.`
}

// FormatToolSchemaAttentionBlock renders a compact, attention-friendly tool
// summary for prompt injection. It front-loads required fields so the model can
// spot them before the full format rules and examples.
func FormatToolSchemaAttentionBlock(name, description string, schema any) string {
	lines := make([]string, 0, 4)

	name = strings.TrimSpace(name)
	if name != "" {
		lines = append(lines, "Tool: "+name)
	}

	description = strings.TrimSpace(description)
	if description != "" {
		lines = append(lines, "Description: "+description)
	}

	required, optional := summarizeToolSchemaFields(schema)
	switch {
	case len(required) > 0:
		lines = append(lines, "MUST INCLUDE: "+strings.Join(required, ", "))
		if len(optional) > 0 {
			lines = append(lines, "OPTIONAL: "+strings.Join(optional, ", "))
		}
	case len(optional) > 0:
		lines = append(lines, "FIELDS: "+strings.Join(optional, ", "))
	case schema != nil:
		if b, err := json.Marshal(schema); err == nil && len(b) > 0 {
			lines = append(lines, "Schema: "+string(b))
		}
	}

	return strings.TrimSpace(strings.Join(lines, "\n"))
}

func matchAny(name string, candidates ...string) bool {
	for _, c := range candidates {
		if name == c {
			return true
		}
	}
	return false
}

func exampleReadParams(name string) string {
	switch strings.TrimSpace(name) {
	case "Read":
		return `{"file_path":"README.md"}`
	case "Glob":
		return `{"pattern":"**/*.go","path":"."}`
	default:
		return `{"path":"src/main.go"}`
	}
}

func exampleWriteOrExecParams(name string) string {
	switch strings.TrimSpace(name) {
	case "Bash", "execute_command":
		return `{"command":"pwd"}`
	case "exec_command":
		return `{"cmd":"pwd"}`
	case "Edit":
		return `{"file_path":"README.md","old_string":"foo","new_string":"bar"}`
	case "MultiEdit":
		return `{"file_path":"README.md","edits":[{"old_string":"foo","new_string":"bar"}]}`
	default:
		return `{"path":"output.txt","content":"Hello world"}`
	}
}

func exampleInteractiveParams(name string) string {
	switch strings.TrimSpace(name) {
	case "Task":
		return `{"description":"Investigate flaky tests","prompt":"Run targeted tests and summarize failures"}`
	default:
		return `{"question":"Which approach do you prefer?","follow_up":[{"text":"Option A"},{"text":"Option B"}]}`
	}
}

func summarizeToolSchemaFields(schema any) (required []string, optional []string) {
	obj, ok := schema.(map[string]any)
	if !ok || len(obj) == 0 {
		return nil, nil
	}

	requiredSet := map[string]struct{}{}
	for _, name := range anySliceToStrings(obj["required"]) {
		requiredSet[name] = struct{}{}
	}

	propNames := map[string]struct{}{}
	if props, ok := obj["properties"].(map[string]any); ok {
		for k := range props {
			name := strings.TrimSpace(k)
			if name == "" {
				continue
			}
			propNames[name] = struct{}{}
		}
	}

	required = make([]string, 0, len(requiredSet))
	for name := range requiredSet {
		required = append(required, name)
	}
	sort.Strings(required)

	if len(propNames) == 0 {
		return required, nil
	}

	optional = make([]string, 0, len(propNames))
	for name := range propNames {
		if _, ok := requiredSet[name]; ok {
			continue
		}
		optional = append(optional, name)
	}
	sort.Strings(optional)
	return required, optional
}

func anySliceToStrings(v any) []string {
	switch x := v.(type) {
	case []string:
		out := make([]string, 0, len(x))
		for _, item := range x {
			item = strings.TrimSpace(item)
			if item != "" {
				out = append(out, item)
			}
		}
		return out
	case []any:
		out := make([]string, 0, len(x))
		for _, item := range x {
			s := strings.TrimSpace(fmt.Sprintf("%v", item))
			if s != "" && s != "<nil>" {
				out = append(out, s)
			}
		}
		return out
	default:
		s := strings.TrimSpace(fmt.Sprintf("%v", v))
		if s == "" || s == "<nil>" {
			return nil
		}
		return []string{s}
	}
}

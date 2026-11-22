package session

import (
	"os"
	"path/filepath"
	"testing"
)

func TestToolActivity(t *testing.T) {
	cases := []struct {
		tool  string
		input map[string]any
		want  string
	}{
		{"Bash", map[string]any{"command": "go  test ./...\nmore"}, "bash: go test ./..."},
		{"Bash", map[string]any{"command": "\ncat > f <<'EOF'\nbody\nEOF"}, "bash: cat > f <<'EOF'"},
		{"shell", map[string]any{"command": "pytest -vv"}, "bash: pytest -vv"},
		{"Edit", map[string]any{"file_path": "/a/b/home.go"}, "edit: home.go"},
		{"read", map[string]any{"filePath": "/a/lcd.c"}, "read: lcd.c"},
		{"apply_patch", map[string]any{"patchText": "*** Begin Patch\n*** Update File: /x/src/lcd.c\n@@"}, "edit: lcd.c"},
		{"Grep", map[string]any{"pattern": "TODO"}, "grep: TODO"},
		{"Task", map[string]any{"description": "review the diff"}, "agent: review the diff"},
		{"TodoWrite", map[string]any{"todos": []any{}}, "todos"},
		{"Mystery", nil, "mystery"},
		{"", nil, ""},
	}
	for _, tc := range cases {
		if got := ToolActivity(tc.tool, tc.input); got != tc.want {
			t.Errorf("ToolActivity(%q) = %q, want %q", tc.tool, got, tc.want)
		}
	}
}

// The watcher finds the newest plan and tool call, then reads only what is
// appended, and leaves a half-written last line for the next read.
func TestClaudeTranscriptWatcher(t *testing.T) {
	path := filepath.Join(t.TempDir(), "t.jsonl")
	write := func(content string, appendTo bool) {
		t.Helper()
		flag := os.O_CREATE | os.O_WRONLY | os.O_TRUNC
		if appendTo {
			flag = os.O_CREATE | os.O_WRONLY | os.O_APPEND
		}
		f, err := os.OpenFile(path, flag, 0o600)
		if err != nil {
			t.Fatal(err)
		}
		if _, err := f.WriteString(content); err != nil {
			t.Fatal(err)
		}
		f.Close()
	}
	write(`{"type":"user","message":{"role":"user","content":"go"}}
{"type":"last-prompt","lastPrompt":"make it XC-safe"}
{"type":"assistant","message":{"content":[{"type":"tool_use","name":"ExitPlanMode","input":{"plan":"\n# Make  the rate window XC-safe\n\n## Context"}}]}}
{"type":"user","message":{"content":[{"type":"tool_result","content":"mentions \"tool_use\" in text"}]}}
{"type":"assistant","message":{"content":[{"type":"text","text":"\n## **Now** testing\nmore"},{"type":"tool_use","name":"Bash","input":{"command":"go test ./internal/ui/"}}]}}
`, false)
	var w ClaudeTranscriptWatcher
	if got, want := w.Read(path), (ClaudeLive{Plan: "Make the rate window XC-safe", Activity: "bash: go test ./internal/ui/", Prompt: "make it XC-safe", Reply: "Now testing"}); got != want {
		t.Fatalf("first read = %+v, want %+v", got, want)
	}

	write(`{"type":"assistant","message":{"content":[{"type":"tool_use","name":"Edit","input":{"file_path":"/a/rate.h"}}]}}
{"type":"assistant","message":{"content":[{"type":"tool_use","name":"Read","inp`, true)
	if got, want := w.Read(path), (ClaudeLive{Plan: "Make the rate window XC-safe", Activity: "edit: rate.h", Prompt: "make it XC-safe", Reply: "Now testing"}); got != want {
		t.Fatalf("after append = %+v, want %+v (a partial line must wait)", got, want)
	}
	write(`ut":{"file_path":"/a/next.go"}}]}}
`, true)
	if got := w.Read(path).Activity; got != "read: next.go" {
		t.Fatalf("completed line must be read, got %q", got)
	}
	if got := w.Read(filepath.Join(t.TempDir(), "missing.jsonl")); got != (ClaudeLive{}) {
		t.Fatalf("a missing transcript must give nothing, got %+v", got)
	}
}

func TestReplyLine(t *testing.T) {
	cases := map[string]string{
		"":                                  "",
		"\n\n**Both changes are in.** More": "Both changes are in. More",
		"## Heading\nbody":                  "Heading",
		"- first point\n- second":           "first point",
		"> quoted":                          "quoted",
		"---\nafter rule":                   "after rule",
		"Opened [PR #600: Fix timer](https://x/600) for `lib_xua`": "Opened PR #600: Fix timer for lib_xua",
	}
	for in, want := range cases {
		if got := ReplyLine(in); got != want {
			t.Errorf("ReplyLine(%q) = %q, want %q", in, got, want)
		}
	}
}

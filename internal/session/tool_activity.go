package session

import (
	"bytes"
	"encoding/json"
	"io"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"sync"
)

// ToolActivity describes a tool call in a few words for a session row:
// "bash: go test ./...", "edit: home.go", "grep: TODO". It reads the input
// keys Claude Code and OpenCode use; an unknown tool shows its name alone.
func ToolActivity(tool string, input map[string]any) string {
	name := strings.ToLower(strings.TrimSpace(tool))
	if name == "" {
		return ""
	}
	str := func(keys ...string) string {
		for _, k := range keys {
			if v, ok := input[k].(string); ok {
				if v = strings.Join(strings.Fields(v), " "); v != "" {
					return v
				}
			}
		}
		return ""
	}
	base := func(p string) string {
		if p == "" {
			return ""
		}
		return filepath.Base(p)
	}
	var detail string
	switch name {
	case "bash", "shell":
		// The first line only: a heredoc body or a script is not a summary.
		name = "bash"
		for _, k := range []string{"command", "cmd"} {
			if v, ok := input[k].(string); ok {
				for _, line := range strings.Split(v, "\n") {
					if line = strings.Join(strings.Fields(line), " "); line != "" {
						detail = line
						break
					}
				}
			}
			if detail != "" {
				break
			}
		}
	case "read", "edit", "write", "multiedit", "notebookedit":
		detail = base(str("file_path", "filePath", "notebook_path", "path"))
	case "apply_patch", "patch":
		name, detail = "edit", base(patchFirstFile(str("patchText", "patch")))
	case "grep", "glob", "list", "ls":
		detail = str("pattern", "path")
	case "task", "agent":
		name, detail = "agent", str("description", "prompt")
	case "webfetch":
		detail = str("url")
	case "websearch":
		detail = str("query")
	case "todowrite", "todoread":
		name = "todos"
	case "exitplanmode":
		name = "plan"
	}
	if detail == "" {
		return name
	}
	return name + ": " + detail
}

// patchFirstFile is the first file an apply_patch body names.
func patchFirstFile(patch string) string {
	for _, marker := range []string{"*** Update File: ", "*** Add File: ", "*** Delete File: "} {
		if i := strings.Index(patch, marker); i >= 0 {
			rest := patch[i+len(marker):]
			if j := strings.IndexAny(rest, " *\n"); j >= 0 {
				rest = rest[:j]
			}
			return rest
		}
	}
	return ""
}

// ClaudeLive is what a Claude transcript says a session is doing.
type ClaudeLive struct {
	// Plan is the heading of the newest plan the session presented.
	Plan string
	// Activity is the newest tool call, described by ToolActivity.
	Activity string
	// Prompt is the last prompt the user sent.
	Prompt string
	// Reply is the first line of the agent's newest text reply.
	Reply string
}

// markdownLink matches an inline markdown link, to keep only its text.
var markdownLink = regexp.MustCompile(`\[([^\]]+)\]\([^)]*\)`)

// ReplyLine is the first line of an agent's reply worth showing on a row:
// the first non-empty line, without markdown heading, list, quote, bold and
// code marks, and with links reduced to their text.
func ReplyLine(text string) string {
	for _, line := range strings.Split(text, "\n") {
		line = strings.TrimSpace(line)
		for _, prefix := range []string{"#", "- ", "* ", "> "} {
			for strings.HasPrefix(line, prefix) {
				line = strings.TrimSpace(strings.TrimPrefix(line, prefix))
			}
		}
		line = markdownLink.ReplaceAllString(line, "$1")
		line = strings.NewReplacer("**", "", "`", "").Replace(line)
		if line = strings.Join(strings.Fields(line), " "); line != "" && line != "---" {
			return line
		}
	}
	return ""
}

// claudeFirstScanBytes bounds the first read of a transcript: one can be
// tens of megabytes, and a plan older than this is long out of date.
const claudeFirstScanBytes = 16 << 20

// ClaudeTranscriptWatcher reads Claude transcripts incrementally: each call
// scans only what was appended since the last one, so a poll every couple of
// seconds stays cheap however long the conversation. Safe for concurrent use.
type ClaudeTranscriptWatcher struct {
	mu    sync.Mutex
	files map[string]*claudeTranscriptState
}

type claudeTranscriptState struct {
	offset int64
	live   ClaudeLive
}

// Read returns the newest plan and tool call in the transcript at path.
// A transcript that cannot be read gives what was last seen in it.
func (w *ClaudeTranscriptWatcher) Read(path string) ClaudeLive {
	w.mu.Lock()
	defer w.mu.Unlock()
	if w.files == nil {
		w.files = make(map[string]*claudeTranscriptState)
	}
	st := w.files[path]
	if st == nil {
		st = &claudeTranscriptState{}
		w.files[path] = st
	}
	f, err := os.Open(path) // #nosec G304 -- the deck's own resolved transcript path
	if err != nil {
		return st.live
	}
	defer f.Close()
	info, err := f.Stat()
	if err != nil {
		return st.live
	}
	size := info.Size()
	if size < st.offset { // rewritten or truncated: start over
		*st = claudeTranscriptState{}
	}
	start, skipFirst := st.offset, false
	if start == 0 && size > claudeFirstScanBytes {
		start, skipFirst = size-claudeFirstScanBytes, true
	}
	if start >= size {
		return st.live
	}
	if _, err := f.Seek(start, io.SeekStart); err != nil {
		return st.live
	}
	data, err := io.ReadAll(io.LimitReader(f, size-start))
	if err != nil {
		return st.live
	}
	consumed := int64(0)
	if skipFirst {
		nl := bytes.IndexByte(data, '\n')
		if nl < 0 {
			return st.live
		}
		consumed = int64(nl + 1)
		data = data[nl+1:]
	}
	// Only whole lines: a record still being written is read next time.
	last := bytes.LastIndexByte(data, '\n')
	if last < 0 {
		st.offset = start + consumed
		return st.live
	}
	scanClaudeRecords(data[:last+1], &st.live)
	st.offset = start + consumed + int64(last+1)
	return st.live
}

// scanClaudeRecords folds the plans, tool calls and prompts in a run of
// transcript lines into live, newest last. The prompt comes from Claude's
// own "last-prompt" records, else from the user messages themselves.
func scanClaudeRecords(data []byte, live *ClaudeLive) {
	sawLastPrompt := false
	for _, line := range bytes.Split(data, []byte("\n")) {
		if bytes.Contains(line, []byte(`"last-prompt"`)) {
			var rec struct {
				Type       string `json:"type"`
				LastPrompt string `json:"lastPrompt"`
			}
			if json.Unmarshal(line, &rec) == nil && rec.Type == "last-prompt" && strings.TrimSpace(rec.LastPrompt) != "" {
				live.Prompt, sawLastPrompt = rec.LastPrompt, true
			}
			continue
		}
		if !bytes.Contains(line, []byte(`"assistant"`)) {
			continue
		}
		var rec struct {
			Type    string `json:"type"`
			Message struct {
				Content []struct {
					Type  string         `json:"type"`
					Text  string         `json:"text"`
					Name  string         `json:"name"`
					Input map[string]any `json:"input"`
				} `json:"content"`
			} `json:"message"`
		}
		if json.Unmarshal(line, &rec) != nil || rec.Type != "assistant" {
			continue
		}
		for _, c := range rec.Message.Content {
			if c.Type == "text" {
				if reply := ReplyLine(c.Text); reply != "" {
					live.Reply = reply
				}
				continue
			}
			if c.Type != "tool_use" {
				continue
			}
			if c.Name == "ExitPlanMode" {
				if plan, _ := c.Input["plan"].(string); planHeading(plan) != "" {
					live.Plan = planHeading(plan)
				}
			}
			if a := ToolActivity(c.Name, c.Input); a != "" {
				live.Activity = a
			}
		}
	}
	if !sawLastPrompt {
		if prompt, err := parseClaudeLatestUserPrompt(data); err == nil && strings.TrimSpace(prompt) != "" {
			live.Prompt = prompt
		}
	}
}

// planHeading is a plan's title: its first non-empty line, without the
// markdown heading marks.
func planHeading(plan string) string {
	for _, line := range strings.Split(plan, "\n") {
		line = strings.TrimSpace(strings.TrimLeft(strings.TrimSpace(line), "#"))
		if line = strings.Join(strings.Fields(line), " "); line != "" {
			return line
		}
	}
	return ""
}

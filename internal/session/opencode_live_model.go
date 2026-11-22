package session

import (
	"database/sql"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"strings"

	_ "modernc.org/sqlite"
)

// openCodeDBPath is OpenCode's SQLite store, next to its storage dir:
// $XDG_DATA_HOME/opencode/opencode.db, else ~/.local/share/opencode/opencode.db.
// "" when no home resolves.
func openCodeDBPath() string {
	home, _ := os.UserHomeDir()
	storage := openCodeStorageDir(home)
	if storage == "" {
		return ""
	}
	return filepath.Join(filepath.Dir(storage), "opencode.db")
}

// OpenCodeLive is what OpenCode's own store says a session is doing now.
type OpenCodeLive struct {
	// Model is the session's current model as "provider/model".
	Model string
	// Todo is the step of the agent's todo list it reached last: the one in
	// progress, else the last one completed, else the first one pending.
	Todo string
	// Prompt is the last prompt the user sent.
	Prompt string
	// Activity is the newest tool call, described by ToolActivity.
	Activity string
	// Reply is the first line of the agent's newest text reply.
	Reply string
}

// OpenCodeLiveSessions reads the current model, latest todo step and last
// prompt of each OpenCode session, keyed by OpenCode session ID, from
// OpenCode's SQLite store, read-only. Newer OpenCode versions keep sessions
// in session_v2/session_message and older ones in session/message/part; both
// are tried. A session OpenCode does not know, or with none of the three, is
// absent. A missing store is not an error: OpenCode versions that keep only
// the JSON tree have none.
func OpenCodeLiveSessions(sessionIDs []string) (map[string]OpenCodeLive, error) {
	path := openCodeDBPath()
	if path == "" || len(sessionIDs) == 0 {
		return nil, nil
	}
	if _, err := os.Stat(path); err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return nil, nil
		}
		return nil, err
	}
	db, err := sql.Open("sqlite", "file:"+path+"?mode=ro&_pragma=busy_timeout(2000)&_pragma=query_only(1)")
	if err != nil {
		return nil, err
	}
	defer db.Close()
	db.SetMaxOpenConns(1)

	out := make(map[string]OpenCodeLive, len(sessionIDs))
	for _, id := range sessionIDs {
		if id == "" {
			continue
		}
		live := OpenCodeLive{
			Model:    openCodeSessionModel(db, id),
			Todo:     openCodeString(db, openCodeLatestTodoQuery, id),
			Prompt:   openCodeSessionPrompt(db, id),
			Activity: openCodeSessionActivity(db, id),
			Reply:    ReplyLine(openCodeSessionReply(db, id)),
		}
		if live != (OpenCodeLive{}) {
			out[id] = live
		}
	}
	return out, nil
}

// openCodeLatestTodoQuery picks the step the agent reached last. OpenCode
// rewrites a session's whole list at once, so the rows share one timestamp
// and the status and position say how far it got: the step in progress, else
// the last completed one, else the first still pending. Cancelled steps are
// never shown.
const openCodeLatestTodoQuery = `SELECT content FROM todo
  WHERE session_id = ? AND status IN ('in_progress', 'completed', 'pending')
  ORDER BY CASE status WHEN 'in_progress' THEN 0 WHEN 'completed' THEN 1 ELSE 2 END,
  CASE status WHEN 'completed' THEN -position ELSE position END LIMIT 1`

// openCodeString runs a one-row, one-column query. Query errors read as "not
// known": a store from another OpenCode version lacks some tables and
// columns, and a store mid-write is retried on the next poll.
func openCodeString(db *sql.DB, query, id string) string {
	var value sql.NullString
	if err := db.QueryRow(query, id).Scan(&value); err != nil {
		return ""
	}
	return strings.TrimSpace(value.String)
}

// openCodeSessionModel reads one session's model: the session row's model
// (newer store first), else the model of the session's newest message.
func openCodeSessionModel(db *sql.DB, id string) string {
	for _, query := range []string{
		`SELECT model FROM session_v2 WHERE id = ?`,
		`SELECT model FROM session WHERE id = ?`,
	} {
		var m struct {
			ID         string `json:"id"`
			ProviderID string `json:"providerID"`
		}
		if raw := openCodeString(db, query, id); raw != "" && json.Unmarshal([]byte(raw), &m) == nil && m.ID != "" {
			return joinProviderModel(m.ProviderID, m.ID)
		}
	}
	var provider, model sql.NullString
	err := db.QueryRow(`SELECT coalesce(json_extract(data, '$.providerID'), json_extract(data, '$.model.providerID')),
  coalesce(json_extract(data, '$.modelID'), json_extract(data, '$.model.modelID'))
  FROM message WHERE session_id = ? ORDER BY time_created DESC, id DESC LIMIT 1`, id).Scan(&provider, &model)
	if err != nil || !model.Valid {
		return ""
	}
	return joinProviderModel(provider.String, model.String)
}

// openCodeSessionPrompt reads the text of the session's newest user message.
func openCodeSessionPrompt(db *sql.DB, id string) string {
	if prompt := openCodeString(db, `SELECT json_extract(data, '$.text') FROM session_message
  WHERE session_id = ? AND type = 'user' ORDER BY seq DESC LIMIT 1`, id); prompt != "" {
		return prompt
	}
	return openCodeString(db, `SELECT json_extract(p.data, '$.text') FROM message m JOIN part p ON p.message_id = m.id
  WHERE m.session_id = ? AND json_extract(m.data, '$.role') = 'user' AND json_extract(p.data, '$.type') = 'text'
  AND coalesce(json_extract(p.data, '$.synthetic'), 0) = 0
  ORDER BY m.time_created DESC, m.id DESC, p.id LIMIT 1`, id)
}

// openCodeSessionReply is the text of the session's newest assistant reply:
// from the newest assistant message's content (newer store), else the newest
// text part of an assistant message (older store).
func openCodeSessionReply(db *sql.DB, id string) string {
	if reply := openCodeString(db, `SELECT json_extract(c.value, '$.text') FROM session_message m, json_each(json_extract(m.data, '$.content')) c
  WHERE m.session_id = ? AND m.type = 'assistant' AND json_extract(c.value, '$.type') = 'text'
  AND trim(coalesce(json_extract(c.value, '$.text'), '')) != ''
  ORDER BY m.seq DESC, c.key DESC LIMIT 1`, id); reply != "" {
		return reply
	}
	return openCodeString(db, `SELECT json_extract(p.data, '$.text') FROM message m JOIN part p ON p.message_id = m.id
  WHERE m.session_id = ? AND json_extract(m.data, '$.role') = 'assistant' AND json_extract(p.data, '$.type') = 'text'
  AND coalesce(json_extract(p.data, '$.synthetic'), 0) = 0 AND trim(coalesce(json_extract(p.data, '$.text'), '')) != ''
  ORDER BY m.time_created DESC, m.id DESC, p.id DESC LIMIT 1`, id)
}

// openCodeSessionActivity describes the session's newest tool call: from
// the newest assistant message's content (newer store), else the newest tool
// part (older store).
func openCodeSessionActivity(db *sql.DB, id string) string {
	type call struct {
		Name  string `json:"name"`
		Tool  string `json:"tool"`
		State struct {
			Input map[string]any `json:"input"`
		} `json:"state"`
	}
	describe := func(raw string) string {
		var c call
		if raw == "" || json.Unmarshal([]byte(raw), &c) != nil {
			return ""
		}
		name := c.Name
		if name == "" {
			name = c.Tool
		}
		return ToolActivity(name, c.State.Input)
	}
	if a := describe(openCodeString(db, `SELECT c.value FROM session_message m, json_each(json_extract(m.data, '$.content')) c
  WHERE m.session_id = ? AND m.type = 'assistant' AND json_extract(c.value, '$.type') = 'tool'
  ORDER BY m.seq DESC, c.key DESC LIMIT 1`, id)); a != "" {
		return a
	}
	return describe(openCodeString(db, `SELECT data FROM part WHERE session_id = ? AND json_extract(data, '$.type') = 'tool'
  ORDER BY time_created DESC, id DESC LIMIT 1`, id))
}

func joinProviderModel(provider, model string) string {
	provider, model = strings.TrimSpace(provider), strings.TrimSpace(model)
	if provider == "" || model == "" {
		return model
	}
	return provider + "/" + model
}

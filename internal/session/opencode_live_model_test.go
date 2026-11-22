package session

import (
	"database/sql"
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/require"
)

// OpenCodeLiveSessions reads what an OpenCode session is doing now from
// OpenCode's own store, in both its newer (session_v2/session_message) and
// older (session/message/part) layouts.
func TestOpenCodeLiveSessions(t *testing.T) {
	data := t.TempDir()
	t.Setenv("XDG_DATA_HOME", data)

	live, err := OpenCodeLiveSessions([]string{"ses_row"})
	require.NoError(t, err, "a missing store is not an error")
	require.Empty(t, live)

	require.NoError(t, os.MkdirAll(filepath.Join(data, "opencode"), 0o700))
	db, err := sql.Open("sqlite", "file:"+filepath.Join(data, "opencode", "opencode.db"))
	require.NoError(t, err)
	for _, stmt := range []string{
		`CREATE TABLE session (id text PRIMARY KEY, model text)`,
		`CREATE TABLE message (id text PRIMARY KEY, session_id text NOT NULL, time_created integer NOT NULL, data text NOT NULL)`,
		`CREATE TABLE part (id text PRIMARY KEY, message_id text NOT NULL, data text NOT NULL)`,
		`CREATE TABLE session_v2 (id text PRIMARY KEY, model text)`,
		`CREATE TABLE session_message (id text PRIMARY KEY, session_id text NOT NULL, type text NOT NULL, seq integer NOT NULL, data text NOT NULL)`,
		`CREATE TABLE todo (session_id text NOT NULL, content text NOT NULL, status text NOT NULL, position integer NOT NULL)`,

		// Older layout: the row's model wins over a message's; the newest
		// user message's real text part is the prompt.
		`INSERT INTO session VALUES ('ses_row', '{"id":"gpt-6.1-sol","providerID":"github-copilot","variant":"default"}')`,
		`INSERT INTO message VALUES ('m0', 'ses_row', 1, '{"role":"user"}')`,
		`INSERT INTO part VALUES ('p0', 'm0', '{"type":"text","text":"first ask"}')`,
		`INSERT INTO message VALUES ('m1', 'ses_row', 2, '{"role":"user"}')`,
		`INSERT INTO part VALUES ('p1', 'm1', '{"type":"text","text":"injected","synthetic":true}')`,
		`INSERT INTO part VALUES ('p2', 'm1', '{"type":"text","text":"second ask"}')`,
		`INSERT INTO message VALUES ('m2', 'ses_row', 3, '{"role":"assistant","modelID":"gpt-5.5","providerID":"openai"}')`,
		`INSERT INTO todo VALUES ('ses_row', 'done step', 'completed', 0)`,
		`INSERT INTO todo VALUES ('ses_row', 'run the tests', 'in_progress', 1)`,
		`INSERT INTO todo VALUES ('ses_row', 'later step', 'pending', 2)`,

		// Older layout with no row model: the newest message's model.
		`INSERT INTO session VALUES ('ses_msg', NULL)`,
		`INSERT INTO message VALUES ('m3', 'ses_msg', 1, '{"role":"assistant","modelID":"gpt-5.5","providerID":"openai"}')`,
		`INSERT INTO message VALUES ('m4', 'ses_msg', 2, '{"role":"assistant","modelID":"claude-opus-5.5","providerID":"github-copilot"}')`,

		// Newer layout.
		`INSERT INTO session_v2 VALUES ('ses_v2', '{"id":"gpt-6.1-sol","providerID":"openai","variant":"high"}')`,
		`INSERT INTO session_message VALUES ('s0', 'ses_v2', 'user', 1, '{"text":"old ask"}')`,
		`INSERT INTO session_message VALUES ('s1', 'ses_v2', 'user', 2, '{"text":"is that better or worse?"}')`,
		`INSERT INTO session_message VALUES ('s2', 'ses_v2', 'assistant', 3, '{"content":[]}')`,

		// With nothing in progress the last completed step is the latest;
		// with nothing started, the first pending one. Cancelled never shows.
		`INSERT INTO todo VALUES ('ses_msg', 'step one', 'completed', 0)`,
		`INSERT INTO todo VALUES ('ses_msg', 'step two', 'completed', 1)`,
		`INSERT INTO todo VALUES ('ses_msg', 'step three', 'pending', 2)`,
		`INSERT INTO todo VALUES ('ses_v2', 'dropped', 'cancelled', 0)`,
		`INSERT INTO todo VALUES ('ses_v2', 'first up', 'pending', 1)`,
		`INSERT INTO todo VALUES ('ses_v2', 'second up', 'pending', 2)`,

		`INSERT INTO session VALUES ('ses_empty', NULL)`,
	} {
		_, err := db.Exec(stmt)
		require.NoError(t, err, stmt)
	}
	require.NoError(t, db.Close())

	live, err = OpenCodeLiveSessions([]string{"ses_row", "ses_msg", "ses_v2", "ses_empty", "ses_unknown", ""})
	require.NoError(t, err)
	require.Equal(t, map[string]OpenCodeLive{
		"ses_row": {Model: "github-copilot/gpt-6.1-sol", Todo: "run the tests", Prompt: "second ask"},
		"ses_msg": {Model: "github-copilot/claude-opus-5.5", Todo: "step two"},
		"ses_v2":  {Model: "openai/gpt-6.1-sol", Todo: "first up", Prompt: "is that better or worse?"},
	}, live)
}

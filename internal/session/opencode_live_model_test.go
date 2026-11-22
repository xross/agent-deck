package session

import (
	"database/sql"
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/require"
)

// OpenCodeLiveModels reads the model an OpenCode session is on now from
// OpenCode's own store: the session row's model first, then the newest
// message's, and nothing for a session with neither.
func TestOpenCodeLiveModels(t *testing.T) {
	data := t.TempDir()
	t.Setenv("XDG_DATA_HOME", data)

	models, err := OpenCodeLiveModels([]string{"ses_row"})
	require.NoError(t, err, "a missing store is not an error")
	require.Empty(t, models)

	require.NoError(t, os.MkdirAll(filepath.Join(data, "opencode"), 0o700))
	db, err := sql.Open("sqlite", "file:"+filepath.Join(data, "opencode", "opencode.db"))
	require.NoError(t, err)
	for _, stmt := range []string{
		`CREATE TABLE session (id text PRIMARY KEY, model text)`,
		`CREATE TABLE message (id text PRIMARY KEY, session_id text NOT NULL, time_created integer NOT NULL, data text NOT NULL)`,
		`INSERT INTO session VALUES ('ses_row', '{"id":"gpt-6.1-sol","providerID":"github-copilot","variant":"default"}')`,
		`INSERT INTO session VALUES ('ses_msg', NULL)`,
		`INSERT INTO session VALUES ('ses_user', NULL)`,
		`INSERT INTO session VALUES ('ses_empty', NULL)`,
		// The row's model wins over an older message's.
		`INSERT INTO message VALUES ('m0', 'ses_row', 1, '{"role":"assistant","modelID":"gpt-5.5","providerID":"openai"}')`,
		`INSERT INTO message VALUES ('m1', 'ses_msg', 1, '{"role":"assistant","modelID":"gpt-5.5","providerID":"openai"}')`,
		`INSERT INTO message VALUES ('m2', 'ses_msg', 2, '{"role":"assistant","modelID":"claude-opus-5.5","providerID":"github-copilot"}')`,
		`INSERT INTO message VALUES ('m3', 'ses_user', 1, '{"role":"user","model":{"providerID":"openai","modelID":"gpt-6.1-sol"}}')`,
	} {
		_, err := db.Exec(stmt)
		require.NoError(t, err, stmt)
	}
	require.NoError(t, db.Close())

	models, err = OpenCodeLiveModels([]string{"ses_row", "ses_msg", "ses_user", "ses_empty", "ses_unknown", ""})
	require.NoError(t, err)
	require.Equal(t, map[string]string{
		"ses_row":  "github-copilot/gpt-6.1-sol",
		"ses_msg":  "github-copilot/claude-opus-5.5",
		"ses_user": "openai/gpt-6.1-sol",
	}, models)
}

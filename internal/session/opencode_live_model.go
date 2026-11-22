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

// OpenCodeLiveModels returns the model each OpenCode session is on now, as
// "provider/model", keyed by OpenCode session ID. It reads OpenCode's own
// SQLite store read-only: the session row's model, else the model of the
// session's newest message. A session OpenCode does not know, or one with
// no model yet, is absent. A missing store is not an error: OpenCode
// versions that keep only the JSON tree have none.
func OpenCodeLiveModels(sessionIDs []string) (map[string]string, error) {
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

	out := make(map[string]string, len(sessionIDs))
	for _, id := range sessionIDs {
		if id == "" {
			continue
		}
		if model := openCodeSessionModel(db, id); model != "" {
			out[id] = model
		}
	}
	return out, nil
}

// openCodeSessionModel reads one session's model. Query errors read as "not
// known": an older store has no session.model column, and a store mid-write
// is retried on the next poll.
func openCodeSessionModel(db *sql.DB, id string) string {
	var raw sql.NullString
	if err := db.QueryRow(`SELECT model FROM session WHERE id = ?`, id).Scan(&raw); err == nil && raw.Valid {
		var m struct {
			ID         string `json:"id"`
			ProviderID string `json:"providerID"`
		}
		if json.Unmarshal([]byte(raw.String), &m) == nil && m.ID != "" {
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

func joinProviderModel(provider, model string) string {
	provider, model = strings.TrimSpace(provider), strings.TrimSpace(model)
	if provider == "" || model == "" {
		return model
	}
	return provider + "/" + model
}

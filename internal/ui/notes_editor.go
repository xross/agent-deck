package ui

import (
	"fmt"
	"os"
	"os/exec"
	"strings"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/asheshgoplani/agent-deck/internal/session"
)

// Session notes in an external editor ([preview] notes_editor). The notes go
// to a temporary file, the editor runs on the real terminal while the TUI
// waits, and the file's contents on exit become the notes: quitting without
// saving (:q!) leaves them as they were.

// notesEditorDoneMsg carries the edited notes back to Update.
type notesEditorDoneMsg struct {
	sessionID string
	before    string
	text      string
	err       error
}

// notesEditorCommand is the configured notes editor, "" for the built-in box.
func notesEditorCommand() string {
	cfg, _ := session.LoadUserConfig()
	if cfg == nil {
		return ""
	}
	return strings.TrimSpace(cfg.Preview.NotesEditor)
}

// editNotesInEditor opens notes in editor and reports the result as a
// notesEditorDoneMsg.
func editNotesInEditor(editor, sessionID, notes string) tea.Cmd {
	f, err := os.CreateTemp("", "agent-deck-notes-*.md")
	if err != nil {
		return func() tea.Msg { return notesEditorDoneMsg{sessionID: sessionID, err: err} }
	}
	path := f.Name()
	text := notes
	if text != "" && !strings.HasSuffix(text, "\n") {
		text += "\n"
	}
	_, writeErr := f.WriteString(text)
	closeErr := f.Close()
	if writeErr != nil || closeErr != nil {
		_ = os.Remove(path)
		return func() tea.Msg {
			return notesEditorDoneMsg{sessionID: sessionID, err: fmt.Errorf("write notes file: %v / %v", writeErr, closeErr)}
		}
	}
	// #nosec G204,G702 -- notes_editor is the user's own configured command;
	// the generated filename is a quoted positional argument.
	cmd := exec.Command("sh", "-c", editor+" \"$1\"", "editor", path)
	return execProcessHoldingInput(cmd, func(err error) tea.Msg {
		defer os.Remove(path)
		if err != nil {
			return notesEditorDoneMsg{sessionID: sessionID, err: err}
		}
		data, err := os.ReadFile(path)
		return notesEditorDoneMsg{sessionID: sessionID, before: notes, text: string(data), err: err}
	})
}

// applyNotesFromEditor stores what the editor left as the session's notes.
func (h *Home) applyNotesFromEditor(msg notesEditorDoneMsg) {
	if msg.err != nil {
		h.setError(fmt.Errorf("edit notes: %w", msg.err))
		return
	}
	notes := strings.TrimRight(msg.text, "\n")
	if notes == strings.TrimRight(msg.before, "\n") {
		return
	}
	h.instancesMu.RLock()
	inst := h.instanceByID[msg.sessionID]
	h.instancesMu.RUnlock()
	if inst == nil {
		h.setError(fmt.Errorf("edit notes: the session was removed while its notes were open"))
		return
	}
	inst.Notes = notes
	h.forceSaveInstances()
}

package ui

import (
	"errors"
	"testing"

	"github.com/asheshgoplani/agent-deck/internal/session"
)

// What the external editor leaves in the file becomes the session's notes;
// an editor error or an unchanged file leaves them as they were.
func TestApplyNotesFromEditor(t *testing.T) {
	home := NewHome()
	home.storage = nil
	inst := &session.Instance{ID: "s", Title: "S", Tool: "claude", Notes: "before"}
	home.instanceByID[inst.ID] = inst

	home.applyNotesFromEditor(notesEditorDoneMsg{sessionID: "s", before: "before", text: "- rebase onto develop\n\n"})
	if inst.Notes != "- rebase onto develop" {
		t.Fatalf("notes = %q, want the edited text without trailing newlines", inst.Notes)
	}
	home.applyNotesFromEditor(notesEditorDoneMsg{sessionID: "s", before: inst.Notes, err: errors.New("exit status 1")})
	if inst.Notes != "- rebase onto develop" {
		t.Fatalf("an editor error must leave the notes, got %q", inst.Notes)
	}
}

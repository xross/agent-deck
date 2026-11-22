package ui

import (
	tea "github.com/charmbracelet/bubbletea"

	"github.com/asheshgoplani/agent-deck/internal/session"
)

// statusFilterCycle is the order ] steps through the status filters ([ goes
// back): all, open, running, waiting, idle, stopped, error. Archived stays
// on its own key; stopped has no key of its own.
var statusFilterCycle = []session.Status{
	"",
	FilterModeActive,
	session.StatusRunning,
	session.StatusWaiting,
	session.StatusIdle,
	session.StatusStopped,
	session.StatusError,
}

// cycleStatusFilter moves to the next (dir 1) or previous (dir -1) status
// filter, wrapping round. A filter outside the cycle (archived) steps from
// "all".
func (h *Home) cycleStatusFilter(dir int) tea.Cmd {
	at := 0
	for i, f := range statusFilterCycle {
		if f == h.statusFilter {
			at = i
			break
		}
	}
	n := len(statusFilterCycle)
	next := statusFilterCycle[((at+dir)%n+n)%n]
	if next == h.statusFilter {
		return nil
	}
	return h.changeStatusFilter(next)
}

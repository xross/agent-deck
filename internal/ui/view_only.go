package ui

import (
	"strings"
	"time"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"

	"github.com/asheshgoplani/agent-deck/internal/session"
)

// View-only mode (agent-deck --view-only): a status board for a second
// window. The session list fills the window, with its colours, badges and
// summary lines, and the keys only move, fold, filter and quit: nothing
// attaches to, starts, stops, edits or creates a session.

// viewOnlyKeys are the normalized keys view-only mode still handles.
var viewOnlyKeys = map[string]bool{
	"up": true, "down": true, "k": true, "j": true,
	"left": true, "right": true, "h": true, "l": true, "tab": true,
	"pgup": true, "pgdown": true, "ctrl+u": true, "ctrl+d": true, "ctrl+f": true, "ctrl+b": true,
	"home": true, "end": true, "g": true, "G": true,
	"1": true, "2": true, "3": true, "4": true, "5": true, "6": true, "7": true, "8": true, "9": true,
	"0": true, "%": true, "!": true, "@": true, "#": true, "&": true,
	"q": true, "ctrl+c": true,
}

// DefaultViewOnlyRecent is how far back the board looks for activity when
// deciding which groups to open (agent-deck --view-only --recent).
const DefaultViewOnlyRecent = 48 * time.Hour

// SetViewOnly turns view-only mode on (before the program starts). The
// board opens the groups with a session active within recent (folding the
// rest) and keeps its folds to itself: it saves neither group folds nor UI
// state, so folding here never changes the main window.
func (h *Home) SetViewOnly(on bool, recent time.Duration) {
	h.viewOnly = on
	h.viewOnlyRecent = recent
	if on {
		h.summaryCollapsed = make(map[string]bool)
		h.summaryExpanded = make(map[string]bool)
		h.summaryFoldNewAfter = time.Time{}
	}
}

// openRecentGroups opens each group with a session active within the
// board's window, counting its subgroups' sessions, and folds the rest.
func (h *Home) openRecentGroups(now time.Time) {
	if h.groupTree == nil {
		return
	}
	since := now.Add(-h.viewOnlyRecent)
	recent := make(map[string]bool)
	for path, g := range h.groupTree.Groups {
		for _, inst := range g.Sessions {
			if inst != nil && !session.LastActiveAt(inst).Before(since) {
				for p := path; ; {
					recent[p] = true
					i := strings.LastIndex(p, "/")
					if i < 0 {
						break
					}
					p = p[:i]
				}
				break
			}
		}
	}
	for path := range h.groupTree.Groups {
		if recent[path] {
			h.groupTree.ExpandGroup(path)
		} else {
			h.groupTree.CollapseGroup(path)
		}
	}
}

// viewOnlyAllows reports whether view-only mode lets a normalized key
// through. Outside view-only mode every key is allowed.
func (h *Home) viewOnlyAllows(key string) bool {
	return !h.viewOnly || viewOnlyKeys[key]
}

// viewOnlyAllowsMouse lets only the scroll wheel through in view-only mode:
// a click selects and a double click attaches.
func (h *Home) viewOnlyAllowsMouse(msg tea.MouseMsg) bool {
	if !h.viewOnly {
		return true
	}
	return msg.Button == tea.MouseButtonWheelUp || msg.Button == tea.MouseButtonWheelDown
}

// renderViewOnlyHelpBar is the footer in view-only mode: the same two lines
// as the normal bar, naming the keys that still work.
func (h *Home) renderViewOnlyHelpBar() string {
	border := lipgloss.NewStyle().Foreground(ColorBorder).Render(strings.Repeat("─", max(0, h.width)))
	label := lipgloss.NewStyle().Foreground(ColorAccent).Bold(true).Render("VIEW ONLY")
	hint := lipgloss.NewStyle().Foreground(ColorComment).Render("  ↑↓ move · ←→ fold · % filter · q quit")
	content := " " + label + hint
	return lipgloss.NewStyle().MaxWidth(h.width).Render(lipgloss.JoinVertical(lipgloss.Left, border, content))
}

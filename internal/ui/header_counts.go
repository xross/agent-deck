package ui

import (
	"fmt"

	"github.com/charmbracelet/lipgloss"

	"github.com/asheshgoplani/agent-deck/internal/session"
)

// Header counts for open groups ([display] header_counts_visible): the
// header's per-status counts cover only sessions whose groups are all
// expanded, with the overall total in brackets when it differs.

// statusCounts is one count per header status bucket.
type statusCounts struct {
	running, waiting, idle, stopped, errored int
}

// groupOpen reports whether a group and every group above it are expanded.
func (h *Home) groupOpen(path string) bool {
	if h.groupTree == nil {
		return true
	}
	if path == "" {
		path = session.DefaultGroupPath
	}
	for p := path; p != ""; p = parentGroupPath(p) {
		if g := h.groupTree.Groups[p]; g != nil && !g.Expanded {
			return false
		}
	}
	return true
}

func parentGroupPath(p string) string {
	for i := len(p) - 1; i >= 0; i-- {
		if p[i] == '/' {
			return p[:i]
		}
	}
	return ""
}

// countVisibleSessionStatuses counts the local, unarchived sessions in open
// groups by status bucket. Remote sessions are not counted here (their folds
// live with their remote), so they show only in the bracketed totals.
func (h *Home) countVisibleSessionStatuses() statusCounts {
	var c statusCounts
	snapshot := h.getSessionRenderSnapshot()
	h.instancesMu.RLock()
	defer h.instancesMu.RUnlock()
	for id, state := range snapshot {
		inst := h.instanceByID[id]
		if state.archived || inst == nil || !h.groupOpen(inst.GroupPath) {
			continue
		}
		switch statusBucket(state.status) {
		case session.StatusRunning:
			c.running++
		case session.StatusWaiting:
			c.waiting++
		case session.StatusIdle:
			c.idle++
		case session.StatusStopped:
			c.stopped++
		case session.StatusError:
			c.errored++
		}
	}
	return c
}

// headerCount formats one header count: the visible count, then the total in
// brackets when the two differ.
func headerCount(visible, total int) string {
	if visible == total {
		return fmt.Sprintf("%d", total)
	}
	return fmt.Sprintf("%d (%d)", visible, total)
}

// headerStat renders one header status count: the symbol and visible count
// bold in the status colour, so they stand out, the total (when it differs)
// in brackets in a darker shade of that colour, then the word.
func headerStat(color lipgloss.Color, symbol string, visible, total int, word string) string {
	style := lipgloss.NewStyle().Foreground(color)
	out := style.Bold(true).Render(fmt.Sprintf("%s %d", symbol, visible))
	if visible != total {
		out += lipgloss.NewStyle().Foreground(darkenTowardBg(color, 0.5)).Render(fmt.Sprintf(" (%d)", total))
	}
	return out + style.Render(" "+word)
}

// darkenTowardBg mixes a "#rrggbb" colour toward the theme background by
// amount (0 none, 1 all the way), giving a darker (on a light theme, paler)
// shade of the same hue. A colour that is not "#rrggbb" comes back as is.
func darkenTowardBg(c lipgloss.Color, amount float64) lipgloss.Color {
	r1, g1, b1, ok1 := parseHexColor(string(c))
	r2, g2, b2, ok2 := parseHexColor(string(ColorBg))
	if !ok1 || !ok2 {
		return c
	}
	mix := func(a, b int) int { return int(float64(a) + (float64(b)-float64(a))*amount + 0.5) }
	return lipgloss.Color(fmt.Sprintf("#%02x%02x%02x", mix(r1, r2), mix(g1, g2), mix(b1, b2)))
}

func parseHexColor(s string) (r, g, b int, ok bool) {
	if len(s) != 7 || s[0] != '#' {
		return 0, 0, 0, false
	}
	if _, err := fmt.Sscanf(s[1:], "%02x%02x%02x", &r, &g, &b); err != nil {
		return 0, 0, 0, false
	}
	return r, g, b, true
}

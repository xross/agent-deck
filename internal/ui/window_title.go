package ui

import (
	"strings"
	"time"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/asheshgoplani/agent-deck/internal/session"
)

// The TUI's own window title ([display] dashboard_title). An attached session
// or an external editor retitles the window while it owns the terminal, so
// the title is set at start and re-sent on a slow tick (the event loop is
// paused while another program holds the terminal, so the tick never fires
// over it).

const windowTitleInterval = 3 * time.Second

type windowTitleTickMsg struct{}

// dashboardWindowTitle is the configured title, "" to leave the window alone.
func dashboardWindowTitle() string {
	cfg, _ := session.LoadUserConfig()
	if cfg == nil {
		return ""
	}
	return strings.TrimSpace(cfg.Display.DashboardTitle)
}

// windowTitleCmd sets the title now and arms the next refresh.
func windowTitleCmd() tea.Cmd {
	next := tea.Tick(windowTitleInterval, func(time.Time) tea.Msg { return windowTitleTickMsg{} })
	if title := dashboardWindowTitle(); title != "" {
		return tea.Batch(tea.SetWindowTitle(title), next)
	}
	return next
}

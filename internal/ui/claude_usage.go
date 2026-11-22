package ui

import (
	"fmt"
	"strings"
	"time"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"

	"github.com/asheshgoplani/agent-deck/internal/quota"
	"github.com/asheshgoplani/agent-deck/internal/session"
	"github.com/asheshgoplani/agent-deck/internal/sysinfo"
)

// Claude plan usage in the header ([display] show_claude_usage): the 5h and
// 7d windows from the quota cache `agent-deck usage` reads, which Claude's
// statusLine keeps current whenever a Claude session is active. Read off the
// event loop on a slow tick; the header renders the cached snapshot.

const claudeUsagePollInterval = 15 * time.Second

type claudeUsageTickMsg struct{}

type claudeUsageLoadedMsg struct {
	snapshot *quota.Snapshot
}

// claudeUsagePollCmd reads the Claude snapshot now (when the option is on)
// and arms the next read.
func (h *Home) claudeUsagePollCmd() tea.Cmd {
	next := tea.Tick(claudeUsagePollInterval, func(time.Time) tea.Msg { return claudeUsageTickMsg{} })
	cfg, _ := session.LoadUserConfig()
	if cfg == nil || !cfg.Display.ShowClaudeUsage {
		// Switched off: clear the line too.
		return tea.Batch(next, func() tea.Msg { return claudeUsageLoadedMsg{} })
	}
	profile := h.profile
	return tea.Batch(next, func() tea.Msg {
		resolved, err := session.ResolveProfileForStorage(profile)
		if err != nil {
			return claudeUsageLoadedMsg{}
		}
		store, err := quota.NewStore(resolved)
		if err != nil {
			return claudeUsageLoadedMsg{}
		}
		snapshots, err := store.Load()
		if err != nil {
			return claudeUsageLoadedMsg{}
		}
		for i := range snapshots {
			if snapshots[i].ID == quota.ProviderClaude && snapshots[i].Error == "" {
				return claudeUsageLoadedMsg{snapshot: &snapshots[i]}
			}
		}
		return claudeUsageLoadedMsg{}
	})
}

// claudeUsageLine renders the usage line under the header, "" when the
// option is off or nothing has been recorded yet:
//
//	Claude  5h 4% used · resets in 4h29m   7d 8% used · resets in 4d3h
//
// Each percentage is coloured by how much is used (green, then yellow from
// 50%, red from 80%); a window whose reset has passed shows "–", and an old
// reading is dimmed.
func (h *Home) claudeUsageLine(now time.Time) string {
	snapshot := h.claudeUsage
	if snapshot == nil {
		return ""
	}
	label := lipgloss.NewStyle().Foreground(ColorOrange).Bold(true)
	if snapshot.Stale {
		label = DimStyle
	}
	var parts []string
	for _, w := range snapshot.Windows {
		if w.Kind != quota.WindowFiveHour && w.Kind != quota.WindowSevenDay {
			continue
		}
		if w.Expired(now) {
			parts = append(parts, DimStyle.Render(w.Label+" –"))
			continue
		}
		style := DimStyle
		switch {
		case snapshot.Stale:
		case w.UsedPercentage >= 80:
			style = lipgloss.NewStyle().Foreground(ColorRed).Bold(true)
		case w.UsedPercentage >= 50:
			style = lipgloss.NewStyle().Foreground(ColorYellow).Bold(true)
		default:
			style = lipgloss.NewStyle().Foreground(ColorGreen).Bold(true)
		}
		part := DimStyle.Render(w.Label+" ") + style.Render(fmt.Sprintf("%.0f%%", w.UsedPercentage)) + DimStyle.Render(" used")
		if w.ResetsAt != nil {
			part += DimStyle.Render(" · resets in " + untilString(time.Unix(*w.ResetsAt, 0).Sub(now)))
		}
		parts = append(parts, part)
	}
	if len(parts) == 0 {
		return ""
	}
	line := " " + label.Render("Claude") + "  " + strings.Join(parts, "   ")
	if snapshot.Stale {
		line += DimStyle.Render("   (last reading " + humanizeSince(now.Sub(time.Unix(snapshot.UpdatedAt, 0))) + ")")
	}
	return lipgloss.NewStyle().MaxWidth(max(1, h.width)).Render(line)
}

// hostStatsSegment renders the CPU/memory/disk stats the header shows, ""
// when the header fields or the collector leave them out or the welcome
// screen is up (ASK-211).
func (h *Home) hostStatsSegment(headerFieldSet map[string]bool) string {
	wants := headerFieldSet[session.PreviewFieldLoad] || headerFieldSet[session.PreviewFieldMemory] || headerFieldSet[session.PreviewFieldDisk]
	if !wants || h.sysStatsCollector == nil || h.welcomeActive() {
		return ""
	}
	stats := headerSysStatsSnapshot(h.sysStatsCollector)
	formatted := sysinfo.Format(stats, h.sysStatsConfig.GetFormat(), h.sysStatsConfig.GetShow())
	if formatted == "" {
		return ""
	}
	return lipgloss.NewStyle().Foreground(ColorComment).Render(formatted)
}

// headerFieldSetFromConfig is the header's field set ([ui.header].fields).
func headerFieldSetFromConfig() map[string]bool {
	fields := session.DefaultHeaderFields
	if cfg, err := session.LoadUserConfig(); err == nil && cfg != nil {
		fields = cfg.UI.GetHeaderFields()
	}
	set := make(map[string]bool, len(fields))
	for _, f := range fields {
		set[f] = true
	}
	return set
}

// hostStatsOnOwnLine reports whether [display] host_stats_line is on.
func hostStatsOnOwnLine() bool {
	cfg, _ := session.LoadUserConfig()
	return cfg != nil && cfg.Display.HostStatsLine
}

// hostStatsLine renders the host stats on their own line under the header
// ([display] host_stats_line), "" when that is off or there are none.
func (h *Home) hostStatsLine() string {
	if !hostStatsOnOwnLine() {
		return ""
	}
	stats := h.hostStatsSegment(headerFieldSetFromConfig())
	if stats == "" {
		return ""
	}
	return lipgloss.NewStyle().MaxWidth(max(1, h.width)).Render(" " + stats)
}

// headerExtraLines are the lines under the header, in order: host stats,
// then Claude usage.
func (h *Home) headerExtraLines(now time.Time) []string {
	var lines []string
	for _, line := range []string{h.hostStatsLine(), h.claudeUsageLine(now)} {
		if line != "" {
			lines = append(lines, line)
		}
	}
	return lines
}

// claudeUsageLineHeight is the rows the lines under the header take.
func (h *Home) claudeUsageLineHeight() int {
	return len(h.headerExtraLines(time.Now()))
}

// untilString renders a time still to come compactly ("4h 29m", "3d 2h"),
// with "under 1m" for less than a minute.
func untilString(d time.Duration) string {
	if d < time.Minute {
		return "under 1m"
	}
	return strings.TrimSuffix(humanizeSince(d), " ago")
}

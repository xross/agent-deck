package ui

import (
	"strings"
	"time"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"

	"github.com/asheshgoplani/agent-deck/internal/session"
	"github.com/asheshgoplani/agent-deck/internal/statedb"
)

// Model row badge ([display] show_session_model).
//
// A session's launch model goes stale the moment someone switches model
// inside the tool, so the badge prefers what the tool itself last recorded:
// for Claude the model its status line reported (the claude_statuslines
// table that `usage statusline-wrap` feeds), for OpenCode the session's
// model in OpenCode's own store. A session with neither, or on another
// tool, falls back to the model it was launched with; with none it shows
// nothing.
//
// The reads happen inside a tea.Cmd on a slow tick; Update only swaps in the
// result and rows render from the cached map.

const modelBadgePollInterval = 5 * time.Second

// modelBadgeFeed is where the poll reads live models from. The default
// reads the profile's state DB and OpenCode's store; tests swap in a fake.
type modelBadgeFeed interface {
	// Models returns the live model ID per agent-deck session ID. openCode
	// maps agent-deck session ID to OpenCode session ID for the OpenCode
	// sessions to look up.
	Models(openCode map[string]string) (map[string]string, error)
}

type stateDBModelBadgeFeed struct{}

func (stateDBModelBadgeFeed) Models(openCode map[string]string) (map[string]string, error) {
	db := statedb.GetGlobal()
	if db == nil {
		return nil, errTriggersNoDB
	}
	records, err := db.ClaudeStatuslineModels()
	if err != nil {
		return nil, err
	}
	out := make(map[string]string, len(records)+len(openCode))
	for id, m := range records {
		if m.ID != "" {
			out[id] = m.ID
		} else {
			out[id] = m.DisplayName
		}
	}
	// OpenCode's store belongs to OpenCode: a failed read there leaves those
	// rows on their launch model and must not cost the Claude rows theirs.
	nativeIDs := make([]string, 0, len(openCode))
	for _, nativeID := range openCode {
		nativeIDs = append(nativeIDs, nativeID)
	}
	if live, err := session.OpenCodeLiveModels(nativeIDs); err == nil {
		for id, nativeID := range openCode {
			if model := live[nativeID]; model != "" {
				out[id] = model
			}
		}
	}
	return out, nil
}

type modelBadgeTickMsg struct{ gen int }

type modelBadgePolledMsg struct {
	gen    int
	models map[string]string
	err    error
}

// openCodeSessionIDs snapshots the OpenCode sessions' native IDs on the
// Update goroutine, so the background read never touches an Instance.
func (h *Home) openCodeSessionIDs() map[string]string {
	h.instancesMu.RLock()
	defer h.instancesMu.RUnlock()
	out := make(map[string]string)
	for _, inst := range h.instances {
		if inst != nil && inst.Tool == "opencode" && inst.OpenCodeSessionID != "" {
			out[inst.ID] = inst.OpenCodeSessionID
		}
	}
	return out
}

// modelBadgePollCmd reads the live models now. With the badge switched off
// it only re-arms the tick, so turning the option on needs no restart.
func (h *Home) modelBadgePollCmd() tea.Cmd {
	feed := h.modelBadgeFeed
	if feed == nil {
		return nil
	}
	if !h.showSessionModel {
		return h.modelBadgeTick()
	}
	gen := h.modelBadgePollGen
	openCode := h.openCodeSessionIDs()
	return func() tea.Msg {
		models, err := feed.Models(openCode)
		return modelBadgePolledMsg{gen: gen, models: models, err: err}
	}
}

func (h *Home) modelBadgeTick() tea.Cmd {
	gen := h.modelBadgePollGen
	return tea.Tick(modelBadgePollInterval, func(time.Time) tea.Msg { return modelBadgeTickMsg{gen: gen} })
}

// restartModelBadgePoll starts a fresh poll loop now, for when the session
// list was just (re)loaded: the labels resolve against that list, and a
// poll that ran before it arrived knew no OpenCode session to look up. The
// old loop's pending tick is dropped by its generation.
func (h *Home) restartModelBadgePoll() tea.Cmd {
	if h.modelBadgeFeed == nil {
		return nil
	}
	h.modelBadgePollGen++
	h.rebuildModelLabels()
	return h.modelBadgePollCmd()
}

// handleModelBadgeMsg is Update's arm for the poll loop messages. A failed
// read keeps the last live models, so one busy DB does not blank every badge.
func (h *Home) handleModelBadgeMsg(msg tea.Msg) tea.Cmd {
	switch msg := msg.(type) {
	case modelBadgeTickMsg:
		if msg.gen != h.modelBadgePollGen {
			return nil
		}
		return h.modelBadgePollCmd()
	case modelBadgePolledMsg:
		if msg.err == nil {
			h.liveModels = msg.models
		}
		h.rebuildModelLabels()
		if msg.gen != h.modelBadgePollGen {
			return nil
		}
		return h.modelBadgeTick()
	}
	return nil
}

// rebuildModelLabels resolves every local session's row label: the live
// model when the status line reported one, else the launch model. It runs on
// the Update goroutine (launch options are not guarded by Instance.mu), so
// rows only do a map lookup.
func (h *Home) rebuildModelLabels() {
	h.instancesMu.RLock()
	defer h.instancesMu.RUnlock()
	labels := make(map[string]string, len(h.instances))
	for _, inst := range h.instances {
		if inst == nil {
			continue
		}
		model := h.liveModels[inst.ID]
		if model == "" {
			model = inst.LaunchModelID()
		}
		if label := shortModelLabel(model); label != "" {
			labels[inst.ID] = label
		}
	}
	h.modelLabels = labels
}

// sessionModelLabel is the row's model label, "" when the badge is off or
// the session's model is not known.
func (h *Home) sessionModelLabel(inst *session.Instance) string {
	if !h.showSessionModel || inst == nil {
		return ""
	}
	return h.modelLabels[inst.ID]
}

// shortModelLabel turns a model ID into a compact lowercase row label:
// "claude-opus-5-5" is "opus 5.5", "openai/gpt-5.5" is "gpt 5.5". It drops
// the provider, the "claude-" prefix, a "[1m]"-style suffix and a trailing
// release date, and joins a run of numeric parts with dots.
func shortModelLabel(modelID string) string {
	id := strings.ToLower(strings.TrimSpace(modelID))
	if _, base, ok := strings.Cut(id, "/"); ok && strings.TrimSpace(base) != "" {
		id = strings.TrimSpace(base)
	}
	if open := strings.IndexAny(id, "[("); open > 0 {
		id = strings.TrimSpace(id[:open])
	}
	id = strings.TrimPrefix(id, "claude-")
	parts := strings.FieldsFunc(id, func(r rune) bool { return r == '-' || r == ' ' || r == '_' })
	if n := len(parts); n > 1 && len(parts[n-1]) == 8 && isDigits(parts[n-1]) {
		parts = parts[:n-1]
	}
	var b strings.Builder
	for i, part := range parts {
		if i > 0 {
			if isDigits(part) && isDigits(parts[i-1]) {
				b.WriteByte('.')
			} else {
				b.WriteByte(' ')
			}
		}
		b.WriteString(part)
	}
	return b.String()
}

func isDigits(s string) bool {
	if s == "" {
		return false
	}
	for _, r := range s {
		if r < '0' || r > '9' {
			return false
		}
	}
	return true
}

// fitModelBadge renders the "[model]" row badge within budget cells
// (negative means unbounded). It is dropped whole when it does not fit, so
// the title keeps its floor (#2201). Purple sets it apart from the tool
// badge's brand colour and the cyan of the worktree and sandbox badges; like
// the tool badge it keeps its colour on a selected row.
func fitModelBadge(label string, budget int) (string, int) {
	if label == "" {
		return "", 0
	}
	badge := " [" + label + "]"
	width := cellWidth(badge)
	if budget >= 0 && width > budget {
		return "", 0
	}
	return lipgloss.NewStyle().Foreground(ColorPurple).Render(badge), width
}

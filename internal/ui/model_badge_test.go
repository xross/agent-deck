package ui

import (
	"errors"
	"strings"
	"testing"

	"github.com/asheshgoplani/agent-deck/internal/session"
)

// Model row badge ([display] show_session_model): the row shows the model
// the session is on now, taken from the status line poll, and falls back to
// the launch model only when no live model is known.

func TestShortModelLabel(t *testing.T) {
	cases := []struct{ id, want string }{
		{"", ""},
		{"claude-opus-5-5", "opus 5.5"},
		{"claude-fable-5-2", "fable 5.2"},
		{"claude-fable-5", "fable 5"},
		{"claude-opus-4-20250514", "opus 4"},
		{"claude-3-5-sonnet-20241022", "3.5 sonnet"},
		{"claude-opus-5-5[1m]", "opus 5.5"},
		{"opus", "opus"},
		{"openai/gpt-5.5", "gpt 5.5"},
		{"gpt-5-codex", "gpt 5 codex"},
		{"gemini-3.1-pro-preview", "gemini 3.1 pro preview"},
		{"Opus 5.5 (1M context)", "opus 5.5"},
	}
	for _, tc := range cases {
		if got := shortModelLabel(tc.id); got != tc.want {
			t.Errorf("shortModelLabel(%q) = %q, want %q", tc.id, got, tc.want)
		}
	}
}

func TestFitModelBadge(t *testing.T) {
	forceTrueColorProfile()
	if text, width := fitModelBadge("", -1); text != "" || width != 0 {
		t.Errorf("empty label must render nothing, got %q (%d)", text, width)
	}
	if text, width := fitModelBadge("opus 5.5", -1); !strings.Contains(text, " [opus 5.5]") || width != 11 {
		t.Errorf("unbounded badge = %q (%d), want \" [opus 5.5]\" at 11 cells", text, width)
	}
	if text, width := fitModelBadge("opus 5.5", 11); text == "" || width != 11 {
		t.Errorf("badge must fit an exact budget, got %q (%d)", text, width)
	}
	if text, width := fitModelBadge("opus 5.5", 10); text != "" || width != 0 {
		t.Errorf("badge over budget must be dropped whole, got %q (%d)", text, width)
	}
}

type fakeModelBadgeFeed struct {
	models   map[string]string
	err      error
	calls    int
	openCode map[string]string
}

func (f *fakeModelBadgeFeed) Models(openCode map[string]string) (map[string]string, error) {
	f.calls++
	f.openCode = openCode
	return f.models, f.err
}

// renderRowWithModel renders one claude session row (launched on opus) after
// a poll that returned live.
func renderRowWithModel(t *testing.T, show bool, live map[string]string, listWidth int) string {
	t.Helper()
	forceTrueColorProfile()

	inst := &session.Instance{ID: "sess-model", Title: "with-model", Tool: "claude"}
	if err := inst.SetClaudeOptions(&session.ClaudeOptions{Model: "claude-opus-5-5"}); err != nil {
		t.Fatalf("SetClaudeOptions: %v", err)
	}
	h := &Home{
		width:            140,
		showSessionModel: show,
		instances:        []*session.Instance{inst},
		modelBadgeFeed:   &fakeModelBadgeFeed{models: live},
	}
	h.handleModelBadgeMsg(modelBadgePolledMsg{models: live})

	item := session.Item{Type: session.ItemTypeSession, Session: inst, Level: 1, Path: "test", IsLastInGroup: true}
	snapshot := map[string]sessionRenderState{inst.ID: {status: session.StatusRunning, tool: "claude", title: inst.Title}}
	var b strings.Builder
	h.renderSessionItem(&b, item, false, snapshot, listWidth)
	return stripAnsi(b.String())
}

func TestModelBadge_LiveModelWinsOverLaunchModel(t *testing.T) {
	row := renderRowWithModel(t, true, map[string]string{"sess-model": "claude-fable-5-2"}, 140)
	if !strings.Contains(row, "claude [fable 5.2]") {
		t.Fatalf("row must show the live model after the tool badge, got %q", row)
	}
	if strings.Contains(row, "opus") {
		t.Fatalf("row must not show the stale launch model, got %q", row)
	}
}

func TestModelBadge_FallsBackToLaunchModel(t *testing.T) {
	row := renderRowWithModel(t, true, nil, 140)
	if !strings.Contains(row, "claude [opus 5.5]") {
		t.Fatalf("row with no live model must show the launch model, got %q", row)
	}
}

func TestModelBadge_OffByDefault(t *testing.T) {
	row := renderRowWithModel(t, false, map[string]string{"sess-model": "claude-fable-5-2"}, 140)
	if strings.Contains(row, "fable") || strings.Contains(row, "opus") {
		t.Fatalf("show_session_model=false must not render a model badge, got %q", row)
	}
}

func TestModelBadge_DroppedBeforeTitleOnNarrowList(t *testing.T) {
	row := renderRowWithModel(t, true, map[string]string{"sess-model": "claude-fable-5-2"}, 30)
	if strings.Contains(row, "fable") {
		t.Fatalf("a narrow list must drop the model badge, got %q", row)
	}
	if !strings.Contains(row, "with-model") {
		t.Fatalf("a narrow list must keep the title, got %q", row)
	}
}

// A failed poll keeps the last live models; the option being off skips the
// read but keeps the loop alive so turning it on needs no restart.
func TestModelBadge_PollLoop(t *testing.T) {
	feed := &fakeModelBadgeFeed{models: map[string]string{"a": "claude-opus-5-5"}}
	h := &Home{modelBadgeFeed: feed, instances: []*session.Instance{
		{ID: "oc", Tool: "opencode", OpenCodeSessionID: "ses_1"},
		{ID: "oc-unbound", Tool: "opencode"},
		{ID: "cl", Tool: "claude"},
	}}

	if cmd := h.modelBadgePollCmd(); cmd == nil {
		t.Fatal("poll with the badge off must still re-arm the tick")
	}
	if feed.calls != 0 {
		t.Fatalf("poll with the badge off must not read the feed, got %d reads", feed.calls)
	}

	h.showSessionModel = true
	msg := h.modelBadgePollCmd()()
	if feed.calls != 1 {
		t.Fatalf("poll with the badge on must read the feed once, got %d reads", feed.calls)
	}
	if len(feed.openCode) != 1 || feed.openCode["oc"] != "ses_1" {
		t.Fatalf("poll must pass only bound OpenCode sessions to the feed, got %v", feed.openCode)
	}
	if cmd := h.handleModelBadgeMsg(msg); cmd == nil {
		t.Fatal("a polled message must schedule the next tick")
	}
	if h.liveModels["a"] != "claude-opus-5-5" {
		t.Fatalf("polled models must be installed, got %v", h.liveModels)
	}

	h.handleModelBadgeMsg(modelBadgePolledMsg{err: errors.New("database is locked")})
	if h.liveModels["a"] != "claude-opus-5-5" {
		t.Fatalf("a failed poll must keep the last live models, got %v", h.liveModels)
	}

	// A session-list reload restarts the loop at once and retires the old one.
	before := feed.calls
	restart := h.restartModelBadgePoll()
	if restart == nil {
		t.Fatal("a restart must poll now")
	}
	restart()
	if feed.calls != before+1 {
		t.Fatalf("a restart must read the feed at once, got %d reads", feed.calls-before)
	}
	if cmd := h.handleModelBadgeMsg(modelBadgeTickMsg{gen: h.modelBadgePollGen - 1}); cmd != nil {
		t.Fatal("a tick from the retired loop must not poll")
	}
	if cmd := h.handleModelBadgeMsg(modelBadgePolledMsg{gen: h.modelBadgePollGen - 1}); cmd != nil {
		t.Fatal("a result from the retired loop must not re-arm a tick")
	}
}

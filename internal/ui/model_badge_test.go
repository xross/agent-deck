package ui

import (
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/charmbracelet/lipgloss"

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
	todos    map[string]string
	err      error
	calls    int
	openCode map[string]string
}

func (f *fakeModelBadgeFeed) Poll(targets modelBadgeTargets) (modelBadgePoll, error) {
	f.calls++
	f.openCode = targets.openCode
	return modelBadgePoll{models: f.models, todos: f.todos}, f.err
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
	h.handleModelBadgeMsg(modelBadgePolledMsg{poll: modelBadgePoll{models: live}})

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

// A failed poll keeps the last live models, and a session-list reload
// restarts the loop.
func TestModelBadge_PollLoop(t *testing.T) {
	feed := &fakeModelBadgeFeed{
		models: map[string]string{"a": "claude-opus-5-5"},
		todos:  map[string]string{"oc": "run the tests"},
	}
	h := &Home{modelBadgeFeed: feed, instances: []*session.Instance{
		{ID: "oc", Tool: "opencode", OpenCodeSessionID: "ses_1"},
		{ID: "oc-unbound", Tool: "opencode"},
		{ID: "cl", Tool: "claude"},
	}}

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
	if h.liveTodos["oc"] != "run the tests" {
		t.Fatalf("polled todos must be installed, got %v", h.liveTodos)
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

func summaryRow(t *testing.T, h *Home, inst *session.Instance, state sessionRenderState) string {
	t.Helper()
	forceTrueColorProfile()
	item := session.Item{Type: session.ItemTypeSession, Session: inst, Level: 1, Path: "test", IsLastInGroup: true}
	var b strings.Builder
	h.renderSessionItem(&b, item, false, map[string]sessionRenderState{inst.ID: state}, h.width)
	return stripAnsi(b.String())
}

// Claude and OpenCode rows share one summary shape: doing (while running),
// prompt, then the plan or todo step. A stale pane title is replaced.
func TestSessionSummary_SingleLine(t *testing.T) {
	claude := &session.Instance{ID: "cl", Title: "General", Tool: "claude"}
	oc := &session.Instance{ID: "oc", Title: "development", Tool: "opencode"}
	h := &Home{width: 220, showPaneTitles: true,
		livePlans:      map[string]string{"cl": "Make the rate window XC-safe"},
		liveTodos:      map[string]string{"oc": "run the tests"},
		livePrompts:    map[string]string{"oc": "is that better?"},
		liveActivities: map[string]string{"cl": "bash: go test ./...", "oc": "edit: lcd.c"},
	}
	cases := []struct {
		name  string
		inst  *session.Instance
		state sessionRenderState
		want  string
		not   string
	}{
		{"claude idle", claude, sessionRenderState{status: session.StatusIdle, tool: "claude", title: "General", paneTitle: "General", latestPrompt: "fix the build"},
			"prompt: fix the build · plan: Make the rate window XC-safe", "doing:"},
		{"claude running keeps the prompt", claude, sessionRenderState{status: session.StatusRunning, tool: "claude", title: "General", latestPrompt: "fix the build"},
			"prompt: fix the build · doing: bash: go test ./... · plan: Make", ""},
		{"opencode idle replaces the stale pane title", oc, sessionRenderState{status: session.StatusIdle, tool: "opencode", title: "development", paneTitle: "OC | Commit message"},
			"prompt: is that better? · todo: run the tests", "Commit message"},
		{"opencode running", oc, sessionRenderState{status: session.StatusRunning, tool: "opencode", title: "development"},
			"prompt: is that better? · doing: edit: lcd.c · todo: run the tests", ""},
	}
	for _, tc := range cases {
		row := summaryRow(t, h, tc.inst, tc.state)
		if !strings.Contains(row, tc.want) {
			t.Errorf("%s: row %q must contain %q", tc.name, row, tc.want)
		}
		if tc.not != "" && strings.Contains(row, tc.not) {
			t.Errorf("%s: row %q must not contain %q", tc.name, row, tc.not)
		}
	}
}

// With summary_lines each part gets its own line under the row, and the
// row's height counts them so scrolling and clicks line up.
func TestSessionSummary_Lines(t *testing.T) {
	inst := &session.Instance{ID: "cl", Title: "General", Tool: "claude"}
	h := &Home{width: 200, summaryLines: true,
		livePlans:      map[string]string{"cl": "Make the rate window XC-safe"},
		liveActivities: map[string]string{"cl": "bash: go test ./..."},
	}
	state := sessionRenderState{status: session.StatusRunning, tool: "claude", title: "General", latestPrompt: "fix the build"}
	lines := strings.Split(strings.TrimSuffix(summaryRow(t, h, inst, state), "\n"), "\n")
	if len(lines) != 4 {
		t.Fatalf("want the row plus 3 summary lines, got %d: %q", len(lines), lines)
	}
	for i, want := range []string{"prompt: fix the build", "plan: Make the rate window XC-safe", "doing: bash: go test ./..."} {
		if !strings.Contains(lines[i+1], want) || strings.Contains(lines[i+1], "·") {
			t.Errorf("line %d = %q, want only %q", i+1, lines[i+1], want)
		}
	}
	if got := strings.Index(lines[1], "│ prompt:"); got != strings.Index(lines[0], "General") {
		t.Errorf("summary lines must start under the title: connector at %d, title at %d", got, strings.Index(lines[0], "General"))
	}
	if !strings.Contains(lines[3], "╰ doing:") || strings.Contains(lines[2], "╰") {
		t.Errorf("only the last summary line ends the connector: %q", lines[1:])
	}

	h.sessionRenderSnapshot.Store(map[string]sessionRenderState{inst.ID: state})
	item := session.Item{Type: session.ItemTypeSession, Session: inst}
	if got := h.sidebarItemRenderHeightAtWidthLines(item, 200, 1); got != 4 {
		t.Errorf("row height = %d, want 4", got)
	}
	if got := h.sidebarItemsHeightWithLines([]session.Item{item, {Type: session.ItemTypeGroup}}, 0, 2, 200, 1); got != 5 {
		t.Errorf("list height = %d, want 5", got)
	}
	h.summaryLines = false
	if got := h.sidebarItemRenderHeightAtWidthLines(item, 200, 1); got != 1 {
		t.Errorf("row height with summary_lines off = %d, want 1", got)
	}
}

func TestRenderSummarySegs_Truncates(t *testing.T) {
	segs := []summarySeg{{"prompt", "fix the build"}, {"todo", "run every test in the tree"}}
	if got := stripAnsi(renderSummarySegs(segs, 100)); got != "prompt: fix the build · todo: run every test in the tree" {
		t.Errorf("unbounded = %q", got)
	}
	if got := stripAnsi(renderSummarySegs(segs, 34)); got != "prompt: fix the build · todo: run…" {
		t.Errorf("cut = %q", got)
	}
	if got := stripAnsi(renderSummarySegs(segs, 27)); got != "prompt: fix the build" {
		t.Errorf("a part with under 6 cells left must be dropped, got %q", got)
	}
}

// Without a plan or todo a row falls back to the agent's newest reply.
func TestSessionSummary_ReplyFallback(t *testing.T) {
	inst := &session.Instance{ID: "cl", Title: "General", Tool: "claude"}
	h := &Home{width: 220, showPaneTitles: true, liveReplies: map[string]string{"cl": "Both changes are in."}}
	state := sessionRenderState{status: session.StatusIdle, tool: "claude", title: "General", latestPrompt: "fix the build"}
	if row := summaryRow(t, h, inst, state); !strings.Contains(row, "prompt: fix the build · reply: Both changes are in.") {
		t.Fatalf("row must fall back to the reply, got %q", row)
	}
	h.livePlans = map[string]string{"cl": "Make it XC-safe"}
	if row := summaryRow(t, h, inst, state); strings.Contains(row, "reply:") || !strings.Contains(row, "plan: Make it XC-safe") {
		t.Fatalf("a plan must win over the reply, got %q", row)
	}
}

// A long reply is cut to a short line ending in "…".
func TestSessionSummary_ReplyTruncated(t *testing.T) {
	inst := &session.Instance{ID: "cl", Title: "General", Tool: "claude"}
	long := strings.Repeat("word ", 30)
	h := &Home{width: 220, summaryLines: true, liveReplies: map[string]string{"cl": long}}
	s := h.sessionSummaryFor(inst, sessionRenderState{status: session.StatusIdle, tool: "claude", title: "General"})
	if cellWidth(s.step) > summaryReplyWidth || !strings.HasSuffix(s.step, "…") {
		t.Fatalf("reply = %q (%d cells), want at most %d ending in …", s.step, cellWidth(s.step), summaryReplyWidth)
	}
}

// Left folds a session's summary lines to one line and right opens them, as
// for a group; folded, the row is one line tall again.
func TestSessionSummary_Fold(t *testing.T) {
	inst := &session.Instance{ID: "cl", Title: "General", Tool: "claude"}
	h := &Home{width: 200, summaryLines: true, showPaneTitles: true, liveReplies: map[string]string{"cl": "Both changes are in."}}
	state := sessionRenderState{status: session.StatusIdle, tool: "claude", title: "General", latestPrompt: "fix the build"}
	h.sessionRenderSnapshot.Store(map[string]sessionRenderState{inst.ID: state})
	item := session.Item{Type: session.ItemTypeSession, Session: inst, Level: 1, Path: "test", IsLastInGroup: true}

	if got := h.sidebarItemRenderHeightAtWidthLines(item, 200, 1); got != 3 {
		t.Fatalf("open row height = %d, want 3", got)
	}
	if !h.foldSummary(item) {
		t.Fatal("left on an open summary must fold it")
	}
	if got := h.sidebarItemRenderHeightAtWidthLines(item, 200, 1); got != 1 {
		t.Fatalf("folded row height = %d, want 1", got)
	}
	state.paneTitle = "General task"
	row := summaryRow(t, h, inst, state)
	if strings.Count(row, "\n") != 1 || strings.Contains(row, "prompt:") || !strings.Contains(row, "General task") {
		t.Fatalf("a folded session must look as stock agent-deck draws it, got %q", row)
	}
	if h.foldSummary(item) {
		t.Fatal("left on a folded summary must fall through to the other folds")
	}
	if !h.toggleSummaryFold(item) || h.summaryCollapsed[inst.ID] {
		t.Fatal("right on a folded summary must open it")
	}
	if !h.toggleSummaryFold(item) || !h.summaryCollapsed[inst.ID] {
		t.Fatal("right on an open summary with nothing else to toggle must fold it")
	}
	h.summaryLines = false
	if h.foldSummary(item) || h.toggleSummaryFold(item) {
		t.Fatal("with summary_lines off the keys must keep their old meaning")
	}
}

// Sessions created after the cut-off start folded until opened; older ones
// start open.
func TestSessionSummary_NewSessionsStartFolded(t *testing.T) {
	cutoff := time.Now()
	old := &session.Instance{ID: "old", Title: "old", Tool: "claude", CreatedAt: cutoff.Add(-time.Hour)}
	fresh := &session.Instance{ID: "new", Title: "new", Tool: "claude", CreatedAt: cutoff.Add(time.Minute)}
	h := &Home{summaryLines: true, summaryFoldNewAfter: cutoff, liveReplies: map[string]string{"old": "hi", "new": "hi"}}
	h.sessionRenderSnapshot.Store(map[string]sessionRenderState{
		"old": {status: session.StatusIdle, tool: "claude", title: "old"},
		"new": {status: session.StatusIdle, tool: "claude", title: "new"},
	})
	if h.summaryFolded(old) || !h.summaryFolded(fresh) {
		t.Fatalf("old folded=%v new folded=%v, want false and true", h.summaryFolded(old), h.summaryFolded(fresh))
	}
	item := session.Item{Type: session.ItemTypeSession, Session: fresh}
	if !h.toggleSummaryFold(item) || h.summaryFolded(fresh) {
		t.Fatal("right must open a new session's summary")
	}
	if !h.foldSummary(item) || !h.summaryFolded(fresh) {
		t.Fatal("left must fold it again")
	}
}

// With summary_lines the row keeps stock agent-deck's pane-title suffix,
// open or folded.
func TestSessionSummary_RowKeepsPaneTitle(t *testing.T) {
	inst := &session.Instance{ID: "oc", Title: "development", Tool: "opencode"}
	h := &Home{width: 200, summaryLines: true, showPaneTitles: true, livePrompts: map[string]string{"oc": "fix the build"}}
	state := sessionRenderState{status: session.StatusIdle, tool: "opencode", title: "development", paneTitle: "OC | Commit message"}
	lines := strings.Split(strings.TrimSuffix(summaryRow(t, h, inst, state), "\n"), "\n")
	if len(lines) != 2 || !strings.Contains(lines[0], "OC | Commit message") || !strings.Contains(lines[1], "prompt: fix the build") {
		t.Fatalf("open: want the pane title on the row and the prompt below, got %q", lines)
	}
	h.summaryCollapsed = map[string]bool{"oc": true}
	lines = strings.Split(strings.TrimSuffix(summaryRow(t, h, inst, state), "\n"), "\n")
	if len(lines) != 1 || !strings.Contains(lines[0], "OC | Commit message") || strings.Contains(lines[0], "prompt:") {
		t.Fatalf("folded: want only the row with its pane title, got %q", lines)
	}
}

// View-only mode shows the list alone and lets only moving, folding,
// filtering and quitting keys through.
func TestViewOnly(t *testing.T) {
	h := &Home{width: 200, viewOnly: true}
	if got := h.getLayoutMode(); got != LayoutModeSingle {
		t.Fatalf("layout = %q, want the list alone", got)
	}
	for _, key := range []string{"down", "left", "right", "q", "%"} {
		if !h.viewOnlyAllows(key) {
			t.Errorf("view-only must allow %q", key)
		}
	}
	for _, key := range []string{"enter", "n", "d", "x", "e", "r", "M", "/", "S"} {
		if h.viewOnlyAllows(key) {
			t.Errorf("view-only must block %q", key)
		}
	}
	h.summaryCollapsed = map[string]bool{"a": true}
	h.SetViewOnly(true, DefaultViewOnlyRecent)
	if len(h.summaryCollapsed) != 0 || !h.summaryFoldNewAfter.IsZero() {
		t.Error("the board must start with every summary open")
	}
	if err := h.saveUIStateErr(); err != nil {
		t.Errorf("view-only must not save UI state, got %v", err)
	}
	h.viewOnly = false
	if !h.viewOnlyAllows("enter") {
		t.Error("outside view-only every key is allowed")
	}
}

// The board shows one line per idle session and a single doing line under a
// running one, with no pane title or prompt.
func TestViewOnly_BoardLines(t *testing.T) {
	inst := &session.Instance{ID: "cl", Title: "General", Tool: "claude"}
	h := &Home{width: 120, viewOnly: true,
		liveActivities: map[string]string{"cl": "bash: go test ./..."},
		liveReplies:    map[string]string{"cl": "Both changes are in."},
	}
	idle := sessionRenderState{status: session.StatusIdle, tool: "claude", title: "General", paneTitle: "General task", latestPrompt: "fix the build"}
	if row := summaryRow(t, h, inst, idle); strings.Count(row, "\n") != 1 || strings.Contains(row, "prompt:") || strings.Contains(row, "General task") {
		t.Fatalf("an idle session must be one plain line on the board, got %q", row)
	}
	running := idle
	running.status = session.StatusRunning
	lines := strings.Split(strings.TrimSuffix(summaryRow(t, h, inst, running), "\n"), "\n")
	if len(lines) != 2 || !strings.Contains(lines[1], "╰ doing: bash: go test ./...") {
		t.Fatalf("a running session must show only its doing line, got %q", lines)
	}
}

// The board opens only groups with a session active within its window,
// counting subgroups, so a parent opens with a busy child.
func TestViewOnly_OpenRecentGroups(t *testing.T) {
	now := time.Now()
	recent := &session.Instance{ID: "r", GroupPath: "work/busy", CreatedAt: now.Add(-72 * time.Hour), LastAccessedAt: now.Add(-time.Hour)}
	old := &session.Instance{ID: "o", GroupPath: "archive", CreatedAt: now.Add(-30 * 24 * time.Hour)}
	idle := &session.Instance{ID: "i", GroupPath: "work/quiet", CreatedAt: now.Add(-10 * 24 * time.Hour)}
	h := &Home{groupTree: session.NewGroupTree([]*session.Instance{recent, old, idle})}
	h.SetViewOnly(true, DefaultViewOnlyRecent)
	h.openRecentGroups(now)
	for path, want := range map[string]bool{"work": true, "work/busy": true, "work/quiet": false, "archive": false} {
		if got := h.groupTree.Groups[path].Expanded; got != want {
			t.Errorf("group %q open = %v, want %v", path, got, want)
		}
	}
}

// With header_counts_visible the header counts cover open groups only, the
// total in brackets when it differs.
func TestHeaderCounts_OpenGroupsOnly(t *testing.T) {
	if got := headerCount(1, 2); got != "1 (2)" {
		t.Errorf("headerCount(1, 2) = %q", got)
	}
	if got := headerCount(3, 3); got != "3" {
		t.Errorf("headerCount(3, 3) = %q", got)
	}
	open := &session.Instance{ID: "o", GroupPath: "work/busy"}
	hidden := &session.Instance{ID: "h", GroupPath: "archive/old"}
	h := &Home{groupTree: session.NewGroupTree([]*session.Instance{open, hidden}),
		instanceByID: map[string]*session.Instance{"o": open, "h": hidden}}
	h.groupTree.CollapseGroup("archive")
	h.sessionRenderSnapshot.Store(map[string]sessionRenderState{
		"o": {status: session.StatusRunning},
		"h": {status: session.StatusRunning},
	})
	if got := h.countVisibleSessionStatuses(); got.running != 1 {
		t.Errorf("visible running = %d, want 1 (a session under a folded parent group is hidden)", got.running)
	}
}

// ] steps forward through the status filters and [ back, wrapping round.
func TestCycleStatusFilter(t *testing.T) {
	h := NewHome()
	h.storage = nil
	want := []session.Status{FilterModeActive, session.StatusRunning, session.StatusWaiting, session.StatusIdle, session.StatusStopped, session.StatusError, ""}
	for _, w := range want {
		h.cycleStatusFilter(1)
		if h.statusFilter != w {
			t.Fatalf("] gave %q, want %q", h.statusFilter, w)
		}
	}
	h.cycleStatusFilter(-1)
	if h.statusFilter != session.StatusError {
		t.Fatalf("[ from all gave %q, want error", h.statusFilter)
	}
}

func TestDarkenTowardBg(t *testing.T) {
	InitTheme("dark")
	got := darkenTowardBg(lipgloss.Color("#ffffff"), 0.5)
	r, g, b, ok := parseHexColor(string(got))
	br, bg, bb, _ := parseHexColor(string(ColorBg))
	if !ok || r >= 0xff || r <= br || g <= bg || b <= bb {
		t.Fatalf("darkenTowardBg(#ffffff) = %q, want between white and the background %q", got, ColorBg)
	}
	if got := darkenTowardBg(lipgloss.Color("3"), 0.5); got != "3" {
		t.Fatalf("an ANSI colour must come back unchanged, got %q", got)
	}
}

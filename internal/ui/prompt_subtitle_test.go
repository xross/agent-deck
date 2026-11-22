package ui

import "testing"

// A pane title that only repeats the session's name gives way to the last
// prompt, so the row's dim suffix says what the session is working on.
func TestSessionDisplayLabelsFromState_PromptReplacesRedundantPaneTitle(t *testing.T) {
	cases := []struct {
		name  string
		state sessionRenderState
		want  string
	}{
		{"pane title repeats the name", sessionRenderState{title: "General", paneTitle: "General", latestPrompt: "fix the  build\nand the tests"}, "fix the build"},
		{"case differs", sessionRenderState{title: "General", paneTitle: "general", latestPrompt: "fix the build"}, "fix the build"},
		{"opencode prefix on the name", sessionRenderState{title: "adapt_pll", paneTitle: "OC | adapt_pll", latestPrompt: "tune the loop"}, "tune the loop"},
		{"opencode prefix, no prompt", sessionRenderState{title: "adapt_pll", paneTitle: "OC | adapt_pll"}, ""},
		{"no pane title", sessionRenderState{title: "General", latestPrompt: "fix the build"}, "fix the build"},
		{"a real pane title wins", sessionRenderState{title: "General", paneTitle: "OC | Commit message", latestPrompt: "fix the build"}, "OC | Commit message"},
		{"slash command is not a description", sessionRenderState{title: "General", paneTitle: "General", latestPrompt: "/compact"}, ""},
		{"leading blank lines", sessionRenderState{title: "General", paneTitle: "General", latestPrompt: "\n\n  second line  "}, "second line"},
		{"nothing known", sessionRenderState{title: "General", paneTitle: "General"}, ""},
		{"claude pane title is an older name", sessionRenderState{tool: "claude", title: "adapt_pll", paneTitle: "adapt", latestPrompt: "force push"}, "force push"},
		{"claude with no usable prompt keeps the pane title", sessionRenderState{tool: "claude", title: "adapt_pll", paneTitle: "adapt", latestPrompt: "/compact"}, "adapt"},
		{"another tool keeps a differing pane title", sessionRenderState{tool: "opencode", title: "adapt_pll", paneTitle: "OC | PLL tuning", latestPrompt: "force push"}, "OC | PLL tuning"},
	}
	for _, tc := range cases {
		if _, got := sessionDisplayLabelsFromState(tc.state); got != tc.want {
			t.Errorf("%s: subtitle = %q, want %q", tc.name, got, tc.want)
		}
	}
}

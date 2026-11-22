package ui

import (
	"strings"

	"github.com/charmbracelet/lipgloss"

	"github.com/asheshgoplani/agent-deck/internal/session"
)

// Session summary: what a session is doing, what it was last prompted with
// and the plan or todo step it reached, read from the tools' own records
// (model_badge.go). It is the row's dim suffix, or with [display]
// summary_lines one line per part under the row.

// summaryReplyWidth caps each summary line's text (reply, prompt, doing; a
// plan or todo heading is short by nature): a whole first paragraph or shell
// script is too much for a row.
const summaryReplyWidth = 60

// summaryStepWidth caps the activity and the plan or todo on a shared line
// so a long one leaves the other parts some room.
const summaryStepWidth = 48

// summarySeg is one labelled part of a summary.
type summarySeg struct {
	label string // "plan", "todo", "reply", "prompt", "doing"; "" for a bare pane title
	text  string
}

type sessionSummary struct {
	stepLabel string // "plan" (Claude), "todo" (OpenCode), else "reply"
	step      string
	prompt    string
	doing     string
	// other is the pane title, for a tool that records none of the above.
	other string
}

func oneLine(s string) string { return strings.Join(strings.Fields(s), " ") }

// sessionSummaryFor builds a session's summary from the poll's cached maps
// and the render snapshot only, so rows stay lock-free.
func (h *Home) sessionSummaryFor(inst *session.Instance, state sessionRenderState) sessionSummary {
	var s sessionSummary
	switch {
	case session.IsClaudeCompatible(state.tool):
		s.stepLabel, s.step = "plan", oneLine(h.livePlans[inst.ID])
		prompt := h.livePrompts[inst.ID]
		if prompt == "" {
			prompt = state.latestPrompt
		}
		s.prompt = promptSubtitle(prompt)
	case state.tool == "opencode":
		s.stepLabel, s.step = "todo", oneLine(h.liveTodos[inst.ID])
		s.prompt = promptSubtitle(h.livePrompts[inst.ID])
	}
	// Without a plan or todo, the first line of the agent's newest reply.
	if s.stepLabel != "" && s.step == "" {
		if reply := oneLine(h.liveReplies[inst.ID]); reply != "" {
			s.stepLabel, s.step = "reply", truncateDisplay(reply, summaryReplyWidth, "…")
		}
	}
	if state.status == session.StatusRunning {
		s.doing = oneLine(h.liveActivities[inst.ID])
	}
	if s.step == "" && s.prompt == "" && s.doing == "" {
		_, s.other = sessionDisplayLabelsFromState(state)
	}
	return s
}

func (s sessionSummary) stepSeg() summarySeg   { return summarySeg{s.stepLabel, s.step} }
func (s sessionSummary) promptSeg() summarySeg { return summarySeg{"prompt", s.prompt} }
func (s sessionSummary) doingSeg() summarySeg  { return summarySeg{"doing", s.doing} }

// singleLine is the summary as one suffix: the prompt first, then what it
// is doing, then the (capped) plan, todo or reply.
func (s sessionSummary) singleLine() []summarySeg {
	var segs []summarySeg
	if s.prompt != "" {
		seg := s.promptSeg()
		seg.text = truncateDisplay(seg.text, summaryStepWidth, "…")
		segs = append(segs, seg)
	}
	if s.doing != "" {
		seg := s.doingSeg()
		seg.text = truncateDisplay(seg.text, summaryStepWidth, "…")
		segs = append(segs, seg)
	}
	if s.step != "" {
		seg := s.stepSeg()
		seg.text = truncateDisplay(seg.text, summaryStepWidth, "…")
		segs = append(segs, seg)
	}
	if s.other != "" {
		segs = append(segs, summarySeg{text: s.other})
	}
	return segs
}

// lines is the summary one part per line: prompt first, then plan, todo or
// reply, then doing. The prompt and activity are capped like the reply.
func (s sessionSummary) lines() [][]summarySeg {
	var out [][]summarySeg
	capped := func(seg summarySeg) []summarySeg {
		seg.text = truncateDisplay(seg.text, summaryReplyWidth, "…")
		return []summarySeg{seg}
	}
	if s.prompt != "" {
		out = append(out, capped(s.promptSeg()))
	}
	if s.step != "" {
		out = append(out, []summarySeg{s.stepSeg()})
	}
	if s.doing != "" {
		out = append(out, capped(s.doingSeg()))
	}
	if s.other != "" {
		out = append(out, []summarySeg{{text: s.other}})
	}
	return out
}

// summaryLineCount is how many summary lines a row draws under it.
func (h *Home) summaryLineCount(item session.Item) int {
	if !h.summaryLines || item.Type != session.ItemTypeSession || item.Session == nil {
		return 0
	}
	return len(h.sessionSummaryFor(item.Session, h.getSessionRenderState(item.Session)).lines())
}

// summaryMutedColor picks one of two muted label colours for the active
// theme: the palette's own colours are too bright next to the dim text.
func summaryMutedColor(dark, light string) lipgloss.Color {
	if currentTheme == ThemeLight {
		return lipgloss.Color(light)
	}
	return lipgloss.Color(dark)
}

// summaryLabelStyle colours each part's label: prompt dark green, plan cyan,
// todo yellow, reply muted blue, doing green. Only the label is coloured;
// the text after it is always dim, so the summary stays quieter than the
// rows.
func summaryLabelStyle(label string) lipgloss.Style {
	switch label {
	case "prompt":
		return lipgloss.NewStyle().Foreground(summaryMutedColor("#5f8a3f", "#3a4d27"))
	case "reply":
		return lipgloss.NewStyle().Foreground(summaryMutedColor("#6d7bb0", "#4a5680"))
	case "plan":
		return lipgloss.NewStyle().Foreground(ColorCyan)
	case "todo":
		return lipgloss.NewStyle().Foreground(ColorYellow)
	case "doing":
		return lipgloss.NewStyle().Foreground(ColorGreen)
	}
	return DimStyle
}

// renderSummarySeg renders one part: its label in the label colour, the
// text dim.
func renderSummarySeg(seg summarySeg, piece string) string {
	if seg.label == "" {
		return DimStyle.Render(piece)
	}
	head := seg.label + ":"
	if !strings.HasPrefix(piece, head) {
		return DimStyle.Render(piece) // cut inside the label
	}
	return summaryLabelStyle(seg.label).Render(head) + DimStyle.Render(strings.TrimPrefix(piece, head))
}

// summarySep joins parts on a shared line.
const summarySep = " · "

// renderSummarySegs renders parts within budget cells, cutting the part that
// does not fit with "…" and dropping the rest (a cut part under 6 cells is
// dropped too).
func renderSummarySegs(segs []summarySeg, budget int) string {
	var b strings.Builder
	used := 0
	for i, seg := range segs {
		piece := seg.text
		if seg.label != "" {
			piece = seg.label + ": " + seg.text
		}
		sep := ""
		if i > 0 {
			sep = summarySep
		}
		need := cellWidth(sep) + cellWidth(piece)
		if used+need > budget {
			avail := budget - used - cellWidth(sep)
			if avail >= 6 {
				b.WriteString(DimStyle.Render(sep))
				b.WriteString(renderSummarySeg(seg, cellTruncate(piece, avail, "…")))
			}
			break
		}
		b.WriteString(DimStyle.Render(sep))
		b.WriteString(renderSummarySeg(seg, piece))
		used += need
	}
	return b.String()
}

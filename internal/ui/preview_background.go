package ui

// defaultBackgroundANSI resets the background to the terminal's own.
const defaultBackgroundANSI = "\x1b[49m"

// previewBackgroundReplacement is what captured dark backgrounds in tool's
// preview are replaced with, "" to leave them as they are.
//
// In light theme every tool's dark bands go to the preview surface
// (#322/#2449). In dark theme OpenCode paints its whole screen in its own
// near-black, which shows as black blocks on a terminal whose background is
// a lighter grey (e.g. Ghostty's default); its dark backgrounds are dropped
// so the preview sits on the terminal's background. Light backgrounds, such
// as a selection, pass through either way.
func previewBackgroundReplacement(tool string) string {
	if GetCurrentTheme() == ThemeLight {
		return previewSurfaceANSI()
	}
	if tool == "opencode" {
		return defaultBackgroundANSI
	}
	return ""
}

// remapPreviewBackground applies previewBackgroundReplacement to one
// captured line.
func remapPreviewBackground(line, tool string) string {
	if replacement := previewBackgroundReplacement(tool); replacement != "" {
		return remapANSIBackground(line, replacement)
	}
	return line
}

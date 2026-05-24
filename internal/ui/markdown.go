package ui

import (
	"strings"
	"sync"

	"github.com/charmbracelet/glamour"
)

// glamour renderer is expensive to build (loads chroma styles) — keep one
// instance and rebuild only when the target width changes.
var (
	mdMu       sync.Mutex
	mdRenderer *glamour.TermRenderer
	mdWidth    int
)

// renderMarkdown renders s as markdown into ANSI at the given column width.
// Falls back to the raw string on any error so the preview still shows
// content if glamour chokes on a transcript line.
func renderMarkdown(s string, width int) string {
	if strings.TrimSpace(s) == "" {
		return s
	}
	if width < 10 {
		width = 10
	}
	mdMu.Lock()
	defer mdMu.Unlock()
	if mdRenderer == nil || mdWidth != width {
		r, err := glamour.NewTermRenderer(
			glamour.WithAutoStyle(),
			glamour.WithWordWrap(width),
		)
		if err != nil {
			return s
		}
		mdRenderer = r
		mdWidth = width
	}
	out, err := mdRenderer.Render(s)
	if err != nil {
		return s
	}
	return strings.Trim(out, "\n")
}

package ui

import (
	"fmt"
	"strings"
	"time"

	"github.com/openaphid/ccsm/internal/session"
)

// renderPreview formats a session's events into the right-hand pane.
// width is the inner content width (excluding border/padding).
func renderPreview(s *session.Session, width int) string {
	if s == nil {
		return ""
	}
	if width < 10 {
		width = 10
	}
	var sb strings.Builder
	title := s.DisplayName()
	if title == "" {
		title = "(no title)"
	}
	header := fmt.Sprintf("%s\nID: %s\nProject: %s\nBranch: %s · Version: %s · Events: %d",
		title, s.ID, s.CWD, valOr(s.GitBranch, "-"), valOr(s.Version, "-"), s.NumEvents)
	sb.WriteString(dimStyle.Render(header))
	sb.WriteString("\n\n")

	count := 0
	const maxBlocks = 200
	_ = s.LoadEvents(func(ev session.Event) bool {
		text := strings.TrimSpace(session.ExtractMessageText(ev))
		if text == "" {
			return true
		}
		var label string
		switch ev.Type {
		case "user":
			if ev.IsMeta {
				return true
			}
			label = roleUser.Render("▌ user")
		case "assistant":
			label = roleAsst.Render("▌ assistant")
		default:
			label = roleTool.Render("▌ " + ev.Type)
		}
		when := ""
		if t, err := time.Parse(time.RFC3339Nano, ev.Timestamp); err == nil {
			when = dimStyle.Render(t.Format(" · 01-02 15:04:05"))
		}
		sb.WriteString(label)
		sb.WriteString(when)
		sb.WriteString("\n")
		sb.WriteString(wrap(text, width))
		sb.WriteString("\n\n")
		count++
		return count < maxBlocks
	})
	if count == 0 {
		sb.WriteString(dimStyle.Render("(transcript has no displayable text)"))
	} else if count >= maxBlocks {
		sb.WriteString(dimStyle.Render(fmt.Sprintf("… truncated after %d blocks", maxBlocks)))
	}
	return sb.String()
}

func valOr(s, fallback string) string {
	if s == "" {
		return fallback
	}
	return s
}

// wrap is a tiny soft-wrap that respects existing newlines.
func wrap(s string, width int) string {
	if width <= 0 {
		return s
	}
	var out strings.Builder
	for _, line := range strings.Split(s, "\n") {
		if len(line) <= width {
			out.WriteString(line)
			out.WriteByte('\n')
			continue
		}
		// break on spaces if possible
		words := strings.Fields(line)
		col := 0
		for i, w := range words {
			if col+len(w)+1 > width && col > 0 {
				out.WriteByte('\n')
				col = 0
			}
			if col > 0 {
				out.WriteByte(' ')
				col++
			}
			out.WriteString(w)
			col += len(w)
			if i == len(words)-1 {
				out.WriteByte('\n')
			}
		}
	}
	return strings.TrimRight(out.String(), "\n")
}

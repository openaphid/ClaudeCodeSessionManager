package ui

import (
	"fmt"
	"path/filepath"
	"time"

	"github.com/charmbracelet/bubbles/list"

	"github.com/openaphid/ccsm/internal/session"
)

// projectItem wraps a *session.Project for the list bubble.
type projectItem struct {
	p *session.Project
}

func (i projectItem) FilterValue() string { return i.p.CWD }
func (i projectItem) Title() string {
	name := filepath.Base(i.p.CWD)
	if name == "" || name == "/" {
		name = i.p.Dir
	}
	return fmt.Sprintf("%s (%d)", name, len(i.p.Sessions))
}
func (i projectItem) Description() string {
	age := humanAge(time.Since(i.p.ModTime))
	return fmt.Sprintf("%s · %s ago", i.p.CWD, age)
}

// sessionItem wraps a *session.Session for the list bubble.
type sessionItem struct {
	s *session.Session
}

func (i sessionItem) FilterValue() string { return i.s.FirstPrompt + " " + i.s.ID }
func (i sessionItem) Title() string {
	prompt := i.s.FirstPrompt
	if prompt == "" {
		prompt = "(no user prompt)"
	}
	return prompt
}
func (i sessionItem) Description() string {
	when := i.s.ModTime.Format("01-02 15:04")
	branch := i.s.GitBranch
	if branch == "" {
		branch = "-"
	}
	return fmt.Sprintf("%s · %s · %s · %d msgs · %s",
		shortID(i.s.ID), when, branch,
		i.s.NumUser+i.s.NumAsst, session.HumanSize(i.s.Size))
}

func shortID(id string) string {
	if len(id) > 8 {
		return id[:8]
	}
	return id
}

func humanAge(d time.Duration) string {
	switch {
	case d < time.Minute:
		return fmt.Sprintf("%ds", int(d.Seconds()))
	case d < time.Hour:
		return fmt.Sprintf("%dm", int(d.Minutes()))
	case d < 24*time.Hour:
		return fmt.Sprintf("%dh", int(d.Hours()))
	default:
		return fmt.Sprintf("%dd", int(d.Hours()/24))
	}
}

// toListItems converts typed slices to list.Item slices.
func projectsToItems(ps []*session.Project) []list.Item {
	out := make([]list.Item, len(ps))
	for i, p := range ps {
		out[i] = projectItem{p}
	}
	return out
}

func sessionsToItems(ss []*session.Session) []list.Item {
	out := make([]list.Item, len(ss))
	for i, s := range ss {
		out[i] = sessionItem{s}
	}
	return out
}

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
	name := projectDisplayName(i.p.CWD)
	if name == "" {
		name = i.p.Dir
	}
	return fmt.Sprintf("%s (%d)", name, len(i.p.Sessions))
}

// projectDisplayName picks a readable label for a project cwd.
// Worktrees of the form `<parent>/.claude/worktrees/<name>` are rendered
// as `<parent-base> [<worktree-name>]` so the user sees the originating
// project at a glance instead of a bare worktree slug.
func projectDisplayName(cwd string) string {
	if cwd == "" || cwd == "/" {
		return ""
	}
	base := filepath.Base(cwd)
	parent := filepath.Dir(cwd)
	if filepath.Base(parent) == "worktrees" && filepath.Base(filepath.Dir(parent)) == ".claude" {
		project := filepath.Base(filepath.Dir(filepath.Dir(parent)))
		if project != "" && project != "/" {
			return fmt.Sprintf("%s [%s]", project, base)
		}
	}
	return base
}
func (i projectItem) Description() string {
	age := humanAge(time.Since(i.p.ModTime))
	return fmt.Sprintf("%s · %s ago", i.p.CWD, age)
}

// sessionItem wraps a *session.Session for the list bubble. live marks a
// session a running `claude` currently owns — a hint computed at list-build
// time; the authoritative collision check still runs at resume.
type sessionItem struct {
	s    *session.Session
	live bool
}

func (i sessionItem) FilterValue() string {
	return i.s.CustomTitle + " " + i.s.AITitle + " " + i.s.FirstPrompt + " " + i.s.ID
}
func (i sessionItem) Title() string {
	name := i.s.DisplayName()
	if name == "" {
		name = "(no title)"
	} else {
		switch {
		case i.s.CustomTitle != "":
			name = "★ " + name
		case i.s.AITitle != "":
			name = "✦ " + name
		}
	}
	if i.live {
		name = "● " + name
	}
	return name
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

func sessionsToItems(ss []*session.Session, active map[string]session.ActiveSession) []list.Item {
	out := make([]list.Item, len(ss))
	for i, s := range ss {
		_, live := active[s.ID]
		out[i] = sessionItem{s: s, live: live}
	}
	return out
}

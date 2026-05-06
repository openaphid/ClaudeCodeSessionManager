package ui

import (
	"fmt"
	"os/exec"
	"strings"

	"github.com/charmbracelet/bubbles/key"
	"github.com/charmbracelet/bubbles/list"
	"github.com/charmbracelet/bubbles/viewport"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"

	"github.com/openaphid/ccsm/internal/session"
)

// pane identifies which list has focus.
type pane int

const (
	paneProjects pane = iota
	paneSessions
)

// confirmKind tracks which destructive action a y/N prompt is asking about.
type confirmKind int

const (
	confirmNone confirmKind = iota
	confirmDelete
)

// Model is the root bubbletea model.
type Model struct {
	projects   []*session.Project
	projList   list.Model
	sessList   list.Model
	preview    viewport.Model
	focus      pane
	width      int
	height     int
	status     string
	err        error
	confirm    confirmKind
	resumeReq  *resumeRequest // set on quit when user picks resume
}

// resumeRequest is filled in just before quit so the caller (main) can re-exec.
type resumeRequest struct {
	SessionID string
	CWD       string
}

// ResumeRequest exposes the resume target after Run returns.
func (m Model) ResumeRequest() (sessionID, cwd string, ok bool) {
	if m.resumeReq == nil {
		return "", "", false
	}
	return m.resumeReq.SessionID, m.resumeReq.CWD, true
}

type keymap struct {
	Tab     key.Binding
	Resume  key.Binding
	Delete  key.Binding
	Refresh key.Binding
	Quit    key.Binding
	Yes     key.Binding
	No      key.Binding
}

var keys = keymap{
	Tab:     key.NewBinding(key.WithKeys("tab"), key.WithHelp("tab", "switch pane")),
	Resume:  key.NewBinding(key.WithKeys("enter", "r"), key.WithHelp("enter/r", "resume")),
	Delete:  key.NewBinding(key.WithKeys("d", "delete"), key.WithHelp("d", "delete")),
	Refresh: key.NewBinding(key.WithKeys("R"), key.WithHelp("R", "refresh")),
	Quit:    key.NewBinding(key.WithKeys("q", "ctrl+c", "esc"), key.WithHelp("q", "quit")),
	Yes:     key.NewBinding(key.WithKeys("y", "Y")),
	No:      key.NewBinding(key.WithKeys("n", "N", "esc")),
}

// NewModel builds an initial Model with projects loaded.
func NewModel(projects []*session.Project) Model {
	pl := list.New(projectsToItems(projects), list.NewDefaultDelegate(), 0, 0)
	pl.Title = "Projects"
	pl.SetShowHelp(false)
	pl.SetShowStatusBar(false)
	pl.Styles.Title = titleStyle

	sl := list.New(nil, list.NewDefaultDelegate(), 0, 0)
	sl.Title = "Sessions"
	sl.SetShowHelp(false)
	sl.SetShowStatusBar(false)
	sl.Styles.Title = titleStyle

	if len(projects) > 0 {
		sl.SetItems(sessionsToItems(projects[0].Sessions))
	}

	vp := viewport.New(0, 0)

	return Model{
		projects: projects,
		projList: pl,
		sessList: sl,
		preview:  vp,
		focus:    paneProjects,
	}
}

func (m Model) Init() tea.Cmd { return nil }

func (m Model) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	var cmd tea.Cmd

	switch msg := msg.(type) {
	case tea.WindowSizeMsg:
		m.width, m.height = msg.Width, msg.Height
		m.layout()
		m.refreshPreview()
		return m, nil

	case tea.KeyMsg:
		// confirm dialog short-circuits all other handling
		if m.confirm != confirmNone {
			switch {
			case key.Matches(msg, keys.Yes):
				m.applyConfirm()
				m.confirm = confirmNone
				return m, nil
			case key.Matches(msg, keys.No):
				m.confirm = confirmNone
				m.status = "cancelled"
				return m, nil
			}
			return m, nil
		}

		switch {
		case key.Matches(msg, keys.Quit):
			// allow list filter ESC to close filter first
			if m.focus == paneProjects && m.projList.FilterState() == list.Filtering {
				break
			}
			if m.focus == paneSessions && m.sessList.FilterState() == list.Filtering {
				break
			}
			return m, tea.Quit

		case key.Matches(msg, keys.Tab):
			if m.focus == paneProjects {
				m.focus = paneSessions
			} else {
				m.focus = paneProjects
			}
			return m, nil

		case key.Matches(msg, keys.Refresh):
			ps, err := session.ListProjects()
			if err != nil {
				m.err = err
				return m, nil
			}
			m.projects = ps
			m.projList.SetItems(projectsToItems(ps))
			m.syncSessionsForCurrentProject()
			m.refreshPreview()
			m.status = fmt.Sprintf("refreshed: %d projects", len(ps))
			return m, nil

		case key.Matches(msg, keys.Resume):
			if m.focus != paneSessions {
				m.focus = paneSessions
				return m, nil
			}
			s := m.currentSession()
			if s == nil {
				return m, nil
			}
			m.resumeReq = &resumeRequest{SessionID: s.ID, CWD: s.CWD}
			return m, tea.Quit

		case key.Matches(msg, keys.Delete):
			if m.focus == paneSessions && m.currentSession() != nil {
				m.confirm = confirmDelete
				return m, nil
			}
		}
	}

	// route input to the focused list, otherwise scroll preview
	switch m.focus {
	case paneProjects:
		prev := m.projList.Index()
		m.projList, cmd = m.projList.Update(msg)
		if m.projList.Index() != prev {
			m.syncSessionsForCurrentProject()
			m.refreshPreview()
		}
	case paneSessions:
		prev := m.sessList.Index()
		m.sessList, cmd = m.sessList.Update(msg)
		if m.sessList.Index() != prev {
			m.refreshPreview()
		}
	}
	return m, cmd
}

func (m *Model) syncSessionsForCurrentProject() {
	p := m.currentProject()
	if p == nil {
		m.sessList.SetItems(nil)
		return
	}
	m.sessList.SetItems(sessionsToItems(p.Sessions))
	m.sessList.ResetSelected()
}

func (m *Model) refreshPreview() {
	s := m.currentSession()
	if s == nil {
		m.preview.SetContent(dimStyle.Render("(no session selected)"))
		return
	}
	innerW := m.previewInnerWidth()
	m.preview.SetContent(renderPreview(s, innerW))
	m.preview.GotoTop()
}

func (m Model) currentProject() *session.Project {
	if len(m.projects) == 0 {
		return nil
	}
	idx := m.projList.Index()
	if idx < 0 || idx >= len(m.projects) {
		return nil
	}
	return m.projects[idx]
}

func (m Model) currentSession() *session.Session {
	p := m.currentProject()
	if p == nil || len(p.Sessions) == 0 {
		return nil
	}
	idx := m.sessList.Index()
	if idx < 0 || idx >= len(p.Sessions) {
		return nil
	}
	return p.Sessions[idx]
}

func (m *Model) applyConfirm() {
	switch m.confirm {
	case confirmDelete:
		s := m.currentSession()
		if s == nil {
			return
		}
		if err := s.Delete(); err != nil {
			m.err = err
			m.status = "delete failed: " + err.Error()
			return
		}
		// drop from project + project list
		p := s.Project
		out := p.Sessions[:0]
		for _, x := range p.Sessions {
			if x != s {
				out = append(out, x)
			}
		}
		p.Sessions = out
		if len(p.Sessions) == 0 {
			// drop project too
			ps := m.projects[:0]
			for _, x := range m.projects {
				if x != p {
					ps = append(ps, x)
				}
			}
			m.projects = ps
			m.projList.SetItems(projectsToItems(m.projects))
		}
		m.syncSessionsForCurrentProject()
		m.refreshPreview()
		m.status = "deleted " + s.ID
	}
}

func (m *Model) layout() {
	if m.width == 0 || m.height == 0 {
		return
	}
	// rows: header(1) + body + status(1)
	bodyH := m.height - 2
	if bodyH < 5 {
		bodyH = 5
	}
	leftW := m.width / 3
	if leftW < 28 {
		leftW = 28
	}
	if leftW > 50 {
		leftW = 50
	}
	rightW := m.width - leftW - 4 // borders/padding budget
	if rightW < 20 {
		rightW = 20
	}

	// borders take 2 chars W and 2 chars H per pane
	innerLeftW := leftW - 4
	innerRightW := rightW - 4
	leftH := bodyH/2 - 2
	if leftH < 5 {
		leftH = 5
	}
	rightH := bodyH - 2

	m.projList.SetSize(innerLeftW, leftH)
	m.sessList.SetSize(innerLeftW, bodyH-leftH-4)
	m.preview.Width = innerRightW
	m.preview.Height = rightH
}

func (m Model) previewInnerWidth() int {
	if m.preview.Width > 0 {
		return m.preview.Width
	}
	return 60
}

func (m Model) View() string {
	if m.width == 0 {
		return "loading…"
	}
	header := titleStyle.Render(" Claude Code Session Manager ") +
		"  " + helpStyle.Render(fmt.Sprintf("%d projects", len(m.projects)))

	pStyle := panelBorder
	sStyle := panelBorder
	prevStyle := panelBorder
	if m.focus == paneProjects {
		pStyle = panelBorderFocus
	} else {
		sStyle = panelBorderFocus
	}

	left := lipgloss.JoinVertical(lipgloss.Left,
		pStyle.Render(m.projList.View()),
		sStyle.Render(m.sessList.View()),
	)
	right := prevStyle.Render(m.preview.View())
	body := lipgloss.JoinHorizontal(lipgloss.Top, left, right)

	footer := m.footer()

	return strings.Join([]string{header, body, footer}, "\n")
}

func (m Model) footer() string {
	if m.confirm == confirmDelete {
		s := m.currentSession()
		id := ""
		if s != nil {
			id = s.ID
		}
		return errStyle.Render(fmt.Sprintf("Delete session %s? [y/N]", id))
	}
	help := "tab: switch · enter/r: resume · d: delete · R: refresh · /: filter · q: quit"
	if m.err != nil {
		return errStyle.Render(m.err.Error()) + "  " + helpStyle.Render(help)
	}
	if m.status != "" {
		return statusStyle.Render(m.status) + "  " + helpStyle.Render(help)
	}
	return helpStyle.Render(help)
}

// Resume re-execs `claude --resume <id>` in the session's cwd. Called by main
// after the bubbletea program has fully torn down, so stdin/tty are clean.
func Resume(sessionID, cwd string) error {
	bin, err := exec.LookPath("claude")
	if err != nil {
		return fmt.Errorf("claude binary not in PATH: %w", err)
	}
	cmd := exec.Command(bin, "--resume", sessionID)
	if cwd != "" {
		cmd.Dir = cwd
	}
	cmd.Stdin = stdin()
	cmd.Stdout = stdout()
	cmd.Stderr = stderr()
	return cmd.Run()
}

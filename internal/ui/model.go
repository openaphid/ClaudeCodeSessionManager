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
	panePreview
)

// confirmKind tracks which destructive action a y/N prompt is asking about.
type confirmKind int

const (
	confirmNone confirmKind = iota
	confirmDelete
	confirmDeleteProject
)

// Model is the root bubbletea model.
type Model struct {
	projects      []*session.Project
	projList      list.Model
	sessList      list.Model
	preview       viewport.Model
	focus         pane
	width         int
	height        int
	status        string
	err           error
	confirm       confirmKind
	confirmChoice bool // false = No (default), true = Yes
	markdown      bool // render user/assistant text as markdown
	resumeReq     *resumeRequest // set on quit when user picks resume
	newReq        *newSessionRequest // set on quit when user picks new session
}

// resumeRequest is filled in just before quit so the caller (main) can re-exec.
type resumeRequest struct {
	SessionID string
	CWD       string
}

// newSessionRequest is filled in just before quit when the user wants to
// launch a fresh `claude` session in a project's cwd.
type newSessionRequest struct {
	CWD string
}

// ResumeRequest exposes the resume target after Run returns.
func (m Model) ResumeRequest() (sessionID, cwd string, ok bool) {
	if m.resumeReq == nil {
		return "", "", false
	}
	return m.resumeReq.SessionID, m.resumeReq.CWD, true
}

// NewSessionRequest exposes the new-session target after Run returns.
func (m Model) NewSessionRequest() (cwd string, ok bool) {
	if m.newReq == nil {
		return "", false
	}
	return m.newReq.CWD, true
}

type keymap struct {
	Tab           key.Binding
	ShiftTab      key.Binding
	Resume        key.Binding
	Delete        key.Binding
	DeleteProject key.Binding
	NewSession    key.Binding
	Markdown      key.Binding
	Refresh       key.Binding
	Quit         key.Binding
	Yes          key.Binding
	No           key.Binding
	ScrollUp     key.Binding
	ScrollDown   key.Binding
	ScrollPgUp   key.Binding
	ScrollPgDown key.Binding
	ScrollTop    key.Binding
	ScrollBot    key.Binding
	PrevPage     key.Binding
	NextPage     key.Binding
}

var keys = keymap{
	Tab:          key.NewBinding(key.WithKeys("tab"), key.WithHelp("tab", "next pane")),
	ShiftTab:     key.NewBinding(key.WithKeys("shift+tab"), key.WithHelp("shift+tab", "prev pane")),
	Resume:       key.NewBinding(key.WithKeys("enter", "r"), key.WithHelp("enter/r", "resume")),
	Delete:        key.NewBinding(key.WithKeys("d", "delete"), key.WithHelp("d", "delete")),
	DeleteProject: key.NewBinding(key.WithKeys("D"), key.WithHelp("D", "delete project")),
	NewSession:    key.NewBinding(key.WithKeys("n"), key.WithHelp("n", "new session")),
	Markdown:      key.NewBinding(key.WithKeys("m"), key.WithHelp("m", "toggle markdown")),
	Refresh:       key.NewBinding(key.WithKeys("R"), key.WithHelp("R", "refresh")),
	Quit:         key.NewBinding(key.WithKeys("q", "ctrl+c", "esc"), key.WithHelp("q", "quit")),
	Yes:          key.NewBinding(key.WithKeys("y", "Y")),
	No:           key.NewBinding(key.WithKeys("n", "N", "esc")),
	// Always-on preview scroll. Don't collide with list nav (j/k, up/down)
	// when the lists have focus — pgup/pgdn, ctrl+u/d, home/end are safe.
	ScrollUp:     key.NewBinding(key.WithKeys("ctrl+u"), key.WithHelp("ctrl+u", "preview ½pg up")),
	ScrollDown:   key.NewBinding(key.WithKeys("ctrl+d"), key.WithHelp("ctrl+d", "preview ½pg dn")),
	ScrollPgUp:   key.NewBinding(key.WithKeys("pgup")),
	ScrollPgDown: key.NewBinding(key.WithKeys("pgdown")),
	ScrollTop:    key.NewBinding(key.WithKeys("g", "home")),
	ScrollBot:    key.NewBinding(key.WithKeys("G", "end")),
	// Page scroll via left/right — preview-pane only so lists keep arrows for nav.
	PrevPage:     key.NewBinding(key.WithKeys("left", "h")),
	NextPage:     key.NewBinding(key.WithKeys("right", "l")),
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
		markdown: true,
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
			case msg.String() == "left", msg.String() == "right",
				msg.String() == "h", msg.String() == "l",
				msg.String() == "tab", msg.String() == "shift+tab":
				m.confirmChoice = !m.confirmChoice
				return m, nil
			case msg.String() == "enter":
				if m.confirmChoice {
					m.applyConfirm()
				} else {
					m.status = "cancelled"
				}
				m.confirm = confirmNone
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
			m.focus = (m.focus + 1) % 3
			return m, nil

		case key.Matches(msg, keys.ShiftTab):
			m.focus = (m.focus + 2) % 3
			return m, nil

		// Always-on preview scrolling — works from any pane.
		case key.Matches(msg, keys.ScrollUp, keys.ScrollDown,
			keys.ScrollPgUp, keys.ScrollPgDown):
			m.preview, cmd = m.preview.Update(msg)
			return m, cmd
		case key.Matches(msg, keys.ScrollTop):
			// scope g/home to preview pane so lists keep their own bindings
			if m.focus == panePreview {
				m.preview.GotoTop()
				return m, nil
			}
		case key.Matches(msg, keys.ScrollBot):
			if m.focus == panePreview {
				m.preview.GotoBottom()
				return m, nil
			}
		case key.Matches(msg, keys.PrevPage):
			if m.focus == panePreview {
				m.preview.ViewUp()
				return m, nil
			}
		case key.Matches(msg, keys.NextPage):
			if m.focus == panePreview {
				m.preview.ViewDown()
				return m, nil
			}

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
				m.confirmChoice = false // default No
				return m, nil
			}

		case key.Matches(msg, keys.DeleteProject):
			if m.focus == paneProjects && m.currentProject() != nil {
				m.confirm = confirmDeleteProject
				m.confirmChoice = false // default No
				return m, nil
			}

		case key.Matches(msg, keys.NewSession):
			// don't swallow `n` while a list filter is being typed
			if m.focus == paneProjects && m.projList.FilterState() == list.Filtering {
				break
			}
			if m.focus == paneSessions && m.sessList.FilterState() == list.Filtering {
				break
			}
			p := m.currentProject()
			if p == nil {
				return m, nil
			}
			cwd := p.CWD
			if cwd == "" || cwd == "/" {
				m.status = "cannot start: project cwd unknown"
				return m, nil
			}
			m.newReq = &newSessionRequest{CWD: cwd}
			return m, tea.Quit

		case key.Matches(msg, keys.Markdown):
			// don't swallow `m` while a list filter is being typed
			if m.focus == paneProjects && m.projList.FilterState() == list.Filtering {
				break
			}
			if m.focus == paneSessions && m.sessList.FilterState() == list.Filtering {
				break
			}
			m.markdown = !m.markdown
			m.reRenderPreview()
			if m.markdown {
				m.status = "markdown on"
			} else {
				m.status = "markdown off"
			}
			return m, nil
		}
	}

	// route input to the focused pane
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
	case panePreview:
		m.preview, cmd = m.preview.Update(msg)
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
	m.setPreview(true)
}

// reRenderPreview re-renders the current session without scrolling to top —
// used when toggling render flags (e.g. markdown) so the user keeps their
// place in the transcript.
func (m *Model) reRenderPreview() {
	m.setPreview(false)
}

func (m *Model) setPreview(resetScroll bool) {
	s := m.currentSession()
	if s == nil {
		m.preview.SetContent(dimStyle.Render("(no session selected)"))
		return
	}
	innerW := m.previewInnerWidth()
	prevOffset := m.preview.YOffset
	m.preview.SetContent(renderPreview(s, innerW, m.markdown))
	if resetScroll {
		m.preview.GotoTop()
		return
	}
	m.preview.SetYOffset(prevOffset) // viewport clamps to content bounds
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
			m.removeProject(p)
		}
		m.syncSessionsForCurrentProject()
		m.refreshPreview()
		m.status = "deleted " + s.ID

	case confirmDeleteProject:
		p := m.currentProject()
		if p == nil {
			return
		}
		n, err := p.DeleteAll()
		if err != nil {
			m.err = err
			m.status = fmt.Sprintf("delete project failed (deleted %d): %s", n, err.Error())
			return
		}
		m.removeProject(p)
		m.syncSessionsForCurrentProject()
		m.refreshPreview()
		m.status = fmt.Sprintf("deleted project %s (%d sessions)", p.CWD, n)
	}
}

func (m *Model) removeProject(p *session.Project) {
	ps := m.projects[:0]
	for _, x := range m.projects {
		if x != p {
			ps = append(ps, x)
		}
	}
	m.projects = ps
	m.projList.SetItems(projectsToItems(m.projects))
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
	switch m.focus {
	case paneProjects:
		pStyle = panelBorderFocus
	case paneSessions:
		sStyle = panelBorderFocus
	case panePreview:
		prevStyle = panelBorderFocus
	}

	left := lipgloss.JoinVertical(lipgloss.Left,
		pStyle.Render(m.projList.View()),
		sStyle.Render(m.sessList.View()),
	)
	right := prevStyle.Render(m.preview.View())
	body := lipgloss.JoinHorizontal(lipgloss.Top, left, right)

	footer := m.footer()

	if m.confirm != confirmNone {
		bodyH := lipgloss.Height(body)
		body = lipgloss.Place(m.width, bodyH, lipgloss.Center, lipgloss.Center, m.confirmDialog())
	}

	return strings.Join([]string{header, body, footer}, "\n")
}

func (m Model) confirmDialog() string {
	var title, info string
	switch m.confirm {
	case confirmDelete:
		title = errStyle.Render("Delete session?")
		s := m.currentSession()
		if s != nil {
			info = s.ID + "\n" + dimStyle.Render(s.Path)
		}
	case confirmDeleteProject:
		title = errStyle.Render("Delete ALL sessions in project?")
		p := m.currentProject()
		if p != nil {
			info = fmt.Sprintf("%s\n%s",
				p.CWD,
				dimStyle.Render(fmt.Sprintf("%d sessions · %s", len(p.Sessions), p.Path)))
		}
	}

	yesLabel := "  Yes  "
	noLabel := "  No  "
	if m.confirmChoice {
		yesLabel = selectedStyle.Render("▶ Yes ◀")
		noLabel = dimStyle.Render("  No   ")
	} else {
		yesLabel = dimStyle.Render("  Yes  ")
		noLabel = selectedStyle.Render("▶ No  ◀")
	}
	buttons := lipgloss.JoinHorizontal(lipgloss.Top, yesLabel, "    ", noLabel)
	hint := helpStyle.Render("←/→ switch · enter confirm · esc cancel")

	content := lipgloss.JoinVertical(lipgloss.Center, title, "", info, "", buttons, "", hint)
	return lipgloss.NewStyle().
		Border(lipgloss.RoundedBorder()).
		BorderForeground(lipgloss.Color("160")).
		Padding(1, 4).
		Render(content)
}

// paneHelp returns shortcut hints relevant to the focused pane. Always-on
// keys (tab/quit/refresh) live in the common prefix; pane-specific keys come
// next.
func (m Model) paneHelp() string {
	md := "off"
	if m.markdown {
		md = "on"
	}
	common := fmt.Sprintf("tab/shift+tab: pane · m: md(%s) · R: refresh · q: quit", md)
	switch m.focus {
	case paneProjects:
		return "n: new · D: delete project · /: filter · " + common
	case paneSessions:
		return "enter/r: resume · n: new · d: delete · /: filter · " + common
	case panePreview:
		return "pgup/pgdn · ctrl+u/d: ½pg · ←/→: page · g/G: top/bot · " + common
	}
	return common
}

func (m Model) footer() string {
	if m.confirm != confirmNone {
		return errStyle.Render("Awaiting confirmation… (←/→ to switch, enter to apply, esc to cancel)")
	}
	help := m.paneHelp()
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

// NewSession re-execs a fresh `claude` (no --resume) in the given cwd.
func NewSession(cwd string) error {
	bin, err := exec.LookPath("claude")
	if err != nil {
		return fmt.Errorf("claude binary not in PATH: %w", err)
	}
	cmd := exec.Command(bin)
	if cwd != "" {
		cmd.Dir = cwd
	}
	cmd.Stdin = stdin()
	cmd.Stdout = stdout()
	cmd.Stderr = stderr()
	return cmd.Run()
}

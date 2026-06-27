package ui

import (
	"fmt"
	"os/exec"
	"strings"
	"time"

	"github.com/charmbracelet/bubbles/key"
	"github.com/charmbracelet/bubbles/list"
	"github.com/charmbracelet/bubbles/textinput"
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
	confirmResumeActive
)

// dialogKind tracks which resume/new flag dialog is open. Separate from
// confirmKind: confirm owns the destructive y/N prompts, dialog owns the
// (non-destructive) flag flow that feeds resume/new-session.
type dialogKind int

const (
	dialogNone         dialogKind = iota
	dialogResumeChoice            // pick: simple resume vs resume with flags
	dialogResumeFlags             // text input for resume flags
	dialogNewFlags                // text input for new-session flags
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
	confirmChoice bool                             // false = No (default), true = Yes
	conflict      *session.ActiveSession           // live process owning the session a resume would collide with
	active        map[string]session.ActiveSession // live sessions by id, for the list ● marker; refreshed on load/R
	markdown      bool                             // render user/assistant text as markdown
	resumeReq     *resumeRequest                   // set on quit when user picks resume
	newReq        *newSessionRequest               // set on quit when user picks new session
	procStart     time.Time                        // process start, for first-frame latency
	firstFrame    time.Duration                    // procStart -> first WindowSizeMsg handled (incl. first preview render)

	// flag-dialog state (resume choice / flag input)
	dialog          dialogKind
	choiceWithFlags bool            // selection in dialogResumeChoice (true = with flags)
	flagInput       textinput.Model // shared input for the flag dialogs
	dialogSessID    string          // resume target while a dialog is open
	dialogCWD       string          // resume/new target cwd while a dialog is open
	pendingArgs     []string        // flags carried through the confirmResumeActive detour
	state           session.State   // persisted per-session/per-project flag memory
}

// WithStartTime arms first-frame latency measurement against t.
func (m Model) WithStartTime(t time.Time) Model {
	m.procStart = t
	return m
}

// FirstFrame returns how long the first full layout+preview took, measured
// from the start time set via WithStartTime. Zero if never armed/reached.
func (m Model) FirstFrame() time.Duration { return m.firstFrame }

// resumeRequest is filled in just before quit so the caller (main) can re-exec.
type resumeRequest struct {
	SessionID string
	CWD       string
	ExtraArgs []string // extra flags to pass to `claude` (e.g. --chrome)
}

// newSessionRequest is filled in just before quit when the user wants to
// launch a fresh `claude` session in a project's cwd.
type newSessionRequest struct {
	CWD       string
	ExtraArgs []string // extra flags to pass to `claude` (e.g. --chrome)
}

// ResumeRequest exposes the resume target after Run returns.
func (m Model) ResumeRequest() (sessionID, cwd string, extraArgs []string, ok bool) {
	if m.resumeReq == nil {
		return "", "", nil, false
	}
	return m.resumeReq.SessionID, m.resumeReq.CWD, m.resumeReq.ExtraArgs, true
}

// NewSessionRequest exposes the new-session target after Run returns.
func (m Model) NewSessionRequest() (cwd string, extraArgs []string, ok bool) {
	if m.newReq == nil {
		return "", nil, false
	}
	return m.newReq.CWD, m.newReq.ExtraArgs, true
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
	Quit          key.Binding
	Yes           key.Binding
	No            key.Binding
	ScrollUp      key.Binding
	ScrollDown    key.Binding
	ScrollPgUp    key.Binding
	ScrollPgDown  key.Binding
	ScrollTop     key.Binding
	ScrollBot     key.Binding
	PrevPage      key.Binding
	NextPage      key.Binding
}

var keys = keymap{
	Tab:           key.NewBinding(key.WithKeys("tab"), key.WithHelp("tab", "next pane")),
	ShiftTab:      key.NewBinding(key.WithKeys("shift+tab"), key.WithHelp("shift+tab", "prev pane")),
	Resume:        key.NewBinding(key.WithKeys("enter", "r"), key.WithHelp("enter/r", "resume")),
	Delete:        key.NewBinding(key.WithKeys("d", "delete"), key.WithHelp("d", "delete")),
	DeleteProject: key.NewBinding(key.WithKeys("D"), key.WithHelp("D", "delete project")),
	NewSession:    key.NewBinding(key.WithKeys("n"), key.WithHelp("n", "new session")),
	Markdown:      key.NewBinding(key.WithKeys("m"), key.WithHelp("m", "toggle markdown")),
	Refresh:       key.NewBinding(key.WithKeys("R"), key.WithHelp("R", "refresh")),
	Quit:          key.NewBinding(key.WithKeys("q", "ctrl+c", "esc"), key.WithHelp("q", "quit")),
	Yes:           key.NewBinding(key.WithKeys("y", "Y")),
	No:            key.NewBinding(key.WithKeys("n", "N", "esc")),
	// Always-on preview scroll. Don't collide with list nav (j/k, up/down)
	// when the lists have focus — pgup/pgdn, ctrl+u/d, home/end are safe.
	ScrollUp:     key.NewBinding(key.WithKeys("ctrl+u"), key.WithHelp("ctrl+u", "preview ½pg up")),
	ScrollDown:   key.NewBinding(key.WithKeys("ctrl+d"), key.WithHelp("ctrl+d", "preview ½pg dn")),
	ScrollPgUp:   key.NewBinding(key.WithKeys("pgup")),
	ScrollPgDown: key.NewBinding(key.WithKeys("pgdown")),
	ScrollTop:    key.NewBinding(key.WithKeys("g", "home")),
	ScrollBot:    key.NewBinding(key.WithKeys("G", "end")),
	// Page scroll via left/right — preview-pane only so lists keep arrows for nav.
	PrevPage: key.NewBinding(key.WithKeys("left", "h")),
	NextPage: key.NewBinding(key.WithKeys("right", "l")),
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

	active, _ := session.ActiveSessions() // best-effort; nil map is a safe lookup
	state, _ := session.LoadState()       // best-effort; empty maps on error

	if len(projects) > 0 {
		sl.SetItems(sessionsToItems(projects[0].Sessions, active))
	}

	vp := viewport.New(0, 0)

	return Model{
		projects: projects,
		projList: pl,
		sessList: sl,
		preview:  vp,
		focus:    paneProjects,
		markdown: true,
		active:   active,
		state:    state,
	}
}

func (m Model) Init() tea.Cmd { return nil }

func (m Model) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	var cmd tea.Cmd

	// While a flag input is open, let it consume its own async messages (cursor
	// blink) so the cursor animates. Keys still flow through the switch below.
	if m.dialog == dialogResumeFlags || m.dialog == dialogNewFlags {
		switch msg.(type) {
		case tea.KeyMsg, tea.WindowSizeMsg:
		default:
			m.flagInput, cmd = m.flagInput.Update(msg)
			return m, cmd
		}
	}

	switch msg := msg.(type) {
	case tea.WindowSizeMsg:
		m.width, m.height = msg.Width, msg.Height
		m.layout()
		m.refreshPreview()
		if m.firstFrame == 0 && !m.procStart.IsZero() {
			m.firstFrame = time.Since(m.procStart)
		}
		return m, nil

	case tea.KeyMsg:
		// confirm dialog short-circuits all other handling
		if m.confirm != confirmNone {
			switch {
			case key.Matches(msg, keys.Yes):
				m.applyConfirm()
				m.confirm = confirmNone
				if m.resumeReq != nil {
					return m, tea.Quit
				}
				return m, nil
			case key.Matches(msg, keys.No):
				m.confirm = confirmNone
				m.conflict = nil
				m.pendingArgs = nil
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
					m.pendingArgs = nil
				}
				m.confirm = confirmNone
				if m.confirmChoice && m.resumeReq != nil {
					return m, tea.Quit
				}
				return m, nil
			}
			return m, nil
		}

		// flag dialogs (resume choice / flag input) capture all keys
		if m.dialog != dialogNone {
			return m.updateDialog(msg)
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
			m.active, _ = session.ActiveSessions()
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
			// Open the resume choice dialog. Default to whatever this session
			// last did (simple vs with-flags); the conflict check runs once the
			// flags are resolved (see finishResume).
			m.dialogSessID = s.ID
			m.dialogCWD = s.CWD
			m.choiceWithFlags = m.state.Resume[s.ID].WithFlags
			m.dialog = dialogResumeChoice
			return m, nil

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
			m.dialogCWD = cwd
			m.flagInput = newFlagInput(m.state.New[cwd])
			m.dialog = dialogNewFlags
			return m, textinput.Blink

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
	m.sessList.SetItems(sessionsToItems(p.Sessions, m.active))
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

// activeConflict reports the live `claude` process already holding sessionID,
// or nil if none. Read at resume time (not cached) so it reflects the moment of
// the keypress. Best-effort: a registry read error is treated as "no conflict"
// — failing open keeps resume working when the check can't run.
func (m Model) activeConflict(sessionID string) *session.ActiveSession {
	active, err := session.ActiveSessions()
	if err != nil {
		return nil
	}
	if a, ok := active[sessionID]; ok {
		return &a
	}
	return nil
}

// newFlagInput builds a focused text input for the flag dialogs, prefilled with
// (and cursor at the end of) prev.
func newFlagInput(prev string) textinput.Model {
	ti := textinput.New()
	ti.Prompt = "flags: "
	ti.Placeholder = "--chrome --model opus …"
	ti.Width = 40
	ti.SetValue(prev)
	ti.CursorEnd()
	ti.Focus()
	return ti
}

// closeDialog clears all transient flag-dialog state.
func (m *Model) closeDialog() {
	m.dialog = dialogNone
	m.choiceWithFlags = false
	m.flagInput = textinput.Model{}
	m.dialogSessID = ""
	m.dialogCWD = ""
}

// saveState persists the flag memory. Best-effort: a write failure is surfaced
// to the status line but never blocks resume.
func (m *Model) saveState() {
	if err := m.state.Save(); err != nil {
		m.status = "warning: could not save flag memory: " + err.Error()
	}
}

// updateDialog handles keys while a resume/new flag dialog is open. It returns
// the new model and command, fully owning input until the dialog closes.
func (m Model) updateDialog(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	if msg.String() == "ctrl+c" {
		return m, tea.Quit
	}
	switch m.dialog {
	case dialogResumeChoice:
		switch msg.String() {
		case "esc":
			m.closeDialog()
			m.status = "cancelled"
			return m, nil
		case "left", "right", "h", "l", "up", "down", "j", "k", "tab", "shift+tab":
			m.choiceWithFlags = !m.choiceWithFlags
			return m, nil
		case "1":
			m.choiceWithFlags = false
			return m, nil
		case "2":
			m.choiceWithFlags = true
			return m, nil
		case "enter":
			if m.choiceWithFlags {
				m.dialog = dialogResumeFlags
				m.flagInput = newFlagInput(m.state.Resume[m.dialogSessID].Flags)
				return m, textinput.Blink
			}
			// simple resume: remember the choice (keep any stored flags for
			// next time's prefill), then resume.
			rs := m.state.Resume[m.dialogSessID]
			rs.WithFlags = false
			m.state.Resume[m.dialogSessID] = rs
			m.saveState()
			return m.finishResume(nil)
		}
		return m, nil

	case dialogResumeFlags:
		switch msg.String() {
		case "esc":
			m.closeDialog()
			m.status = "cancelled"
			return m, nil
		case "enter":
			val := strings.TrimSpace(m.flagInput.Value())
			args := splitArgs(val)
			m.state.Resume[m.dialogSessID] = session.ResumeState{Flags: val, WithFlags: true}
			m.saveState()
			return m.finishResume(args)
		}
		var cmd tea.Cmd
		m.flagInput, cmd = m.flagInput.Update(msg)
		return m, cmd

	case dialogNewFlags:
		switch msg.String() {
		case "esc":
			m.closeDialog()
			m.status = "cancelled"
			return m, nil
		case "enter":
			val := strings.TrimSpace(m.flagInput.Value())
			args := splitArgs(val)
			if val == "" {
				delete(m.state.New, m.dialogCWD)
			} else {
				m.state.New[m.dialogCWD] = val
			}
			m.saveState()
			m.newReq = &newSessionRequest{CWD: m.dialogCWD, ExtraArgs: args}
			return m, tea.Quit
		}
		var cmd tea.Cmd
		m.flagInput, cmd = m.flagInput.Update(msg)
		return m, cmd
	}
	return m, nil
}

// finishResume runs the live-session conflict check and either pops the
// confirmResumeActive prompt or sets the resume request and quits. args is the
// resolved extra flags (nil for a simple resume).
func (m Model) finishResume(args []string) (tea.Model, tea.Cmd) {
	id, cwd := m.dialogSessID, m.dialogCWD
	m.closeDialog()
	// Guard against opening a transcript another live `claude` already owns —
	// concurrent appends to the same JSONL corrupt history.
	if a := m.activeConflict(id); a != nil {
		m.conflict = a
		m.pendingArgs = args
		m.confirm = confirmResumeActive
		m.confirmChoice = false // default No
		return m, nil
	}
	m.resumeReq = &resumeRequest{SessionID: id, CWD: cwd, ExtraArgs: args}
	return m, tea.Quit
}

func (m *Model) applyConfirm() {
	switch m.confirm {
	case confirmResumeActive:
		s := m.currentSession()
		if s == nil {
			return
		}
		m.resumeReq = &resumeRequest{SessionID: s.ID, CWD: s.CWD, ExtraArgs: m.pendingArgs}
		m.pendingArgs = nil
		m.conflict = nil

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
	} else if m.dialog != dialogNone {
		bodyH := lipgloss.Height(body)
		body = lipgloss.Place(m.width, bodyH, lipgloss.Center, lipgloss.Center, m.flagDialog())
	}

	return strings.Join([]string{header, body, footer}, "\n")
}

// flagDialog renders the resume choice menu or the flag text input.
func (m Model) flagDialog() string {
	switch m.dialog {
	case dialogResumeChoice:
		title := titleStyle.Render(" Resume session ")
		info := dimStyle.Render(m.dialogSessID)
		simple := dimStyle.Render("  Simple  ")
		withFlags := dimStyle.Render("  With flags  ")
		if m.choiceWithFlags {
			withFlags = selectedStyle.Render("▶ With flags ◀")
		} else {
			simple = selectedStyle.Render("▶ Simple ◀")
		}
		buttons := lipgloss.JoinHorizontal(lipgloss.Top, simple, "    ", withFlags)
		hint := helpStyle.Render("←/→ switch · enter confirm · esc cancel")
		content := lipgloss.JoinVertical(lipgloss.Center, title, "", info, "", buttons, "", hint)
		return dialogBoxStyle.Render(content)

	case dialogResumeFlags, dialogNewFlags:
		title := titleStyle.Render(" Resume with flags ")
		if m.dialog == dialogNewFlags {
			title = titleStyle.Render(" New session with flags ")
		}
		hint := helpStyle.Render("enter confirm · esc cancel · empty = none")
		content := lipgloss.JoinVertical(lipgloss.Left, title, "", m.flagInput.View(), "", hint)
		return dialogBoxStyle.Render(content)
	}
	return ""
}

func (m Model) confirmDialog() string {
	var title, info string
	switch m.confirm {
	case confirmResumeActive:
		title = errStyle.Render("Session already open!")
		if m.conflict != nil {
			where := m.conflict.CWD
			if where == "" {
				where = "(unknown cwd)"
			}
			detail := fmt.Sprintf("live in pid %d · %s", m.conflict.PID, where)
			if m.conflict.Status != "" {
				detail += " · " + m.conflict.Status
			}
			info = m.conflict.SessionID + "\n" +
				dimStyle.Render(detail) + "\n\n" +
				dimStyle.Render("Resuming will let two processes write the same\ntranscript and corrupt its history.")
		}
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
		return "n: new(+flags) · D: delete project · /: filter · " + common
	case paneSessions:
		return "enter/r: resume(+flags) · n: new · d: delete · /: filter · " + common
	case panePreview:
		return "pgup/pgdn · ctrl+u/d: ½pg · ←/→: page · g/G: top/bot · " + common
	}
	return common
}

func (m Model) footer() string {
	if m.confirm != confirmNone {
		return errStyle.Render("Awaiting confirmation… (←/→ to switch, enter to apply, esc to cancel)")
	}
	if m.dialog != dialogNone {
		return helpStyle.Render("enter: confirm · esc: cancel")
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

// Resume re-execs `claude --resume <id> [extraArgs...]` in the session's cwd.
// Called by main after the bubbletea program has fully torn down, so stdin/tty
// are clean.
func Resume(sessionID, cwd string, extraArgs []string) error {
	bin, err := exec.LookPath("claude")
	if err != nil {
		return fmt.Errorf("claude binary not in PATH: %w", err)
	}
	args := append([]string{"--resume", sessionID}, extraArgs...)
	cmd := exec.Command(bin, args...)
	if cwd != "" {
		cmd.Dir = cwd
	}
	cmd.Stdin = stdin()
	cmd.Stdout = stdout()
	cmd.Stderr = stderr()
	return cmd.Run()
}

// NewSession re-execs a fresh `claude [extraArgs...]` (no --resume) in the given
// cwd.
func NewSession(cwd string, extraArgs []string) error {
	bin, err := exec.LookPath("claude")
	if err != nil {
		return fmt.Errorf("claude binary not in PATH: %w", err)
	}
	cmd := exec.Command(bin, extraArgs...)
	if cwd != "" {
		cmd.Dir = cwd
	}
	cmd.Stdin = stdin()
	cmd.Stdout = stdout()
	cmd.Stderr = stderr()
	return cmd.Run()
}

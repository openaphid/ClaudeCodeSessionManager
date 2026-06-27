package ui

import (
	"reflect"
	"testing"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/openaphid/ccsm/internal/session"
)

func keyMsg(s string) tea.KeyMsg {
	switch s {
	case "enter":
		return tea.KeyMsg{Type: tea.KeyEnter}
	case "esc":
		return tea.KeyMsg{Type: tea.KeyEsc}
	default:
		return tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune(s)}
	}
}

func send(t *testing.T, m Model, msg tea.Msg) Model {
	t.Helper()
	nm, _ := m.Update(msg)
	mm, ok := nm.(Model)
	if !ok {
		t.Fatalf("Update returned %T, want Model", nm)
	}
	return mm
}

func oneSessionModel(t *testing.T) Model {
	t.Helper()
	t.Setenv("CLAUDE_HOME", t.TempDir()) // isolate state + active-session reads
	proj := &session.Project{CWD: "/work"}
	s := &session.Session{ID: "sess1", CWD: "/work", Project: proj}
	proj.Sessions = []*session.Session{s}
	m := NewModel([]*session.Project{proj})
	m.focus = paneSessions
	return m
}

func TestResumeWithFlagsFlow(t *testing.T) {
	m := oneSessionModel(t)

	// enter opens the choice dialog, defaulting to "Simple" (no history).
	m = send(t, m, keyMsg("enter"))
	if m.dialog != dialogResumeChoice {
		t.Fatalf("dialog = %v, want dialogResumeChoice", m.dialog)
	}
	if m.choiceWithFlags {
		t.Fatal("default choice should be Simple for a session with no history")
	}

	// pick "With flags" (key 2), enter opens the flag input.
	m = send(t, m, keyMsg("2"))
	m = send(t, m, keyMsg("enter"))
	if m.dialog != dialogResumeFlags {
		t.Fatalf("dialog = %v, want dialogResumeFlags", m.dialog)
	}

	// type flags, enter resumes.
	m = send(t, m, keyMsg("--chrome"))
	m = send(t, m, keyMsg("enter"))

	if m.resumeReq == nil {
		t.Fatal("resumeReq not set after confirming flags")
	}
	if m.resumeReq.SessionID != "sess1" || m.resumeReq.CWD != "/work" {
		t.Errorf("resumeReq target = %+v", m.resumeReq)
	}
	if !reflect.DeepEqual(m.resumeReq.ExtraArgs, []string{"--chrome"}) {
		t.Errorf("ExtraArgs = %#v, want [--chrome]", m.resumeReq.ExtraArgs)
	}

	// state is persisted: next load defaults to with-flags, prefilled.
	st, _ := session.LoadState()
	if got := st.Resume["sess1"]; got.Flags != "--chrome" || !got.WithFlags {
		t.Errorf("persisted resume state = %+v, want {--chrome true}", got)
	}
}

func TestRememberedDefaultChoice(t *testing.T) {
	m := oneSessionModel(t)
	// Seed memory as if this session last resumed with flags.
	m.state.Resume["sess1"] = session.ResumeState{Flags: "--chrome", WithFlags: true}

	m = send(t, m, keyMsg("enter"))
	if !m.choiceWithFlags {
		t.Fatal("choice should default to With flags when last resume used flags")
	}
}

func TestSimpleResumeNoFlags(t *testing.T) {
	m := oneSessionModel(t)
	m = send(t, m, keyMsg("enter")) // choice dialog, default Simple
	m = send(t, m, keyMsg("enter")) // confirm Simple

	if m.resumeReq == nil {
		t.Fatal("resumeReq not set")
	}
	if len(m.resumeReq.ExtraArgs) != 0 {
		t.Errorf("simple resume should carry no flags, got %#v", m.resumeReq.ExtraArgs)
	}
	if m.dialog != dialogNone {
		t.Errorf("dialog should be closed, got %v", m.dialog)
	}
}

func TestEscCancelsDialog(t *testing.T) {
	m := oneSessionModel(t)
	m = send(t, m, keyMsg("enter")) // open choice
	m = send(t, m, keyMsg("esc"))   // cancel
	if m.dialog != dialogNone {
		t.Errorf("dialog = %v, want dialogNone after esc", m.dialog)
	}
	if m.resumeReq != nil {
		t.Error("esc must not set a resume request")
	}
}

func TestNewSessionWithFlags(t *testing.T) {
	m := oneSessionModel(t)
	m.focus = paneProjects

	m = send(t, m, keyMsg("n"))
	if m.dialog != dialogNewFlags {
		t.Fatalf("dialog = %v, want dialogNewFlags", m.dialog)
	}
	m = send(t, m, keyMsg("--chrome"))
	m = send(t, m, keyMsg("enter"))

	if m.newReq == nil {
		t.Fatal("newReq not set")
	}
	if m.newReq.CWD != "/work" {
		t.Errorf("newReq cwd = %q", m.newReq.CWD)
	}
	if !reflect.DeepEqual(m.newReq.ExtraArgs, []string{"--chrome"}) {
		t.Errorf("ExtraArgs = %#v, want [--chrome]", m.newReq.ExtraArgs)
	}
	st, _ := session.LoadState()
	if st.New["/work"] != "--chrome" {
		t.Errorf("persisted new-session flags = %q, want --chrome", st.New["/work"])
	}
}

package session

import "testing"

func TestStateRoundTrip(t *testing.T) {
	t.Setenv("CLAUDE_HOME", t.TempDir())

	s, err := LoadState()
	if err != nil {
		t.Fatalf("LoadState on empty: %v", err)
	}
	if len(s.Resume) != 0 || len(s.New) != 0 {
		t.Fatalf("expected empty maps, got resume=%d new=%d", len(s.Resume), len(s.New))
	}

	s.Resume["abc"] = ResumeState{Flags: "--chrome", WithFlags: true}
	s.New["/work/proj"] = "--add-dir /x"
	if err := s.Save(); err != nil {
		t.Fatalf("Save: %v", err)
	}

	got, err := LoadState()
	if err != nil {
		t.Fatalf("LoadState after save: %v", err)
	}
	if got.Resume["abc"] != (ResumeState{Flags: "--chrome", WithFlags: true}) {
		t.Errorf("resume entry not round-tripped: %+v", got.Resume["abc"])
	}
	if got.New["/work/proj"] != "--add-dir /x" {
		t.Errorf("new entry not round-tripped: %q", got.New["/work/proj"])
	}
}

func TestLoadStateMissingFile(t *testing.T) {
	t.Setenv("CLAUDE_HOME", t.TempDir())
	s, err := LoadState()
	if err != nil {
		t.Fatalf("LoadState: %v", err)
	}
	if s.Resume == nil || s.New == nil {
		t.Fatal("maps must be non-nil even when file missing")
	}
}

package session

import (
	"fmt"
	"os"
	"path/filepath"
	"testing"
)

func TestActiveSessions(t *testing.T) {
	home := t.TempDir()
	t.Setenv("CLAUDE_HOME", home)
	dir := filepath.Join(home, "sessions")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}

	write := func(name, body string) {
		if err := os.WriteFile(filepath.Join(dir, name), []byte(body), 0o644); err != nil {
			t.Fatal(err)
		}
	}

	// alive: this test process is guaranteed running.
	live := os.Getpid()
	write(fmt.Sprintf("%d.json", live),
		fmt.Sprintf(`{"pid":%d,"sessionId":"live-sess","cwd":"/tmp/work","status":"busy"}`, live))
	// stale: a pid that does not exist must be filtered out.
	write("999999.json", `{"pid":999999,"sessionId":"dead-sess","cwd":"/tmp/old"}`)
	// junk: malformed and non-json files are ignored, not fatal.
	write("garbage.json", `{not valid json`)
	write("notes.txt", `{"pid":1,"sessionId":"ignored"}`)

	active, err := ActiveSessions()
	if err != nil {
		t.Fatal(err)
	}
	if len(active) != 1 {
		t.Fatalf("want 1 active session, got %d: %v", len(active), active)
	}
	a, ok := active["live-sess"]
	if !ok {
		t.Fatalf("live-sess missing from %v", active)
	}
	if a.PID != live || a.CWD != "/tmp/work" || a.Status != "busy" {
		t.Errorf("unexpected entry: %+v", a)
	}
	if _, ok := active["dead-sess"]; ok {
		t.Error("stale (dead pid) session should be filtered out")
	}
}

func TestActiveSessionsNoRegistry(t *testing.T) {
	// No sessions/ dir (older Claude Code) → empty map, no error.
	t.Setenv("CLAUDE_HOME", t.TempDir())
	active, err := ActiveSessions()
	if err != nil {
		t.Fatalf("missing registry dir should not error: %v", err)
	}
	if len(active) != 0 {
		t.Errorf("want empty, got %v", active)
	}
}

func TestClaudeRoot(t *testing.T) {
	t.Setenv("CLAUDE_HOME", "/custom/home")
	root, err := ClaudeRoot()
	if err != nil {
		t.Fatal(err)
	}
	if root != "/custom/home" {
		t.Errorf("ClaudeRoot=%q want /custom/home", root)
	}
}

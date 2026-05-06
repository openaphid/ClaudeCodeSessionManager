package session

import (
	"os"
	"path/filepath"
	"testing"
)

func TestDecodeDirToCWD(t *testing.T) {
	cases := []struct {
		in, want string
	}{
		{"-Volumes-Data-Codes-BabelStream", "/Volumes/Data/Codes/BabelStream"},
		{"-Users-bohu", "/Users/bohu"},
		{"", ""},
		{"relative-path", "relative/path"},
	}
	for _, c := range cases {
		if got := DecodeDirToCWD(c.in); got != c.want {
			t.Errorf("DecodeDirToCWD(%q) = %q, want %q", c.in, got, c.want)
		}
	}
}

func TestLoadHeader(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "abc.jsonl")
	body := `{"type":"permission-mode","permissionMode":"default","sessionId":"abc"}
{"type":"user","message":{"role":"user","content":"hello world"},"uuid":"u1","timestamp":"2026-05-01T14:07:33.859Z","cwd":"/tmp/x","gitBranch":"main","version":"2.1.0"}
{"type":"assistant","message":{"role":"assistant","content":[{"type":"text","text":"hi back"}]},"uuid":"u2","timestamp":"2026-05-01T14:07:34.000Z"}
`
	if err := os.WriteFile(path, []byte(body), 0o644); err != nil {
		t.Fatal(err)
	}
	s := &Session{Path: path}
	if err := s.LoadHeader(); err != nil {
		t.Fatal(err)
	}
	if s.NumUser != 1 || s.NumAsst != 1 {
		t.Errorf("counts: user=%d asst=%d", s.NumUser, s.NumAsst)
	}
	if s.FirstPrompt != "hello world" {
		t.Errorf("FirstPrompt=%q", s.FirstPrompt)
	}
	if s.CWD != "/tmp/x" {
		t.Errorf("CWD=%q", s.CWD)
	}
	if s.GitBranch != "main" {
		t.Errorf("GitBranch=%q", s.GitBranch)
	}
}

func TestCleanPromptDropsWrappers(t *testing.T) {
	in := "<command-name>/plugin</command-name>"
	if got := cleanPrompt(in); got != "" {
		t.Errorf("expected empty, got %q", got)
	}
}

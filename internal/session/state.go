package session

import (
	"encoding/json"
	"os"
	"path/filepath"
)

// ResumeState remembers the extra `claude` flags a session was last resumed
// with. Claude Code itself records no launch flags anywhere (the transcript
// keeps only entrypoint, never argv), so this is ccsm's own memory used to
// default the resume dialog sensibly.
type ResumeState struct {
	Flags     string `json:"flags"`     // last-used extra flags, verbatim dialog input
	WithFlags bool   `json:"withFlags"` // whether the last resume used flags (drives the choice default)
}

// State is ccsm's persisted per-session / per-project flag memory, stored at
// <claude-home>/ccsm/state.json. It is UI-free so the session package stays
// usable from -list mode and any scripting entry point.
type State struct {
	Resume map[string]ResumeState `json:"resume"` // keyed by session ID
	New    map[string]string      `json:"new"`    // keyed by project cwd -> last-used flags
}

// statePath returns <claude-home>/ccsm/state.json.
func statePath() (string, error) {
	root, err := ClaudeRoot()
	if err != nil {
		return "", err
	}
	return filepath.Join(root, "ccsm", "state.json"), nil
}

// LoadState reads the persisted flag memory. Best-effort (like ActiveSessions):
// a missing or malformed file yields empty, non-nil maps rather than an error,
// so callers degrade to "no memory" instead of failing.
func LoadState() (State, error) {
	s := State{Resume: map[string]ResumeState{}, New: map[string]string{}}
	path, err := statePath()
	if err != nil {
		return s, err
	}
	b, err := os.ReadFile(path)
	if err != nil {
		if os.IsNotExist(err) {
			return s, nil
		}
		return s, err
	}
	// Decode into a temp so a malformed file leaves the empty maps intact.
	var dec State
	if err := json.Unmarshal(b, &dec); err != nil {
		return s, nil
	}
	if dec.Resume != nil {
		s.Resume = dec.Resume
	}
	if dec.New != nil {
		s.New = dec.New
	}
	return s, nil
}

// Save writes the flag memory, creating <claude-home>/ccsm if needed. Callers
// treat a returned error as non-fatal — saving flag memory must never block a
// resume.
func (s State) Save() error {
	path, err := statePath()
	if err != nil {
		return err
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return err
	}
	b, err := json.MarshalIndent(s, "", "  ")
	if err != nil {
		return err
	}
	return os.WriteFile(path, b, 0o644)
}

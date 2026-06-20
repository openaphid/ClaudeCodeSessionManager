package session

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"syscall"
)

// ActiveSession is one live `claude` process, as recorded in the session
// registry Claude Code maintains at <claude-home>/sessions/<pid>.json. The UI
// uses it to warn before resuming a transcript another process already owns —
// two processes appending to the same JSONL corrupt each other's history.
type ActiveSession struct {
	PID       int    `json:"pid"`
	SessionID string `json:"sessionId"`
	CWD       string `json:"cwd"`
	Status    string `json:"status"` // "busy"/"idle"/… — informational, schema owned by Claude Code
}

// ClaudeRoot returns the Claude Code home dir (parent of the projects root):
// $CLAUDE_HOME if set, else ~/.claude.
func ClaudeRoot() (string, error) {
	root, err := Home()
	if err != nil {
		return "", err
	}
	return filepath.Dir(root), nil // Home() returns <root>/projects
}

// ActiveSessions returns the live sessions keyed by SessionID. It reads every
// <claude-home>/sessions/<pid>.json and keeps only entries whose pid is still
// running, so a crashed process that left a stale file is ignored.
//
// Best-effort: a missing registry dir (older Claude Code) or an unreadable /
// malformed file yields no entry rather than an error, so callers degrade to
// "no conflict known" instead of blocking resume.
func ActiveSessions() (map[string]ActiveSession, error) {
	root, err := ClaudeRoot()
	if err != nil {
		return nil, err
	}
	dir := filepath.Join(root, "sessions")
	entries, err := os.ReadDir(dir)
	if err != nil {
		if os.IsNotExist(err) {
			return map[string]ActiveSession{}, nil
		}
		return nil, err
	}
	out := make(map[string]ActiveSession)
	for _, e := range entries {
		if e.IsDir() || !strings.HasSuffix(e.Name(), ".json") {
			continue
		}
		b, err := os.ReadFile(filepath.Join(dir, e.Name()))
		if err != nil {
			continue
		}
		var a ActiveSession
		if err := json.Unmarshal(b, &a); err != nil {
			continue
		}
		if a.SessionID == "" || a.PID <= 0 || !processAlive(a.PID) {
			continue
		}
		out[a.SessionID] = a
	}
	return out, nil
}

// processAlive reports whether pid names a running process. signal 0 performs
// the permission/existence check without delivering a signal: nil means alive,
// EPERM means alive-but-not-ours, ESRCH means gone.
func processAlive(pid int) bool {
	err := syscall.Kill(pid, 0)
	return err == nil || err == syscall.EPERM
}

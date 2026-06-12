package session

import (
	"bufio"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"
)

// Event is one line of a Claude Code session JSONL transcript.
// Unknown fields are tolerated; only fields used by the UI are decoded.
type Event struct {
	Type        string          `json:"type"`
	UUID        string          `json:"uuid"`
	ParentUUID  string          `json:"parentUuid"`
	SessionID   string          `json:"sessionId"`
	CWD         string          `json:"cwd"`
	GitBranch   string          `json:"gitBranch"`
	Version     string          `json:"version"`
	IsMeta      bool            `json:"isMeta"`
	IsSidechain bool            `json:"isSidechain"`
	Timestamp   string          `json:"timestamp"`
	Message     json.RawMessage `json:"message"`
	CustomTitle string          `json:"customTitle"`
	AITitle     string          `json:"aiTitle"`
}

// Message is the inner Claude API message, simplified.
type Message struct {
	Role    string          `json:"role"`
	Content json.RawMessage `json:"content"`
}

// ContentBlock covers both string content and tool/text blocks.
type ContentBlock struct {
	Type  string `json:"type"`
	Text  string `json:"text"`
	Name  string `json:"name"`  // tool name
	Input json.RawMessage `json:"input"`
}

// Session is a parsed view of one .jsonl transcript.
type Session struct {
	ID          string
	Path        string
	Project     *Project
	Size        int64
	ModTime     time.Time
	FirstPrompt string
	CustomTitle string // user-set name via /rename or sidebar
	AITitle     string // auto-generated summary
	LastTime    time.Time
	NumEvents   int
	NumUser     int
	NumAsst     int
	LoadDur     time.Duration // wall time spent in LoadHeader during the scan
	GitBranch   string
	Version     string
	CWD         string
}

// DisplayName picks the best label for the session: user rename > AI title >
// first user prompt. Empty if nothing usable was recorded.
func (s *Session) DisplayName() string {
	switch {
	case s.CustomTitle != "":
		return s.CustomTitle
	case s.AITitle != "":
		return s.AITitle
	default:
		return s.FirstPrompt
	}
}

// Project groups sessions for one working directory.
type Project struct {
	Dir      string // raw dir name in ~/.claude/projects (encoded)
	CWD      string // best-effort decoded cwd
	Path     string // absolute path to the project dir
	Sessions []*Session
	ModTime  time.Time
}

// Home returns the Claude projects root.
func Home() (string, error) {
	if v := os.Getenv("CLAUDE_HOME"); v != "" {
		return filepath.Join(v, "projects"), nil
	}
	h, err := os.UserHomeDir()
	if err != nil {
		return "", err
	}
	return filepath.Join(h, ".claude", "projects"), nil
}

// DecodeDirToCWD reverses the dir-name encoding ("/" -> "-"). Lossy: a real
// "-" in the path collides. Best-effort only — used for display.
func DecodeDirToCWD(dir string) string {
	if dir == "" {
		return ""
	}
	if strings.HasPrefix(dir, "-") {
		return "/" + strings.ReplaceAll(dir[1:], "-", "/")
	}
	return strings.ReplaceAll(dir, "-", "/")
}

// ListProjects scans the projects root and returns all projects with sessions
// loaded (header-only, see LoadHeader).
func ListProjects() ([]*Project, error) {
	root, err := Home()
	if err != nil {
		return nil, err
	}
	entries, err := os.ReadDir(root)
	if err != nil {
		return nil, err
	}
	var out []*Project
	for _, e := range entries {
		if !e.IsDir() {
			continue
		}
		p := &Project{
			Dir:  e.Name(),
			CWD:  DecodeDirToCWD(e.Name()),
			Path: filepath.Join(root, e.Name()),
		}
		if err := p.loadSessions(); err != nil {
			// keep going, surface in caller
			continue
		}
		if len(p.Sessions) == 0 {
			continue
		}
		// project mtime = newest session mtime; prefer any session's recorded
		// cwd over the lossy dir-name decode for display.
		for _, s := range p.Sessions {
			if s.ModTime.After(p.ModTime) {
				p.ModTime = s.ModTime
			}
			if s.CWD != "" && p.CWD != s.CWD {
				p.CWD = s.CWD
			}
		}
		out = append(out, p)
	}
	sort.Slice(out, func(i, j int) bool {
		return out[i].ModTime.After(out[j].ModTime)
	})
	return out, nil
}

func (p *Project) loadSessions() error {
	entries, err := os.ReadDir(p.Path)
	if err != nil {
		return err
	}
	for _, e := range entries {
		if e.IsDir() || !strings.HasSuffix(e.Name(), ".jsonl") {
			continue
		}
		fi, err := e.Info()
		if err != nil {
			continue
		}
		s := &Session{
			ID:      strings.TrimSuffix(e.Name(), ".jsonl"),
			Path:    filepath.Join(p.Path, e.Name()),
			Project: p,
			Size:    fi.Size(),
			ModTime: fi.ModTime(),
		}
		// header parse: first user prompt, counts. cheap enough on demand.
		t0 := time.Now()
		_ = s.LoadHeader()
		s.LoadDur = time.Since(t0)
		p.Sessions = append(p.Sessions, s)
	}
	sort.Slice(p.Sessions, func(i, j int) bool {
		return p.Sessions[i].ModTime.After(p.Sessions[j].ModTime)
	})
	return nil
}

// LoadHeader streams the JSONL once to fill summary fields without holding
// the whole transcript in memory.
func (s *Session) LoadHeader() error {
	f, err := os.Open(s.Path)
	if err != nil {
		return err
	}
	defer f.Close()
	scanner := bufio.NewScanner(f)
	scanner.Buffer(make([]byte, 1<<20), 16<<20)
	for scanner.Scan() {
		line := scanner.Bytes()
		var ev Event
		if err := json.Unmarshal(line, &ev); err != nil {
			continue
		}
		s.NumEvents++
		if ev.CWD != "" && s.CWD == "" {
			s.CWD = ev.CWD
		}
		if ev.GitBranch != "" {
			s.GitBranch = ev.GitBranch
		}
		if ev.Version != "" {
			s.Version = ev.Version
		}
		if t, err := time.Parse(time.RFC3339Nano, ev.Timestamp); err == nil {
			if t.After(s.LastTime) {
				s.LastTime = t
			}
		}
		switch ev.Type {
		case "user":
			s.NumUser++
			if s.FirstPrompt == "" && !ev.IsMeta && !ev.IsSidechain {
				s.FirstPrompt = firstText(ev.Message)
			}
		case "assistant":
			s.NumAsst++
		case "custom-title":
			if ev.CustomTitle != "" {
				s.CustomTitle = ev.CustomTitle
			}
		case "ai-title":
			if ev.AITitle != "" {
				s.AITitle = ev.AITitle
			}
		}
	}
	return scanner.Err()
}

// LoadEvents streams every event to fn. Caller decides what to keep.
func (s *Session) LoadEvents(fn func(Event) bool) error {
	f, err := os.Open(s.Path)
	if err != nil {
		return err
	}
	defer f.Close()
	scanner := bufio.NewScanner(f)
	scanner.Buffer(make([]byte, 1<<20), 32<<20)
	for scanner.Scan() {
		var ev Event
		if err := json.Unmarshal(scanner.Bytes(), &ev); err != nil {
			continue
		}
		if !fn(ev) {
			return nil
		}
	}
	return scanner.Err()
}

// Delete removes the transcript file. Caller must have confirmed.
func (s *Session) Delete() error {
	return os.Remove(s.Path)
}

// DeleteAll removes every session file in the project. Returns the number of
// files removed and the first error encountered (deletion continues past
// individual failures so partial cleanup still happens). Caller must have
// confirmed.
func (p *Project) DeleteAll() (int, error) {
	var firstErr error
	n := 0
	for _, s := range p.Sessions {
		if err := s.Delete(); err != nil {
			if firstErr == nil {
				firstErr = err
			}
			continue
		}
		n++
	}
	p.Sessions = nil
	return n, firstErr
}

// firstText pulls a short text excerpt from a Message.Content payload.
// Content can be a JSON string OR an array of blocks.
func firstText(raw json.RawMessage) string {
	if len(raw) == 0 {
		return ""
	}
	var msg Message
	if err := json.Unmarshal(raw, &msg); err != nil {
		return ""
	}
	// try string
	var s string
	if err := json.Unmarshal(msg.Content, &s); err == nil {
		return cleanPrompt(s)
	}
	// try array of blocks
	var blocks []ContentBlock
	if err := json.Unmarshal(msg.Content, &blocks); err == nil {
		for _, b := range blocks {
			if b.Type == "text" && b.Text != "" {
				return cleanPrompt(b.Text)
			}
		}
	}
	return ""
}

// cleanPrompt strips the Claude Code shell wrappers so the list shows the
// actual user intent rather than scaffolding.
func cleanPrompt(s string) string {
	s = strings.TrimSpace(s)
	// drop common wrappers
	wrappers := []string{
		"<local-command-caveat>",
		"<command-name>",
		"<command-message>",
		"<command-args>",
		"<local-command-stdout>",
		"<local-command-stderr>",
		"<system-reminder>",
	}
	for _, w := range wrappers {
		if strings.HasPrefix(s, w) {
			return ""
		}
	}
	if i := strings.IndexByte(s, '\n'); i > 0 {
		s = s[:i]
	}
	if len(s) > 200 {
		s = s[:200] + "…"
	}
	return s
}

// HumanSize renders bytes as KB/MB.
func HumanSize(n int64) string {
	switch {
	case n < 1024:
		return fmt.Sprintf("%dB", n)
	case n < 1024*1024:
		return fmt.Sprintf("%.1fK", float64(n)/1024)
	default:
		return fmt.Sprintf("%.1fM", float64(n)/(1024*1024))
	}
}

// ExtractMessageText pulls displayable text out of any event for the preview pane.
func ExtractMessageText(ev Event) string {
	if len(ev.Message) == 0 {
		return ""
	}
	var msg Message
	if err := json.Unmarshal(ev.Message, &msg); err != nil {
		return ""
	}
	var sb strings.Builder
	// string content
	var str string
	if err := json.Unmarshal(msg.Content, &str); err == nil {
		sb.WriteString(str)
		return sb.String()
	}
	var blocks []ContentBlock
	if err := json.Unmarshal(msg.Content, &blocks); err != nil {
		return ""
	}
	for _, b := range blocks {
		switch b.Type {
		case "text":
			sb.WriteString(b.Text)
			sb.WriteString("\n")
		case "tool_use":
			fmt.Fprintf(&sb, "[tool_use %s]\n", b.Name)
		case "tool_result":
			sb.WriteString("[tool_result]\n")
		}
	}
	return sb.String()
}

// ReadAll loads every event into memory. Use for preview/export only on
// reasonably-sized transcripts.
func (s *Session) ReadAll() ([]Event, error) {
	var evs []Event
	err := s.LoadEvents(func(e Event) bool {
		evs = append(evs, e)
		return true
	})
	return evs, err
}

// Discard io.Discard alias kept for callers that want to count without reading.
var _ = io.Discard

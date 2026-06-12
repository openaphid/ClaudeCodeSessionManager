package ui

import (
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/openaphid/ccsm/internal/session"
)

// TestPreviewTiming measures renderPreview cost on real transcripts to
// attribute slow launches. Run with: go test ./internal/ui -run TestPreviewTiming -v
func TestPreviewTiming(t *testing.T) {
	root, err := session.Home()
	if err != nil {
		t.Skip("no projects root")
	}
	if _, err := os.Stat(root); err != nil {
		t.Skip("no projects root")
	}
	projects, err := session.ListProjects()
	if err != nil || len(projects) == 0 {
		t.Skip("no projects")
	}
	// measure the sessions a real launch would preview first, plus the largest
	var targets []*session.Session
	targets = append(targets, projects[0].Sessions[0]) // selected on launch
	var largest *session.Session
	for _, p := range projects {
		for _, s := range p.Sessions {
			if largest == nil || s.Size > largest.Size {
				largest = s
			}
		}
	}
	if largest != targets[0] {
		targets = append(targets, largest)
	}
	for _, s := range targets {
		for _, md := range []bool{false, true} {
			t0 := time.Now()
			out := renderPreview(s, 100, md)
			t.Logf("markdown=%-5v %8s %-40s -> %s (%d chars)",
				md, session.HumanSize(s.Size), filepath.Base(s.Path), time.Since(t0).Round(time.Millisecond), len(out))
		}
	}
}

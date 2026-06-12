package main

import (
	"flag"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"time"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/muesli/termenv"

	"github.com/openaphid/ccsm/internal/session"
	"github.com/openaphid/ccsm/internal/ui"
)

var version = "dev"

func main() {
	procStart := time.Now()
	var (
		showVersion = flag.Bool("version", false, "print version and exit")
		listOnly    = flag.Bool("list", false, "print sessions and exit (no TUI)")
		timing      = flag.Bool("timing", false, "print startup-scan timing report and exit (no TUI)")
	)
	flag.Parse()

	if *showVersion {
		fmt.Println("ccsm", version)
		return
	}

	scanStart := time.Now()
	projects, err := session.ListProjects()
	scanDur := time.Since(scanStart)
	if err != nil {
		fmt.Fprintln(os.Stderr, "error:", err)
		os.Exit(1)
	}
	logTiming(projects, scanDur)

	if *timing {
		printTiming(projects, scanDur)
		return
	}

	if *listOnly {
		printList(projects)
		return
	}

	if len(projects) == 0 {
		fmt.Fprintln(os.Stderr, "no Claude Code sessions found")
		return
	}

	// Resolve dark/light before bubbletea takes over stdin: the OSC background
	// query is only answered reliably while the terminal is still in normal
	// mode. Doing it later races bubbletea's input reader and stalls 5s.
	ui.SetMarkdownDark(termenv.HasDarkBackground())

	m := ui.NewModel(projects).WithStartTime(procStart)
	prog := tea.NewProgram(m, tea.WithAltScreen())
	final, err := prog.Run()
	if err != nil {
		fmt.Fprintln(os.Stderr, "tui error:", err)
		os.Exit(1)
	}

	fm, ok := final.(ui.Model)
	if !ok {
		return
	}
	// firstframe = process start -> first layout+preview rendered. The gap
	// between this and the scan line is bubbletea/terminal/glamour init.
	appendTimingLine("firstframe=%dms scan=%dms", fm.FirstFrame().Milliseconds(), scanDur.Milliseconds())
	if id, cwd, ok := fm.ResumeRequest(); ok {
		if err := ui.Resume(id, cwd); err != nil {
			fmt.Fprintln(os.Stderr, "resume failed:", err)
			os.Exit(1)
		}
		return
	}
	if cwd, ok := fm.NewSessionRequest(); ok {
		if err := ui.NewSession(cwd); err != nil {
			fmt.Fprintln(os.Stderr, "new session failed:", err)
			os.Exit(1)
		}
	}
}

// appendTimingLine appends one timestamped line to the timing log in the
// cache dir. Best effort: any failure is silently ignored — never block or
// break startup/shutdown.
func appendTimingLine(format string, args ...any) {
	cache, err := os.UserCacheDir()
	if err != nil {
		return
	}
	dir := filepath.Join(cache, "ccsm")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return
	}
	f, err := os.OpenFile(filepath.Join(dir, "timing.log"), os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0o644)
	if err != nil {
		return
	}
	defer f.Close()
	fmt.Fprintf(f, "%s %s\n", time.Now().Format(time.RFC3339), fmt.Sprintf(format, args...))
}

// logTiming appends one line per launch so occasional slow launches get
// captured during real use without re-running anything.
func logTiming(projects []*session.Project, total time.Duration) {
	var (
		files   int
		bytes   int64
		parse   time.Duration
		slowest *session.Session
	)
	for _, p := range projects {
		for _, s := range p.Sessions {
			files++
			bytes += s.Size
			parse += s.LoadDur
			if slowest == nil || s.LoadDur > slowest.LoadDur {
				slowest = s
			}
		}
	}
	slow := ""
	if slowest != nil {
		slow = fmt.Sprintf(" slowest=%dms:%s", slowest.LoadDur.Milliseconds(), slowest.Path)
	}
	appendTimingLine("total=%dms parse=%dms files=%d bytes=%d%s",
		total.Milliseconds(), parse.Milliseconds(), files, bytes, slow)
}

// printTiming reports where the startup scan spent its time, so occasional
// slow launches can be attributed to specific files or projects.
func printTiming(projects []*session.Project, total time.Duration) {
	var all []*session.Session
	var sum time.Duration
	for _, p := range projects {
		var pd time.Duration
		for _, s := range p.Sessions {
			pd += s.LoadDur
			all = append(all, s)
		}
		sum += pd
		fmt.Printf("%8s  %3d sessions  %s\n", pd.Round(time.Millisecond), len(p.Sessions), p.CWD)
	}
	sort.Slice(all, func(i, j int) bool { return all[i].LoadDur > all[j].LoadDur })
	fmt.Printf("\nslowest sessions:\n")
	for i, s := range all {
		if i >= 10 {
			break
		}
		fmt.Printf("%8s  %8s  %6d events  %s\n",
			s.LoadDur.Round(time.Millisecond), session.HumanSize(s.Size), s.NumEvents, s.Path)
	}
	fmt.Printf("\ntotal scan %s (header parse %s, other %s)\n",
		total.Round(time.Millisecond), sum.Round(time.Millisecond), (total - sum).Round(time.Millisecond))
}

func printList(projects []*session.Project) {
	for _, p := range projects {
		fmt.Printf("# %s (%d sessions)\n", p.CWD, len(p.Sessions))
		for _, s := range p.Sessions {
			fmt.Printf("  %s  %s  %s  %s\n",
				s.ID,
				s.ModTime.Format("2006-01-02 15:04"),
				session.HumanSize(s.Size),
				s.DisplayName(),
			)
		}
	}
}

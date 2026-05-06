package main

import (
	"flag"
	"fmt"
	"os"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/openaphid/ccsm/internal/session"
	"github.com/openaphid/ccsm/internal/ui"
)

var version = "dev"

func main() {
	var (
		showVersion = flag.Bool("version", false, "print version and exit")
		listOnly    = flag.Bool("list", false, "print sessions and exit (no TUI)")
	)
	flag.Parse()

	if *showVersion {
		fmt.Println("ccsm", version)
		return
	}

	projects, err := session.ListProjects()
	if err != nil {
		fmt.Fprintln(os.Stderr, "error:", err)
		os.Exit(1)
	}

	if *listOnly {
		printList(projects)
		return
	}

	if len(projects) == 0 {
		fmt.Fprintln(os.Stderr, "no Claude Code sessions found")
		return
	}

	m := ui.NewModel(projects)
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
	if id, cwd, ok := fm.ResumeRequest(); ok {
		if err := ui.Resume(id, cwd); err != nil {
			fmt.Fprintln(os.Stderr, "resume failed:", err)
			os.Exit(1)
		}
	}
}

func printList(projects []*session.Project) {
	for _, p := range projects {
		fmt.Printf("# %s (%d sessions)\n", p.CWD, len(p.Sessions))
		for _, s := range p.Sessions {
			fmt.Printf("  %s  %s  %s  %s\n",
				s.ID,
				s.ModTime.Format("2006-01-02 15:04"),
				session.HumanSize(s.Size),
				s.FirstPrompt,
			)
		}
	}
}

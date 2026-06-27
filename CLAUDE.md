# CLAUDE.md

This file provides guidance to Claude Code (claude.ai/code) when working with code in this repository.

## Project

`ccsm` (Claude Code Session Manager). Interactive Go CLI for browsing, previewing, deleting, and resuming the JSONL session transcripts that Claude Code writes under `~/.claude/projects/<encoded-cwd>/<session-uuid>.jsonl`.

## Commands

```bash
go build ./...                         # build
go run ./cmd/ccsm                      # launch TUI
go run ./cmd/ccsm -list                # dump sessions, no TUI
go run ./cmd/ccsm -version
go test ./...                          # all tests
go test ./internal/session -run TestLoadHeader  # single test
go vet ./...
```

The TUI assumes a TTY (`tea.WithAltScreen`). Use `-list` for non-TTY contexts.

## Architecture

Three packages, one binary.

- `cmd/ccsm/main.go` — flag parsing, calls `session.ListProjects`, runs the bubbletea program, then re-execs `claude --resume <id>` if the user picked a resume target. The resume call happens *after* `prog.Run` returns so the alt-screen has been torn down and stdin/tty are clean.
- `internal/session` — pure data layer. No UI deps.
  - `Home()` resolves projects root: `$CLAUDE_HOME/projects` if set, else `~/.claude/projects`.
  - `ListProjects` scans the root, builds a `*Project` per dir, and calls `LoadHeader` on every transcript so the list view has counts + first-prompt without holding full transcripts in memory.
  - `Session.LoadHeader` streams JSONL with `bufio.Scanner` (1 MiB initial / 16 MiB max line) — transcripts can be hundreds of MiB.
  - `Session.LoadEvents(fn)` is the streaming reader used by the preview pane; the callback returns `false` to stop early.
  - `DecodeDirToCWD` reverses Claude Code's dir encoding (leading `/` becomes `-`, every `/` becomes `-`). Lossy: a real `-` in the path collides. Display only — never round-trip back to disk.
  - `State` (`state.go`) is ccsm's own per-session/per-project flag memory at `<claude-home>/ccsm/state.json` (`LoadState`/`Save`). Best-effort like `ActiveSessions`: missing/malformed → empty maps, write errors are non-fatal. Claude Code persists no launch flags anywhere, so this is the only record of which flags a session resumed with.
- `internal/ui` — bubbletea model.
  - Layout: left column = projects list (top) + sessions list (bottom); right column = scrollable preview viewport. Tab switches focus between the two left lists; preview always tracks the selected session.
  - `Model.Update` short-circuits all input when `confirm != confirmNone` (destructive y/N) and, separately, when `dialog != dialogNone` (the resume/new flag flow). The two states are mutually exclusive; a flag dialog that hits a live-session conflict closes itself and hands off to `confirmResumeActive`, carrying flags via `pendingArgs`.
  - Resume/new can pass extra `claude` flags (e.g. `--chrome`). `enter`/`r` opens a Simple/With-flags chooser (default remembered per session); `n` opens a flag input (default remembered per project cwd). Flags are split by `splitArgs` (`args.go`, quote-aware; drops `--resume`/`-r`/`--continue`/`-c`).
  - Resume is implemented as "set `resumeReq` (with `ExtraArgs`), return `tea.Quit`, let main re-exec." Do NOT shell out from inside `Update` — bubbletea owns the terminal.

## Conventions

- JSONL schema is owned by Claude Code and drifts. `Event`/`Message` decode only the fields the UI needs; everything else falls into the implicit unknown-field bucket. Don't add hard validation.
- `cleanPrompt` filters out `<command-name>`, `<local-command-*>`, `<system-reminder>`, and similar wrappers when picking the "first user prompt" preview — those are scaffolding, not user intent. Add new wrapper tags here if Claude Code adds them.
- Destructive ops (currently just delete) require a y/N confirmation rendered in the footer. Never bypass — these are user data files.
- Keep `internal/session` UI-free so it stays usable from `-list` mode and any future scripting entry points.

## External

No runtime services. Reads `~/.claude/projects/` and shells out to `claude` (must be on `$PATH`) for resume/new — appending any user-chosen extra flags after `--resume <id>`. Honors `$CLAUDE_HOME` to override the projects root. Writes its own flag memory to `<claude-home>/ccsm/state.json` (see `session.State`).

Also reads the live-session registry Claude Code keeps at `<claude-home>/sessions/<pid>.json` (each holds `pid`, `sessionId`, `cwd`, `status`). `session.ActiveSessions` parses these, drops entries whose pid is dead (`syscall.Kill(pid,0)`), and keys the rest by `sessionId`. The UI checks this at resume time and pops a confirm (`confirmResumeActive`) before opening a transcript a running `claude` already owns — two processes appending the same JSONL corrupt its history. Best-effort: a missing registry dir or read error fails open (resume proceeds).

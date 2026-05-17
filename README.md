# ccsm — Claude Code Session Manager

Interactive Go TUI for browsing, previewing, deleting, and resuming the JSONL session transcripts that [Claude Code](https://claude.com/claude-code) writes to `~/.claude/projects/`.

## Install

```bash
go install github.com/openaphid/ClaudeCodeSessionManager/cmd/ccsm@latest
```

Or build from source:

```bash
git clone https://github.com/openaphid/ClaudeCodeSessionManager.git
cd ClaudeCodeSessionManager
make install        # → $GOBIN/ccsm
```

## Usage

```bash
ccsm                # launch TUI
ccsm -list          # dump sessions to stdout (non-TTY)
ccsm -version
```

### Keys

| Key       | Action                                       |
|-----------|----------------------------------------------|
| `↑/↓`     | Move selection in focused list               |
| `Tab`     | Switch focus: projects ↔ sessions ↔ preview  |
| `←/→`     | Page preview (when preview focused)          |
| `Enter`   | Resume selected session via `claude --resume`|
| `d`       | Delete selected session (y/N confirm)        |
| `q` / `Ctrl+C` | Quit                                    |

## Layout

```
┌──────────────┬────────────────────────┐
│ Projects     │ Preview                │
├──────────────┤  (selected session     │
│ Sessions     │   transcript, scroll)  │
└──────────────┴────────────────────────┘
```

## Configuration

- `$CLAUDE_HOME` — override projects root (default: `~/.claude`). Sessions read from `$CLAUDE_HOME/projects/`.
- `claude` must be on `$PATH` for resume.

## Develop

```bash
make build          # → bin/ccsm
make run            # go run ./cmd/ccsm
make test
make vet
```

See [`CLAUDE.md`](CLAUDE.md) for architecture notes.

## License

MIT

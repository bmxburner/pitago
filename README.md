# gotui

![gotui screenshot](resources/Screenshot.png)

A polished Terminal User Interface (TUI) frontend for the `pi` agent, built with Bubble Tea. `pi --mode rpc` serves as the backend (multi-provider, tools, sessions, compaction), while gotui provides a rich terminal interface communicating over JSONL.

## Overview

gotui wraps the `pi` agent in a beautiful terminal interface with:

- Real-time chat with streaming responses
- Sidebar showing session info, model details, token usage, and git status
- Command palette with builtins and extension commands
- File mention support (`@file`)
- Recent models picker
- Clipboard integration (`Ctrl+Y` to yank last answer)

## Requirements

- Go ≥ 1.27
- `pi` available in PATH (or set `PI_BIN` to its path)
- An API key for your provider (e.g. `ANTHROPIC_API_KEY`), or run `/login` inside the app to save one to the keystore

## Quick Start

```bash
# From the repo root — everything lives under src/
go run ./src

# Resume most recent session
go run ./src -c

# Specify provider and model
go run ./src --provider anthropic --model claude-sonnet-4-20250514

# Don't persist a session
go run ./src --no-session

# Enable mouse support (sidebar click + wheel scroll)
go run ./src --mouse
```

## Build & Test

```bash
# Vet
go vet ./...

# Build
go build -o /tmp/gotui ./src

# Test
go test ./...
```

## Keybindings

| Key            | Action                                                                                           |
| -------------- | ------------------------------------------------------------------------------------------------ |
| `Enter`        | Send (idle) / steer (while running)                                                              |
| `Esc`          | Cancel running turn (clear queue + abort)                                                        |
| `Ctrl+C`       | Quit                                                                                             |
| `Ctrl+N`       | New session                                                                                      |
| `Ctrl+P`       | Cycle model                                                                                      |
| `Ctrl+R`       | Recent-models picker                                                                             |
| `Ctrl+B`       | Hide/show sidebar (hide for clean drag-select of chat only)                                      |
| `Ctrl+Y`       | Yank last assistant answer to clipboard (chat-only, no sidebar)                                  |
| `Ctrl+O`       | Yank picker: choose any message to copy (sidebar stays visible)                                  |
| `Alt+1…5`      | Jump straight to a recent model                                                                  |
| `Tab`          | Complete `/command` or `@file`                                                                   |
| `@`            | Mention a file (fuzzy finder, like pi — Tab/Enter completes, text goes to pi raw)                |
| `↑↓ PgUp PgDn` | Scroll chat (when input is single-line)                                                          |
| `Mouse`        | Off by default so you can highlight-to-copy; run with `--mouse` for sidebar click + wheel scroll |

### Copying text

- Default: use your terminal's normal mouse selection to copy
- To copy only chat content (without sidebar): hide the sidebar with `/sidebar` or `Ctrl+B`, then select
- Or use `Ctrl+Y` / `/yank` (`/copy`) to copy the last assistant answer directly to clipboard
- Or press `Ctrl+O` to pick any message to copy — sidebar stays visible
- With `--mouse`: hold `Option`/`Shift` (terminal-dependent) to select

## Commands

Type `/` to open the command popup. Two kinds:

### Builtins

(intercepted locally, re-implemented over RPC):

- `/model` — change model
- `/recent` — recent models picker
- `/yank` / `/copy` — copy last answer to clipboard
- `/sidebar` — hide/show sidebar
- `/thinking` — toggle thinking level
- `/tree` — show file tree
- `/settings` — open settings
- `/login` / `/logout` — manage API keys
- `/reload` — reload extensions
- `/new` — new session
- `/quit` — exit
- `/session` — session management

### Extension / prompt / skill

(from pi's `get_commands`): forwarded to pi as `/...` prompt text, executed server-side.

## Sidebar

The right column (pi session-panel style) shows:

- **SESSION** — first message + session id
- Model + thinking level
- Context bar (`used/total tkns`)
- **Stats | Tokens** — `time`, `last`, `speed`, `turns`, context left, `in/out/total/cache/cost`
- Clickable **RECENT MODELS** and **COMMANDS** counts
- **WORKSPACE** — git status (branch + per-file `+add -del`, refreshed every 10s and after each turn)
- Current working directory

Hidden on terminals narrower than 80 columns.

## Layout

```
src/main.go       # entry: flags, spawn pi, wire packages, run
src/app/          # TUI shell: model, update, view, palette, dialogs,
                  # recent models, workspace git panel, styles, utils
src/builtin/      # pi builtin features re-implemented over RPC
                  # Origin "pi" = pi TUI builtin, "gotui" = ours (/recent)
src/extension/    # extension protocol: UI requests, permission replies,
                  # command sources (extension/prompt/skill)
src/pirpc/        # JSONL transport for `pi --mode rpc` (stdlib only)
```

**Rules**: `app` never imports `builtin`/`extension` (wired in main via `UseBuiltins`); `builtin` operates on `*app.Model`, `extension` is pure protocol helpers.

## Configuration Files

- `~/.config/gotui/keys.json` (0600) — saved API keys (`/login`, `/logout`)
- `~/.config/gotui/recent_models.json` — recent models (max 5)
- `/tmp/gotui-pi-stderr.log` — pi child stderr

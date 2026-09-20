# openpi

![openpi screenshot](resources/Screenshot.png)

A polished Terminal User Interface (TUI) frontend for the `pi` agent, built with Bubble Tea. `pi --mode rpc` serves as the backend (multi-provider, tools, sessions, compaction), while openpi provides a rich terminal interface communicating over JSONL.

## Overview

openpi wraps the `pi` agent in a beautiful terminal interface with:

- Real-time chat with streaming responses
- Sidebar showing session info, model details, token usage, and git status
- Command palette with builtins and extension commands
- File mention support (`@file`)
- Image send via `@photo.png` or dropping/pasting file paths — or `Ctrl+V` on a copied screenshot (macOS needs `pngpaste`, Linux uses `wl-paste`/`xclip`). Vision over RPC, max 5 × 8MB. Paths collapse into `[Image N]` chips; `↓` moves into the tray, `←→` picks a chip, `⌫` deletes it, `Esc` back
- Recent models picker
- Clipboard integration (`Ctrl+Y` to yank last answer)

## Requirements

- Go ≥ 1.27
- `pi` available in PATH (or set `PI_BIN` to its path)
- An API key for your provider (e.g. `ANTHROPIC_API_KEY`), or run `/login` inside the app to save one to the keystore

## Install

Two ways: install once into a `bin` on your PATH, or keep the binary
local to the project. No Go toolchain needed for the release binaries —
only for building from source.

> Release assets are named `openpi-<tag>-<os>-<arch>`
> (`openpi-v0.0.1-linux-amd64`, `openpi-v0.0.1-darwin-arm64`, …).
> Windows gets a `.exe`. Get them from the
> [Releases page](https://github.com/cavaldos/openpi/releases).

### Option 1 — install into bin (use anywhere)

From a release binary:

```bash
# Pick the asset matching your OS/arch, e.g. v0.0.1 on Linux
curl -L -o openpi https://github.com/cavaldos/openpi/releases/download/v0.0.1/openpi-v0.0.1-linux-amd64
chmod +x openpi
sudo mv openpi /usr/local/bin/openpi   # or ~/go/bin, ~/.local/bin — any dir on PATH
openpi --version
```

From source:

```bash
git clone https://github.com/cavaldos/openpi.git
cd openpi
script/build.sh                      # outputs bin/openpi (VERSION defaults to git tag/commit)
sudo cp bin/openpi /usr/local/bin/openpi
openpi --version
```

Uninstall (if installed into bin):

```bash
sudo rm /usr/local/bin/openpi   # or wherever you put it: ~/go/bin, ~/.local/bin, …
rm -rf ~/.config/openpi         # optional: remove saved API keys + recent models
```

### Option 2 — run local in the project (no install)

From a release binary:

```bash
cd /path/to/your-project
curl -L -o openpi https://github.com/cavaldos/openpi/releases/download/v0.0.1/openpi-v0.0.1-linux-amd64
chmod +x openpi
./openpi --version
```

From source:

```bash
git clone https://github.com/cavaldos/openpi.git
cd openpi
script/build.sh
./bin/openpi --version
```

Or skip the build and run straight from source:

```bash
script/run.sh
```

## Quick Start

```bash
# From the repo root — everything lives under src/
go run ./src

# Open another working directory (flags first, then the path)
go run ./src ~/Code/workspace
go run ./src --cwd ~/Code/workspace

# Resume most recent session
go run ./src -c

# Specify provider and model
go run ./src --provider anthropic --model claude-sonnet-4-20250514

# Don't persist a session
go run ./src --no-session

# Mouse (sidebar click + wheel scroll) is on by default;
# opt out with --mouse=false for plain highlight-to-copy
go run ./src --mouse=false
```

## Build & Test

```bash
# Vet
go vet ./...

# Build
go build -o /tmp/openpi ./src

# Test
go test ./...
```

## Release

Tag push triggers the `release` workflow, which cross-builds
(linux-amd64, darwin-amd64/arm64, windows-amd64) and publishes a GitHub Release:

```bash
script/test-cicd.sh        # check vet + test + builds locally first
script/release.sh v0.0.1
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
| `Ctrl+V`       | Paste text — or screenshot data (pngpaste/wl-paste/xclip); errors shown, terminal Cmd+V still works |
| `Backspace`    | Empty input + image tray → remove last `[Image N]` chip                                          |
| `↓` (+tray)    | Cursor into the image tray · `←→` pick a chip · `⌫` delete it · `Esc` back to input               |
| `Ctrl+O`       | Yank picker: choose any message to copy (sidebar stays visible)                                  |
| `Ctrl+G`       | Expand/collapse tool output: write content, read results, diffs (collapsed previews like pi)     |
| `Alt+1…5`      | Jump straight to a recent model                                                                  |
| `Tab`          | Complete `/command` or `@file`                                                                   |
| `@`            | Mention a file (fuzzy finder, like pi — Tab/Enter completes, text goes to pi raw; `@*.png/.jpg/.gif/.webp` also sends vision) |
| `↑↓ PgUp PgDn` | Scroll chat (when input is single-line) |
| `Alt+↑↓ PgUp PgDn Home End` or `Ctrl+↑↓ PgUp PgDn Home End` | Scroll sidebar (keyboard, always works) |
| `Mouse wheel` | On by default: hover sidebar to scroll it, chat otherwise; `--mouse=false` disables |

### Copying text

- With mouse on (default): hold `Option`/`Shift` (terminal-dependent) to select
- Or run with `--mouse=false` for plain highlight-to-copy
- To copy only chat content (without sidebar): hide the sidebar with `/sidebar` or `Ctrl+B`, then select
- Or use `Ctrl+Y` / `/yank` (`/copy`) to copy the last assistant answer directly to clipboard
- Or press `Ctrl+O` to pick any message to copy — sidebar stays visible
- With mouse on: hold `Option`/`Shift` (terminal-dependent) to select

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
src/app/          # TUI shell: Model, update, view, dialogs, sidepanels;
                  # thin wiring over src/components (no pure logic here)
src/components/  # feature components (pure, testable without TUI state):
                   # chat (Block), mention (@file lookup), image (@image vision),
                   # palette (/ match),
                  # pet (status core), recent (models store), yank (copy),
                  # format (shared string/number helpers)
src/builtin/      # pi builtin features re-implemented over RPC
                  # Origin "pi" = pi TUI builtin, "openpi" = ours (/recent)
src/extension/    # extension protocol: UI requests, permission replies,
                  # command sources (extension/prompt/skill)
src/pirpc/        # JSONL transport for `pi --mode rpc` (stdlib only)
tests/            # integration tests (black-box, public API only).
                  # Unit white-box tests stay next to code as *_test.go
                  # (Go requires this for private access) — see tests/README.md
```

**Rules**: `app` never imports `builtin`/`extension` (wired in main via `UseBuiltins`); `builtin` operates on `*app.Model`, `extension` is pure protocol helpers.

## Configuration Files

- `~/.config/openpi/keys.json` (0600) — saved API keys (`/login`, `/logout`)
- `~/.config/openpi/recent_models.json` — recent models (max 5)
- `/tmp/openpi-pi-stderr.log` — pi child stderr

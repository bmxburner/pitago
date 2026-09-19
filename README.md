# gotui

A beautiful TUI frontend for the `pi` agent, built with Bubble Tea.

## Requirements

- Go ≥ 1.27
- `pi` available in PATH (or set `PI_BIN` to its path)
- An API key for your provider (e.g. `ANTHROPIC_API_KEY`), or run `/login` inside the app to save one to the keystore

## Build

```bash
go vet ./...
go build -o /tmp/gotui .
```

## Run

```bash
# new session
go run .

# resume most recent session
go run . -c

# specify provider and model
go run . --provider anthropic --model claude-sonnet-4-20250514

# don't persist a session
go run . --no-session
```

## Keystore

Saved API keys live at `~/.config/gotui/keys.json` (mode 0600). Use `/login <provider>` inside the app to add one, and `/logout <provider>` to remove it. On login, gotui restarts `pi` so the new key takes effect immediately.

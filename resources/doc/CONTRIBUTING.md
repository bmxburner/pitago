# Contributing Guide — pitago

## Requirements

- Go ≥ 1.27
- `pi` on PATH (or point `PI_BIN` at the pi binary)
- A provider API key (e.g. `ANTHROPIC_API_KEY`), or run `/login` inside the app

## Dev loop

```bash
script/run.sh            # run straight from source, no build needed
go run ./src ~/Code/workspace   # open another working directory
go run ./src -c          # resume the most recent session
```

## Code layout

```
src/main.go       # entry: flags, spawn pi, wire packages, run
src/app/          # TUI shell (Bubble Tea Model/Update/View, dialogs, sidepanels)
src/components/   # pure components, testable without TUI state
src/builtin/      # locally-executed builtins over RPC (/model, /login, /settings...)
src/extension/    # pi extension protocol (permission UI, command sources)
src/pirpc/        # JSONL client for `pi --mode rpc` (stdlib only)
tests/            # black-box integration tests
```

### 3 import rules (mandatory)

1. `src/app` **never** imports `src/builtin` or `src/extension`.
   The only wiring point is `main.go` via `m.UseBuiltins(...)`.
2. `src/builtin` may operate on `*app.Model` (already wired, no reverse import).
3. `src/extension` is pure helpers (no TUI state).

### `/` command origin rule

- `OriginPi` — re-implements a pi TUI builtin over RPC (pi's own builtins don't
  run over RPC, so they must be re-implemented, e.g. `/model`).
- `OriginPitago` — pitago's own command (e.g. `/recent`).
- Everything from `get_commands` (extension/prompt/skill) runs pi-side by
  forwarding prompt text.

## Tests

- **White-box unit tests** (`*_test.go` next to the code in `src/...`): may touch
  private fields/methods. Go requires these files to live in the same folder as
  the code.
- **Black-box integration tests** (`tests/integration/`): public API only
  (`app.New`, `Model.View`/`Update`, `src/components/*`).
- RPC tests (`src/pirpc`) need a real `pi` on PATH — they self-skip without it.

```bash
go test ./src/app/ ./src/builtin/ ./src/pirpc/ ./tests/...
```

## Checklist before opening a PR

```bash
go vet ./...
go build -o /tmp/pitago ./src
go test ./...
```

## Commit messages

Conventional commits: `feat:`, `fix:`, `chore:`, `docs:`.
Example: `feat: pi-style welcome header on empty chat`.

## Release

```bash
script/test-cicd.sh        # vet + test + trial builds locally first
script/release.sh v0.0.4   # tag push → workflow builds 4 binaries + publishes a Release
```

Assets are named `pitago-<tag>-<os>-<arch>` (Windows gets `.exe`) — renaming
them means the install links in `README.md` (Option 1) must be updated too.

## Never commit

API keys, secrets, or `~/.config/pitago/keys.json`. Pass keys via environment
variables (`env:`), see Configuration Files in `README.md`.

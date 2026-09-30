# Follow-up: the MCP edit / remove surface

Status: **proposal, not started.** This PR carries no code. It exists so
the redesign discussion lives where it can be reviewed, before anyone
builds on a `pirpc` contract that is still moving.

Context: the inline editor and the two-press remove gate that shipped in
the MCP settings panel are usable, but they are a stopgap. The gate
prompt, the caret and the long-value window all work (see `d121cb1`);
what follows is about the shape of the editing surface itself.

## What exists today

- `Edit server…` swaps pane 3 for an inline form, prefilled from the file
  pi reported — never from a re-resolved path, so an edit lands in the
  project-scoped `mcp.json` rather than the agent dir.
- Per-field caret: `←` `→` move, `Home`/`End` jump, `Backspace` /
  `Delete` remove, typing inserts at the caret, rune-indexed. Moving is
  not editing, so arrows never arm the Esc discard gate.
- Long values render as a window that follows the caret
  (`mcpCaretWindow`): the left edge is elided and the caret is always in
  view, so an args or env string longer than the pane stays editable.
- `Remove server…` is a two-press gate. The armed prompt renders inline
  on the action row using the same predicate the gate enforces, window
  included, and matches the action kind from the payload rather than the
  label text.
- Every write re-reads the file, compares against the bytes it was parsed
  from, snapshots a `.bak`, and refuses to write if the backup fails.

## What is wrong with it

1. **The pane is a fixed-width cell.** A value wider than the pane is
   editable only because the window scrolls one line at a time. There is
   no way to see the whole value while editing it, and the only cue that
   a row is windowed is the `…` on one side.
2. **The caret is per-field, not per-document.** Args, env and headers
   are structured values (an argv list, a JSON object). Editing them as
   one opaque string is honest about the file format but wrong about the
   domain: a stray space in `args` silently changes the command, and it
   saves cleanly, so nothing catches it.
3. **No undo.** One wrong keystroke inside a field is only recoverable by
   closing the form and re-opening it. For a field that may hold a
   hand-written API token, that is not an acceptable default.
4. **No diff before save.** `Ctrl+S` writes the whole entry. Nothing
   shows what is about to change.
5. **The remove gate names the server but not the file.** A project-scoped
   `mcp.json` and the global one can hold entries of the same name, and
   the prompt does not say which one is about to be edited.

## Options

### A. Full-screen editor overlay for the entry

`Edit server…` opens a dedicated overlay — the standalone `mcpedit` form
already exists — instead of mutating pane 3.

- Pros: the whole value is visible or soft-wrapped; room for a field
  legend, a transport switcher, and a footer naming the file; reuses
  `updateMcpForm` / `renderMcpForm` unchanged; the hub's pane 3 can stay a
  read-only summary while editing.
- Cons: a context switch for a small edit; if the hub goes full-screen
  the standalone panel's duplicate form becomes a liability, so the
  decision includes whether that surface keeps its own editor.

### B. Stay in-pane, make the focused row an editor

Keep pane 3, but expand the focused field to the full pane width — label
above value, or the label column collapsed while editing.

- Pros: no context switch; the detail block above stays visible.
- Cons: fights the fixed-height three-pane layout. The pane scrolls, so
  expanding one row shifts the others.

### C. Structured fields, not strings

Parse args into an argv list and env/headers into key/value rows, with a
JSON escape hatch for the raw form.

- Pros: the domain is actually right; a stray space cannot corrupt a
  command; the remove gate can name the exact key being dropped.
- Cons: the most work. `pirpc` must round-trip what it cannot model —
  unknown keys, ordering — and the risk of drifting from pi's own parser
  is real, because that parser is the thing moving fastest.

## Recommendation

**A now, C once `pirpc` settles.**

A is a small change with most of the benefit, and it gives the later
structured work a home that does not require reworking the hub layout.
B is the worst of both: it fights the layout for a subset of A's benefit.
C is right but gated on pi's config contract, which is the part moving
fastest — building a field model on it now would be building on sand.

## Preconditions before starting

- [ ] `pirpc`'s MCP surface stable enough to build a field model on
- [ ] Decide whether the standalone panel keeps its own editor at all
- [ ] Undo model agreed: per-field ring buffer vs. whole-form snapshot
- [ ] Whether the remove gate names the file as well as the server
- [ ] Whether `Edit server…` should keep its home in the actions column
      at all, or become a keybinding on the server row
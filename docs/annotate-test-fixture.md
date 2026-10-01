# Annotate Test Fixture

A deliberately varied document for exercising `plannotator annotate`. Every block type
here is something worth leaving a comment on.

## 1. Prose paragraph

The quick brown fox jumps over the lazy dog. This paragraph exists so you can select a
sentence and leave an inline note about tone, wording, or a factual claim you disagree
with. Selection comments should anchor to the smallest sensible range.

## 2. Bullet list

- First item, with a **bold** run and some `inline code`
- Second item, which is arguably wrong and deserves a callout
  - Nested bullet, because nested lists break naive selection logic
  - Another nested bullet
- Third item

## 3. Numbered list

1. Parse the input
2. Validate the schema
3. Emit the output
4. Record the decision in the journal

## 4. Code fence

```go
func annotate(doc string, opts Options) (string, error) {
	if doc == "" {
		return "", ErrEmptyDocument
	}
	lines := strings.Split(doc, "\n")
	for i, line := range lines {
		if strings.HasPrefix(line, "## ") {
			lines[i] = highlight(line)
		}
	}
	return strings.Join(lines, "\n"), nil
}
```

Inline prose referencing `annotate()` continues after the fence, which is where
selection often gets confused about which code block it landed in.

## 5. Table

| Symbol | Meaning | Blast radius |
|---|---|---|
| `✓` | passed | none |
| `✗` | failed | CI stops |
| `▸` | deferred | tracked next session |
| `▴` | escalated | needs a human |

Two cells in this table are worth flagging: the "Blast radius" for `▴` is vague, and
"tracked next session" has no owner column.

## 6. Blockquote

> Deferred work is not a plan. If it has no owner and no date, it is a wish.
> — the one true rule

Quote blocks should be commentable as a unit.

## 7. Checklist

- [x] Read the diff
- [x] Run the tests
- [ ] Reply to the reviewer
- [ ] Ship it

## 8. Link and reference

See [the annotate docs](https://example.com/docs/annotate) and the
[project README](../README.md) for background.

## 9. Long paragraph for wrapping tests

When a paragraph wraps across several terminal rows, highlight overlays and copied text
selection have to agree on where each row begins and ends. If the overlay is off by one
column the user sees a selection that starts mid-word, which reads as a bug even when the
underlying range is correct. Drag-select across a wrapped paragraph, through a list
marker, and over a line containing a tab are all cases that historically regressed. The
gutter column should never be included in the copied range, and double-click should
expand to word boundaries without swallowing the two-cell gutter prefix.

## 10. Mixed content tail

Final mixed paragraph: inline `code`, **bold**, *italic*, a ~~strikethrough~~ span, and a
[link](https://example.com). Ends here.
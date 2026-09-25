package app

// The receiver side of the Plannotator TUI surface.
//
// plannotator-tui owns review state. Every annotation is written to a per-document
// record under the Plannotator data directory, and every successful send appends a
// `deliveries` entry naming the annotation ids it covered. That record is the host
// adapter: after the TUI exits, Pitago reads it and learns exactly what the user
// sent, with no terminal scraping and no change to plannotator-tui itself.
//
// The keying below (data dir, project name, history slug) is a port of
// plannotator-tui-schema::datadir, and the Markdown renderer is a port of
// plannotator-tui's export::feedback. Both are covered by the TUI's own test
// vectors in reviewstore_test.go so the two stay byte-identical.

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strings"
	"time"
	"unicode"
)

// Review outcomes, normalized across every surface.
const (
	reviewOutcomeApproved  = "approved"
	reviewOutcomeAnnotated = "annotated"
	reviewOutcomeDismissed = "dismissed"
)

func reviewDataDir() string {
	home, err := os.UserHomeDir()
	if err != nil {
		home = ""
	}
	return reviewDataDirFor(os.Getenv, home, func(path string) bool {
		st, err := os.Stat(path)
		return err == nil && st.IsDir()
	})
}

// reviewDataDirFor mirrors plannotator_tui_schema::data_dir: PLANNOTATOR_DATA_DIR
// wins and expands a leading ~, then an existing ~/.plannotator, then
// XDG_DATA_HOME/plannotator when it is set and absolute, then ~/.plannotator.
func reviewDataDirFor(env func(string) string, home string, exists func(string) bool) string {
	if dir := strings.TrimSpace(env("PLANNOTATOR_DATA_DIR")); dir != "" {
		if dir == "~" {
			return home
		}
		if rest, ok := strings.CutPrefix(dir, "~/"); ok {
			return filepath.Join(home, rest)
		}
		if rest, ok := strings.CutPrefix(dir, `~\`); ok {
			return filepath.Join(home, rest)
		}
		return dir
	}
	legacy := filepath.Join(home, ".plannotator")
	if exists(legacy) {
		return legacy
	}
	if xdg := strings.TrimSpace(env("XDG_DATA_HOME")); xdg != "" && filepath.IsAbs(xdg) {
		return filepath.Join(xdg, "plannotator")
	}
	return legacy
}

// reviewSanitizeTag mirrors Plannotator's sanitizeTag: lowercase, spaces and
// underscores to hyphens, drop everything outside [a-z0-9-], collapse hyphens,
// trim the edges, cap at 30, and fail under 2 characters.
func reviewSanitizeTag(name string) (string, bool) {
	var out []rune
	pendingHyphen := false
	for _, ch := range strings.ToLower(strings.TrimSpace(name)) {
		if unicode.IsSpace(ch) || ch == '_' {
			ch = '-'
		}
		switch {
		case ch >= 'a' && ch <= 'z', ch >= '0' && ch <= '9':
			if pendingHyphen && len(out) > 0 {
				out = append(out, '-')
			}
			pendingHyphen = false
			out = append(out, ch)
		default:
			pendingHyphen = true
		}
	}
	tag := string(out)
	if runes := []rune(tag); len(runes) > 30 {
		tag = string(runes[:30])
	}
	tag = strings.Trim(tag, "-")
	return tag, len([]rune(tag)) >= 2
}

func reviewPathBase(path string) string {
	if path == "" {
		return ""
	}
	base := filepath.Base(filepath.Clean(path))
	if base == "." || base == string(filepath.Separator) {
		return ""
	}
	return base
}

// reviewProjectName mirrors plannotator_tui_schema::project_name: the enclosing git
// repository's name, else the folder's own name, else "_unknown".
func reviewProjectName(toplevel, folder string) string {
	if toplevel != "" {
		if tag, ok := reviewSanitizeTag(reviewPathBase(toplevel)); ok {
			return tag
		}
	}
	if tag, ok := reviewSanitizeTag(reviewPathBase(folder)); ok {
		return tag
	}
	return "_unknown"
}

func reviewGitToplevel(folder string) string {
	out, err := exec.Command("git", "-C", folder, "rev-parse", "--show-toplevel").Output()
	if err != nil {
		return ""
	}
	return strings.TrimSpace(string(out))
}

// reviewProjectNameFor is the per-run project name for a document's folder.
func reviewProjectNameFor(folder string) string {
	return reviewProjectName(reviewGitToplevel(folder), folder)
}

// reviewHistorySlug mirrors deriveAnnotateHistorySlug: `annotate-<base>-<8 hex>`, the
// hex being the first 8 of sha256 over the resolved path exactly as given.
func reviewHistorySlug(resolvedPath string) string {
	base := ""
	pending := false
	name := resolvedPath
	if idx := strings.LastIndexAny(name, `/\`); idx >= 0 {
		name = name[idx+1:]
	}
	if name != "" {
		var out []rune
		for _, ch := range strings.ToLower(name) {
			if (ch >= 'a' && ch <= 'z') || (ch >= '0' && ch <= '9') {
				if pending && len(out) > 0 {
					out = append(out, '-')
				}
				pending = false
				out = append(out, ch)
			} else {
				pending = true
			}
		}
		base = string(out)
		if runes := []rune(base); len(runes) > 60 {
			base = string(runes[:60])
		}
		base = strings.Trim(base, "-")
	}
	if base == "" {
		base = "document"
	}
	digest := sha256.Sum256([]byte(resolvedPath))
	return fmt.Sprintf("annotate-%s-%s", base, hex.EncodeToString(digest[:])[:8])
}

// reviewAnnotationsDir is the directory holding plannotator-tui's record for one document.
func reviewAnnotationsDir(dataDir, project, resolvedPath string) string {
	return filepath.Join(dataDir, "clients", "plannotator-tui", "annotations", project, reviewHistorySlug(resolvedPath))
}

func reviewRecordPath(dataDir, project, resolvedPath string) string {
	return filepath.Join(reviewAnnotationsDir(dataDir, project, resolvedPath), "annotations.json")
}

// ---- the record plannotator-tui writes ----

type reviewSourceRange struct {
	Start   int    `json:"start"`
	End     int    `json:"end"`
	Version string `json:"version"`
}

type reviewTUIAnchor struct {
	Kind   string            `json:"kind"`
	Quote  string            `json:"quote"`
	Source reviewSourceRange `json:"source"`
}

type reviewAnchor struct {
	OriginalText string           `json:"originalText"`
	Quote        string           `json:"quote"`
	TUI          *reviewTUIAnchor `json:"plannotator_tui"`
}

type reviewReply struct {
	Author *string `json:"author"`
	Body   string  `json:"body"`
}

type reviewAnnotation struct {
	ID        string        `json:"id"`
	Anchor    reviewAnchor  `json:"anchor"`
	Body      string        `json:"body"`
	State     string        `json:"state"`
	CreatedAt string        `json:"created_at"`
	UpdatedAt string        `json:"updated_at"`
	Replies   []reviewReply `json:"replies"`
}

type reviewDelivery struct {
	At            string   `json:"at"`
	Target        string   `json:"target"`
	AnnotationIDs []string `json:"annotation_ids"`
}

type reviewRecord struct {
	Path        string             `json:"path"`
	Annotations []reviewAnnotation `json:"annotations"`
	Deliveries  []reviewDelivery   `json:"deliveries"`
	Archived    []reviewAnnotation `json:"archived"`
}

func readReviewRecord(path string) (*reviewRecord, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	record := &reviewRecord{}
	if err := json.Unmarshal(data, record); err != nil {
		return nil, fmt.Errorf("parsing %s: %w", filepath.Base(path), err)
	}
	return record, nil
}

// annotation looks an id up in the archive first: a sent annotation is archived
// whole, so its anchor and body survive the send that removed it from the pending set.
func (r *reviewRecord) annotation(id string) (reviewAnnotation, bool) {
	for _, group := range [][]reviewAnnotation{r.Archived, r.Annotations} {
		for _, a := range group {
			if a.ID == id {
				return a, true
			}
		}
	}
	return reviewAnnotation{}, false
}

// sentSince returns the annotation ids covered by deliveries made at or after since,
// in send order, deduplicated. The one-second slack absorbs a record written in the
// same second Pitago recorded its launch.
func (r *reviewRecord) sentSince(since time.Time) []string {
	threshold := since.Add(-time.Second)
	seen := map[string]bool{}
	var ids []string
	for _, delivery := range r.Deliveries {
		at, err := time.Parse(time.RFC3339, strings.TrimSpace(delivery.At))
		if err != nil || at.Before(threshold) {
			continue
		}
		for _, id := range delivery.AnnotationIDs {
			if !seen[id] {
				seen[id] = true
				ids = append(ids, id)
			}
		}
	}
	return ids
}

// ---- capture ----

// ReviewCapture is the normalized outcome every surface produces.
type ReviewCapture struct {
	Outcome         string
	Feedback        string
	AnnotationCount int
	Files           []string
	Target          string
	DeliveredAt     string
	Surface         ReviewSurface
}

type reviewItem struct {
	annotation reviewAnnotation
	start, end int
}

func (a reviewAnnotation) span() (int, int) {
	if a.Anchor.TUI != nil {
		return a.Anchor.TUI.Source.Start, a.Anchor.TUI.Source.End
	}
	return 0, 0
}

func (a reviewAnnotation) kind() string {
	if a.Anchor.TUI != nil && a.Anchor.TUI.Kind != "" {
		return a.Anchor.TUI.Kind
	}
	return "comment"
}

func (a *reviewTUIAnchor) quoteOrEmpty() string {
	if a == nil {
		return ""
	}
	return a.Quote
}

// reviewCaptureRecord turns one document's record into entries plus the delivery
// metadata. It is empty when nothing was sent during this review.
func reviewCaptureRecord(record *reviewRecord, source, name string, since time.Time) (items []reviewItem, deliveredAt string, targets []string) {
	for _, id := range record.sentSince(since) {
		annotation, ok := record.annotation(id)
		if !ok {
			continue
		}
		start, end := annotation.span()
		if source == "" {
			start, end = 0, 0
		}
		items = append(items, reviewItem{annotation: annotation, start: start, end: end})
	}
	for _, delivery := range record.Deliveries {
		if at, err := time.Parse(time.RFC3339, strings.TrimSpace(delivery.At)); err == nil && at.After(since.Add(-time.Second)) {
			deliveredAt = delivery.At
			if delivery.Target != "" {
				targets = append(targets, delivery.Target)
			}
		}
	}
	sort.SliceStable(items, func(i, j int) bool { return items[i].start < items[j].start })
	return items, deliveredAt, targets
}

// reviewOutcome classifies the sent annotations: a send whose notes are all
// approval is an approval, anything else is an annotated review, and no send is a
// dismissed review.
func reviewOutcome(items []reviewItem) string {
	if len(items) == 0 {
		return reviewOutcomeDismissed
	}
	for _, item := range items {
		if item.annotation.kind() != "looks_good" {
			return reviewOutcomeAnnotated
		}
	}
	return reviewOutcomeApproved
}

// ---- feedback rendering (port of plannotator-tui's export::feedback) ----

func reviewLineAt(source string, offset int) int {
	if offset < 0 {
		offset = 0
	}
	if offset > len(source) {
		offset = len(source)
	}
	return 1 + strings.Count(source[:offset], "\n")
}

func reviewLineSpan(source string, start, end int) (int, int) {
	if end > len(source) {
		end = len(source)
	}
	last := end - 1
	if last < start {
		last = start
	}
	return reviewLineAt(source, start), reviewLineAt(source, last)
}

// reviewFence uses a fence longer than any backtick run inside the text, so quoted
// markdown cannot escape the block.
func reviewFence(text string) string {
	longest := 0
	run := 0
	for _, ch := range text {
		if ch == '`' {
			run++
			if run > longest {
				longest = run
			}
			continue
		}
		run = 0
	}
	fence := strings.Repeat("`", max(longest, 2)+1)
	return fence + "\n" + text + "\n" + fence + "\n"
}

func reviewSingleLine(text string) string {
	return strings.Join(strings.Fields(text), " ")
}

func reviewQuoteLines(text string) string {
	return strings.ReplaceAll(text, "\n", "\n> ")
}

func reviewSlice(source string, start, end int) string {
	if start < 0 {
		start = 0
	}
	if end > len(source) {
		end = len(source)
	}
	if start > end {
		return ""
	}
	return source[start:end]
}

// reviewFeedback renders the agent-facing Markdown for a review. This is what the
// TUI would have delivered, reproduced so Pitago can send the same text as a Pi turn.
func reviewFeedback(source, name string, items []reviewItem) string {
	if len(items) == 0 {
		return "No annotations."
	}
	var out strings.Builder
	fmt.Fprintf(&out, "# Annotations on %s\n\n", name)
	for i, item := range items {
		quoted := reviewSlice(source, item.start, item.end)
		startLine, endLine := reviewLineSpan(source, item.start, item.end)
		label := fmt.Sprintf("line %d", startLine)
		if endLine != startLine {
			label = fmt.Sprintf("lines %d\u2013%d", startLine, endLine)
		}
		fmt.Fprintf(&out, "## Annotation %d (%s)\n", i+1, label)
		body := strings.TrimSpace(item.annotation.Body)
		switch item.annotation.kind() {
		case "delete":
			out.WriteString("Remove this:\n")
			out.WriteString(reviewFence(quoted))
			if body == "" {
				fmt.Fprintf(&out, "> %s\n", "I don't want this.")
			} else {
				fmt.Fprintf(&out, "> %s\n", body)
			}
		case "looks_good":
			fmt.Fprintf(&out, "Looks good: \"%s\"\n", reviewSingleLine(quoted))
			if body != "" {
				fmt.Fprintf(&out, "> %s\n", reviewQuoteLines(body))
			}
		default:
			fmt.Fprintf(&out, "Comment on: \"%s\"\n", reviewSingleLine(quoted))
			fmt.Fprintf(&out, "> %s\n", reviewQuoteLines(body))
		}
		for _, reply := range item.annotation.Replies {
			who := "reply"
			if reply.Author != nil && *reply.Author != "" {
				who = *reply.Author
			}
			fmt.Fprintf(&out, "- **Reply (%s):** %s\n", who, strings.ReplaceAll(reply.Body, "\n", "\n  "))
		}
		out.WriteString("\n")
	}
	return out.String()
}

// ---- dispatch-side capture ----

func reviewReadSource(path string) (string, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return "", err
	}
	return string(data), nil
}

// captureReview reads back what a TUI review actually sent. since is when Pitago
// launched the TUI, so deliveries from an earlier session are never mistaken for
// this one.
func captureReview(req ReviewRequest, since time.Time, sourcePath string) (ReviewCapture, error) {
	capture := ReviewCapture{Surface: ReviewSurfaceTUI}
	dataDir := reviewDataDir()
	switch req.Kind {
	case ReviewFolder:
		target, err := reviewTarget(req)
		if err != nil {
			return capture, err
		}
		project := reviewProjectNameFor(target)
		dir := filepath.Join(dataDir, "clients", "plannotator-tui", "annotations", project)
		entries, err := os.ReadDir(dir)
		if err != nil {
			if os.IsNotExist(err) {
				return capture, nil
			}
			return capture, err
		}
		var blocks []string
		for _, entry := range entries {
			if !entry.IsDir() {
				continue
			}
			record, err := readReviewRecord(filepath.Join(dir, entry.Name(), "annotations.json"))
			if err != nil {
				continue
			}
			source, err := reviewReadSource(record.Path)
			if err != nil {
				continue
			}
			items, deliveredAt, targets := reviewCaptureRecord(record, source, reviewPathBase(record.Path), since)
			if len(items) == 0 {
				continue
			}
			blocks = append(blocks, reviewFeedback(source, reviewPathBase(record.Path), items))
			capture.AnnotationCount += len(items)
			capture.Files = append(capture.Files, record.Path)
			if deliveredAt != "" {
				capture.DeliveredAt = deliveredAt
			}
			capture.Target = strings.Join(targets, ", ")
		}
		if len(blocks) == 0 {
			capture.Outcome = reviewOutcomeDismissed
			return capture, nil
		}
		capture.Outcome = reviewOutcomeAnnotated
		capture.Feedback = strings.Join(blocks, "\n")
		return capture, nil
	default:
		docPath := sourcePath
		if req.Kind == ReviewFile || req.Kind == ReviewFolder {
			target, err := reviewTarget(req)
			if err != nil {
				return capture, err
			}
			docPath = target
		}
		if docPath == "" {
			return capture, fmt.Errorf("no reviewed document")
		}
		project := reviewProjectNameFor(filepath.Dir(docPath))
		record, err := readReviewRecord(reviewRecordPath(dataDir, project, docPath))
		if err != nil {
			if os.IsNotExist(err) {
				// The TUI never wrote a record: nothing was annotated.
				capture.Outcome = reviewOutcomeDismissed
				return capture, nil
			}
			return capture, err
		}
		source := req.Content
		if source == "" {
			if source, err = reviewReadSource(docPath); err != nil {
				return capture, err
			}
		}
		items, deliveredAt, targets := reviewCaptureRecord(record, source, reviewPathBase(docPath), since)
		capture.Outcome = reviewOutcome(items)
		capture.AnnotationCount = len(items)
		capture.DeliveredAt = deliveredAt
		capture.Target = strings.Join(targets, ", ")
		if len(items) > 0 {
			capture.Feedback = reviewFeedback(source, reviewPathBase(docPath), items)
			capture.Files = []string{record.Path}
		}
		return capture, nil
	}
}

func writeReviewRecord(path string, record *reviewRecord) error {
	data, err := json.MarshalIndent(record, "", "  ")
	if err != nil {
		return err
	}
	return os.WriteFile(path, data, 0o644)
}

package output

import (
	"encoding/json"
	"fmt"
	"io"
	"os"
	"sort"
	"strings"
	"time"
	"unicode"

	"github.com/charmbracelet/lipgloss"
	"github.com/charmbracelet/lipgloss/table"
)

// Human reads a JSON response body and writes a human-friendly rendering to stdout.
func Human(body io.Reader) error {
	return HumanTo(os.Stdout, body)
}

// HumanTo reads a JSON response body and writes a human-friendly rendering to w.
// If the body isn't JSON, the raw bytes are written through unchanged.
func HumanTo(w io.Writer, body io.Reader) error {
	data, err := io.ReadAll(body)
	if err != nil {
		return fmt.Errorf("reading response: %w", err)
	}
	if len(strings.TrimSpace(string(data))) == 0 {
		fmt.Fprintln(w, "✓ ok")
		return nil
	}

	var v any
	if err := json.Unmarshal(data, &v); err != nil {
		_, werr := w.Write(data)
		fmt.Fprintln(w)
		return werr
	}
	renderValue(w, v)
	return nil
}

// HumanError prints a plain-text error message to stderr.
func HumanError(statusCode int, detail string) {
	HumanErrorTo(os.Stderr, statusCode, detail)
}

// HumanErrorTo prints a plain-text error message to w.
func HumanErrorTo(w io.Writer, statusCode int, detail string) {
	if detail == "" {
		detail = fmt.Sprintf("HTTP %d", statusCode)
	}
	if statusCode > 0 {
		fmt.Fprintf(w, "Error: %s (HTTP %d)\n", detail, statusCode)
	} else {
		fmt.Fprintf(w, "Error: %s\n", detail)
	}
}

func renderValue(w io.Writer, v any) {
	switch x := v.(type) {
	case []any:
		if len(x) == 0 {
			fmt.Fprintln(w, "No results.")
			return
		}
		renderTable(w, x)
	case map[string]any:
		renderObject(w, x)
	default:
		fmt.Fprintln(w, formatScalar(v))
	}
}

// renderObject decides how a top-level object renders based on which of its
// fields hold scalars vs. arrays. Objects with only an array field (e.g. list
// endpoints) become a table. Objects with a mix (e.g. AI endpoints that return
// both an `answer` string and a `sources` array) render each section in turn.
func renderObject(w io.Writer, obj map[string]any) {
	// Success envelope: {success: true, message: "...", <resource>: {...}}
	if s, ok := obj["success"].(bool); ok && s {
		if msg, ok := obj["message"].(string); ok && msg != "" {
			fmt.Fprintf(w, "✓ %s\n", msg)
		} else {
			fmt.Fprintln(w, "✓ ok")
		}
		for _, key := range sortedKeys(obj) {
			if key == "success" || key == "message" {
				continue
			}
			if inner, ok := obj[key].(map[string]any); ok {
				fmt.Fprintln(w)
				renderKeyValue(w, inner)
				return
			}
		}
		return
	}

	var scalarKeys, arrayKeys []string
	for _, k := range sortedKeys(obj) {
		if k == "pageInfo" {
			continue
		}
		if _, ok := obj[k].([]any); ok {
			arrayKeys = append(arrayKeys, k)
			continue
		}
		scalarKeys = append(scalarKeys, k)
	}

	// Pure list-wrapper: only one array field (plus optional pageInfo).
	if len(scalarKeys) == 0 && len(arrayKeys) == 1 {
		arr, _ := obj[arrayKeys[0]].([]any)
		if len(arr) == 0 {
			fmt.Fprintln(w, "No results.")
		} else {
			renderTable(w, arr)
		}
		renderPageInfo(w, obj)
		return
	}

	// No arrays: classic single resource.
	if len(arrayKeys) == 0 {
		renderKeyValue(w, obj)
		return
	}

	// Mixed: render scalars first, then each array under a heading.
	if len(scalarKeys) > 0 {
		renderSections(w, obj, scalarKeys)
	}
	for _, k := range arrayKeys {
		fmt.Fprintf(w, "\n%s\n", humanizeKey(k))
		arr, _ := obj[k].([]any)
		if len(arr) == 0 {
			fmt.Fprintln(w, "(none)")
			continue
		}
		renderTable(w, arr)
	}
	renderPageInfo(w, obj)
}

func renderPageInfo(w io.Writer, obj map[string]any) {
	pi, ok := obj["pageInfo"].(map[string]any)
	if !ok {
		return
	}
	hasMore, _ := pi["hasMore"].(bool)
	cursor, _ := pi["cursor"].(string)
	if hasMore && cursor != "" {
		fmt.Fprintf(w, "\nMore results available. Cursor: %s\n", cursor)
	}
}

// renderSections prints scalar fields. Short values use aligned key: value
// formatting; long or multi-line strings get their own heading and an
// indented body, so the text stays readable.
func renderSections(w io.Writer, obj map[string]any, keys []string) {
	keys = promote(keys, []string{"id", "name", "answer", "message", "description"})

	var shortKeys []string
	type longEntry struct {
		key string
		val string
	}
	var longEntries []longEntry

	for _, k := range keys {
		v := obj[k]
		s, ok := v.(string)
		if ok && (len(s) > 80 || strings.Contains(s, "\n")) {
			longEntries = append(longEntries, longEntry{k, s})
			continue
		}
		shortKeys = append(shortKeys, k)
	}

	if len(shortKeys) > 0 {
		sub := make(map[string]any, len(shortKeys))
		for _, k := range shortKeys {
			sub[k] = obj[k]
		}
		renderKeyValue(w, sub)
	}
	for _, e := range longEntries {
		if len(shortKeys) > 0 {
			fmt.Fprintln(w)
		}
		fmt.Fprintf(w, "%s\n", humanizeKey(e.key))
		for _, line := range strings.Split(e.val, "\n") {
			fmt.Fprintf(w, "  %s\n", line)
		}
	}
}

// renderTable prints rows as a lipgloss-rendered table with box-drawing
// borders. Headers render bold; identifier and timestamp columns render in
// muted colors so the eye can skip past them. Color is auto-disabled when
// stdout isn't a TTY (termenv's default renderer handles the downgrade).
func renderTable(w io.Writer, rows []any) {
	records := make([]map[string]any, 0, len(rows))
	for _, r := range rows {
		if m, ok := r.(map[string]any); ok {
			records = append(records, m)
		}
	}
	if len(records) == 0 {
		// Array of scalars — one per line.
		for _, r := range rows {
			fmt.Fprintln(w, formatScalar(r))
		}
		return
	}

	columns := pickColumns(records)
	headers := make([]string, len(columns))
	for i, c := range columns {
		headers[i] = humanizeKey(c)
	}

	t := table.New().
		Border(lipgloss.NormalBorder()).
		BorderStyle(lipgloss.NewStyle().Foreground(lipgloss.Color("240"))).
		Headers(headers...).
		StyleFunc(styleFor(columns))

	for _, rec := range records {
		row := make([]string, len(columns))
		for i, c := range columns {
			row[i] = truncate(formatScalar(rec[c]), 60)
		}
		t.Row(row...)
	}
	fmt.Fprintln(w, t.Render())
}

// styleFor returns a StyleFunc that dims identifier columns and greys out
// timestamps, keeping names and other scalars at default foreground.
func styleFor(columns []string) func(row, col int) lipgloss.Style {
	dim := lipgloss.NewStyle().Padding(0, 1).Foreground(lipgloss.Color("8"))
	grey := lipgloss.NewStyle().Padding(0, 1).Foreground(lipgloss.Color("244"))
	header := lipgloss.NewStyle().Padding(0, 1).Bold(true)
	base := lipgloss.NewStyle().Padding(0, 1)

	return func(row, col int) lipgloss.Style {
		if row == table.HeaderRow {
			return header
		}
		if col < 0 || col >= len(columns) {
			return base
		}
		switch columns[col] {
		case "id", "baseModelId", "connectionId", "parentId", "ownerId", "userId":
			return dim
		case "createdAt", "updatedAt", "deletedAt":
			return grey
		}
		if strings.HasSuffix(columns[col], "Id") {
			return dim
		}
		return base
	}
}

// renderKeyValue prints a single object as aligned key: value lines.
func renderKeyValue(w io.Writer, obj map[string]any) {
	keys := sortedKeys(obj)
	// Promote common identity fields to the top.
	keys = promote(keys, []string{"id", "name", "modelKind", "dialect", "type", "kind"})

	labels := make(map[string]string, len(keys))
	maxKey := 0
	for _, k := range keys {
		l := humanizeKey(k)
		labels[k] = l
		if len(l) > maxKey {
			maxKey = len(l)
		}
	}
	for _, k := range keys {
		v := obj[k]
		if isComplex(v) {
			fmt.Fprintf(w, "%-*s  %s\n", maxKey+1, labels[k]+":", summarizeComplex(v))
			continue
		}
		fmt.Fprintf(w, "%-*s  %s\n", maxKey+1, labels[k]+":", formatScalar(v))
	}
}

// pickColumns selects up to 6 scalar columns across the given records.
// Priority: id, name, then keys observed in insertion-ish order (alphabetical
// since we walked JSON), skipping complex types.
func pickColumns(records []map[string]any) []string {
	seen := map[string]bool{}
	var scalarKeys []string
	for _, rec := range records {
		for _, k := range sortedKeys(rec) {
			if seen[k] {
				continue
			}
			if isComplex(rec[k]) {
				continue
			}
			seen[k] = true
			scalarKeys = append(scalarKeys, k)
		}
	}
	preferred := []string{"id", "name", "modelKind", "dialect", "type", "kind", "status"}
	ordered := promote(scalarKeys, preferred)
	// Push timestamp-ish fields to the end so identity fields show first.
	ordered = demote(ordered, []string{"createdAt", "updatedAt", "deletedAt"})
	if len(ordered) > 6 {
		ordered = ordered[:6]
	}
	return ordered
}

func promote(keys, preferred []string) []string {
	set := make(map[string]bool, len(keys))
	for _, k := range keys {
		set[k] = true
	}
	out := make([]string, 0, len(keys))
	used := map[string]bool{}
	for _, p := range preferred {
		if set[p] {
			out = append(out, p)
			used[p] = true
		}
	}
	for _, k := range keys {
		if !used[k] {
			out = append(out, k)
		}
	}
	return out
}

func demote(keys, trailing []string) []string {
	trailSet := map[string]bool{}
	for _, t := range trailing {
		trailSet[t] = true
	}
	var head, tail []string
	for _, k := range keys {
		if trailSet[k] {
			tail = append(tail, k)
		} else {
			head = append(head, k)
		}
	}
	return append(head, tail...)
}

func sortedKeys(m map[string]any) []string {
	keys := make([]string, 0, len(m))
	for k := range m {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	return keys
}

func isComplex(v any) bool {
	switch v.(type) {
	case map[string]any, []any:
		return true
	}
	return false
}

func summarizeComplex(v any) string {
	switch x := v.(type) {
	case []any:
		return fmt.Sprintf("[%d items]", len(x))
	case map[string]any:
		return fmt.Sprintf("{%d fields}", len(x))
	}
	return ""
}

func formatScalar(v any) string {
	switch x := v.(type) {
	case nil:
		return "-"
	case string:
		if x == "" {
			return "-"
		}
		if t, ok := parseTime(x); ok {
			return relativeTime(t)
		}
		return x
	case bool:
		if x {
			return "true"
		}
		return "false"
	case float64:
		// JSON numbers decode as float64; print without trailing .0 when integral.
		if x == float64(int64(x)) {
			return fmt.Sprintf("%d", int64(x))
		}
		return fmt.Sprintf("%g", x)
	case json.Number:
		return x.String()
	}
	b, _ := json.Marshal(v)
	return string(b)
}

func parseTime(s string) (time.Time, bool) {
	// Common Omni API timestamp shapes.
	layouts := []string{time.RFC3339Nano, time.RFC3339, "2006-01-02T15:04:05"}
	for _, l := range layouts {
		if t, err := time.Parse(l, s); err == nil {
			return t, true
		}
	}
	return time.Time{}, false
}

func relativeTime(t time.Time) string {
	d := time.Since(t)
	if d < 0 {
		d = -d
	}
	switch {
	case d < time.Minute:
		return "just now"
	case d < time.Hour:
		return fmt.Sprintf("%dm ago", int(d.Minutes()))
	case d < 24*time.Hour:
		return fmt.Sprintf("%dh ago", int(d.Hours()))
	case d < 30*24*time.Hour:
		return fmt.Sprintf("%dd ago", int(d.Hours()/24))
	case d < 365*24*time.Hour:
		return fmt.Sprintf("%dmo ago", int(d.Hours()/(24*30)))
	default:
		return fmt.Sprintf("%dy ago", int(d.Hours()/(24*365)))
	}
}

func truncate(s string, max int) string {
	if max <= 1 || len(s) <= max {
		return s
	}
	return s[:max-1] + "…"
}

// humanizeKey converts API field names like "modelKind", "MODEL_KIND", or
// "created_at" into readable labels like "Model Kind" / "Created At".
// camelCase splits on case transitions; snake_case / kebab-case become spaces.
func humanizeKey(s string) string {
	if s == "" {
		return ""
	}
	s = strings.NewReplacer("_", " ", "-", " ").Replace(s)

	var b strings.Builder
	runes := []rune(s)
	for i, r := range runes {
		if i > 0 {
			prev := runes[i-1]
			var next rune
			if i+1 < len(runes) {
				next = runes[i+1]
			}
			switch {
			case (unicode.IsLower(prev) || unicode.IsDigit(prev)) && unicode.IsUpper(r):
				b.WriteRune(' ')
			case unicode.IsUpper(prev) && unicode.IsUpper(r) && unicode.IsLower(next):
				b.WriteRune(' ')
			}
		}
		b.WriteRune(r)
	}

	fields := strings.Fields(b.String())
	for i, f := range fields {
		rs := []rune(f)
		if len(rs) > 0 {
			rs[0] = unicode.ToUpper(rs[0])
			for j := 1; j < len(rs); j++ {
				rs[j] = unicode.ToLower(rs[j])
			}
		}
		fields[i] = string(rs)
	}
	return strings.Join(fields, " ")
}


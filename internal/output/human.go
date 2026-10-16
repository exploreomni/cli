package output

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"math"
	"os"
	"sort"
	"strconv"
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
	return HumanBytes(w, data)
}

// HumanBytes renders an already-read JSON body to w. Callers that have the
// bytes in hand use this so a large payload isn't read — and allocated — a
// second time.
func HumanBytes(w io.Writer, data []byte) error {
	if len(bytes.TrimSpace(data)) == 0 {
		fmt.Fprintln(w, "✓ ok")
		return nil
	}

	var v any
	if err := json.Unmarshal(data, &v); err != nil {
		_, werr := w.Write(data)
		fmt.Fprintln(w)
		return werr
	}
	renderValue(w, sanitizeJSON(v))
	return nil
}

// HumanError prints a plain-text error message to stderr.
func HumanError(statusCode int, detail string) {
	HumanErrorTo(os.Stderr, statusCode, detail)
}

// HumanErrorTo prints a plain-text error message to w.
func HumanErrorTo(w io.Writer, statusCode int, detail string) {
	detail = sanitize(detail)
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
		// Nulls are visual noise in human mode (e.g. a null `error` field on
		// a successful response renders as "Error: -" and looks scary).
		// Skip them; JSON mode still shows the full response.
		if v == nil {
			continue
		}
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
		Border(lipgloss.RoundedBorder()).
		BorderStyle(styleBorder).
		Headers(headers...).
		StyleFunc(styleFor(columns))

	for _, rec := range records {
		row := make([]string, len(columns))
		for i, c := range columns {
			row[i] = truncateCells(formatField(c, rec[c]), 60)
		}
		t.Row(row...)
	}
	fmt.Fprintln(w, t.Render())
}

// styleFor returns a StyleFunc that mutes identifier and timestamp columns,
// keeping names and other scalars at default foreground.
func styleFor(columns []string) func(row, col int) lipgloss.Style {
	dim, grey, header, base := styleMuted, styleMuted, styleHeader, styleCell

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
// Nulls are skipped — same rationale as renderSections.
func renderKeyValue(w io.Writer, obj map[string]any) {
	keys := sortedKeys(obj)
	// Promote common identity fields to the top.
	keys = promote(keys, []string{"id", "name", "modelKind", "dialect", "type", "kind"})

	visible := make([]string, 0, len(keys))
	for _, k := range keys {
		if obj[k] == nil {
			continue
		}
		visible = append(visible, k)
	}

	labels := make(map[string]string, len(visible))
	maxKey := 0
	for _, k := range visible {
		l := humanizeKey(k)
		labels[k] = l
		if len(l) > maxKey {
			maxKey = len(l)
		}
	}
	for _, k := range visible {
		v := obj[k]
		if isComplex(v) {
			fmt.Fprintf(w, "%-*s  %s\n", maxKey+1, labels[k]+":", summarizeComplex(v))
			continue
		}
		fmt.Fprintf(w, "%-*s  %s\n", maxKey+1, labels[k]+":", formatField(k, v))
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

// formatField renders a value under the field name it arrived with. An
// identifier is a value someone copies back into a command, so it keeps its
// digits ungrouped; everything else reads better with separators.
func formatField(key string, v any) string {
	if f, ok := v.(float64); ok && identifierKey(key) {
		return formatNumberPlain(f)
	}
	return formatScalar(v)
}

// identifierKey reports whether a field name reads as an identifier or a port
// rather than a magnitude.
func identifierKey(k string) bool {
	switch k {
	case "id", "port", "version":
		return true
	}
	return strings.HasSuffix(k, "Id") || strings.HasSuffix(k, "ID")
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
		return formatNumber(x)
	case json.Number:
		return x.String()
	}
	b, _ := json.Marshal(v)
	return string(b)
}

// formatNumber renders a JSON number for reading: no exponent, no invented
// precision, and thousands separators once the digits outrun a glance.
// Separators start at five digits so a year stays 2026 rather than 2,026.
func formatNumber(f float64) string {
	return formatNumberGrouped(f, 5)
}

// formatNumberPlain is formatNumber without separators, for fields named as
// identifiers: a grouped id is a value someone copies back wrong.
func formatNumberPlain(f float64) string {
	return formatNumberGrouped(f, math.MaxInt)
}

// formatNumberGrouped groups thousands once the integer part has minDigits digits.
func formatNumberGrouped(f float64, minDigits int) string {
	if math.IsNaN(f) || math.IsInf(f, 0) {
		return fmt.Sprintf("%g", f)
	}
	abs := math.Abs(f)
	integral := f == math.Trunc(f)
	// Outside these ranges the plain form is longer than it is useful. Whole
	// numbers get far more room: a 16-digit warehouse ID is a value someone
	// reads, not a magnitude they skim.
	switch {
	case abs == 0:
	case integral && abs >= 1e18:
		return fmt.Sprintf("%g", f)
	case !integral && (abs >= 1e15 || abs < 1e-6):
		return fmt.Sprintf("%g", f)
	}

	// Shortest form that round-trips: a coordinate keeps its digits. Model
	// formats, not this, decide decimals for query results.
	s := strconv.FormatFloat(f, 'f', -1, 64)

	sign := ""
	if strings.HasPrefix(s, "-") {
		sign, s = "-", s[1:]
	}
	intPart, frac := s, ""
	if i := strings.IndexByte(s, '.'); i >= 0 {
		intPart, frac = s[:i], s[i:]
	}
	if len(intPart) < minDigits {
		return sign + intPart + frac
	}

	var b strings.Builder
	for i := range intPart {
		if i > 0 && (len(intPart)-i)%3 == 0 {
			b.WriteByte(',')
		}
		b.WriteByte(intPart[i])
	}
	return sign + b.String() + frac
}

// truncateCells shortens s to max terminal cells. Runes aren't cells: a CJK
// label of 28 runes occupies 56 columns, which would blow the layout its
// width was budgeted for.
func truncateCells(s string, max int) string {
	switch {
	case lipgloss.Width(s) <= max:
		return s
	case max <= 0:
		return ""
	case max == 1:
		return "…"
	}
	var b strings.Builder
	used := 0
	for _, r := range s {
		w := lipgloss.Width(string(r))
		if used+w > max-1 {
			break
		}
		b.WriteRune(r)
		used += w
	}
	return b.String() + "…"
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

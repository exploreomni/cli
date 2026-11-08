package openapi

import (
	"bytes"
	"encoding/json"
	"fmt"
	"math"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"unicode/utf16"
	"unicode/utf8"

	"github.com/pb33f/libopenapi/datamodel/high/base"
)

// Violation is one problem --dry-run found in a request, located by Path: a
// dotted path into the body ("body" for the body itself, "body.fields[2]" for an
// array element), or "--flag" for a query flag.
type Violation struct {
	Path    string `json:"path"`
	Message string `json:"message"`
}

// uuidPattern accepts any RFC 4122 layout, version and variant alike. Being
// looser than the server only lets a rare bad value through to a 400; being
// stricter would block a valid request, which is what --dry-run must not do.
var uuidPattern = regexp.MustCompile(`^[0-9a-fA-F]{8}-[0-9a-fA-F]{4}-[0-9a-fA-F]{4}-[0-9a-fA-F]{4}-[0-9a-fA-F]{12}$`)

// ValidateBody checks a JSON request body against the operation's spec schema
// and returns every violation, not just the first, sorted by path. It is a
// static check: the embedded spec is the only input.
//
// Unknown keys are strict by default: an object schema that declares properties
// rejects any other key unless it explicitly allows extras (additionalProperties
// true or a schema). Only a small share of the spec's objects say
// additionalProperties: false, but the API rejects undeclared keys on all of
// them, so the absent case is treated like false.
//
// Wherever the check could disagree with a lenient server, it errs toward
// passing: oneOf is checked like anyOf, string lengths are counted the way that
// favors the caller, regexes Go cannot compile are skipped, and the only string
// format enforced is uuid.
func ValidateBody(schema *base.SchemaProxy, body []byte) []Violation {
	if schema == nil {
		return nil
	}
	dec := json.NewDecoder(bytes.NewReader(body))
	dec.UseNumber()
	var value interface{}
	if err := dec.Decode(&value); err != nil {
		return []Violation{{Path: "body", Message: "not valid JSON: " + err.Error()}}
	}
	v := &validator{}
	v.check(normalizeJSON(value), schema, "body", nil, true)
	return v.sorted()
}

type validator struct {
	out []Violation
}

func (v *validator) add(path, format string, args ...interface{}) {
	v.out = append(v.out, Violation{Path: path, Message: fmt.Sprintf(format, args...)})
}

func (v *validator) sorted() []Violation {
	sort.SliceStable(v.out, func(i, j int) bool { return v.out[i].Path < v.out[j].Path })
	// Two union branches can report the same problem; say it once.
	seen := map[Violation]bool{}
	out := v.out[:0]
	for _, x := range v.out {
		if !seen[x] {
			seen[x] = true
			out = append(out, x)
		}
	}
	return out
}

// sub runs a nested check in isolation and returns what it found, so unions
// can compare branches without committing their errors.
func sub(value interface{}, proxy *base.SchemaProxy, path string, allowed map[string]bool, strict bool) []Violation {
	v := &validator{}
	v.check(value, proxy, path, allowed, strict)
	return v.out
}

// check validates value against proxy. allowed carries property names declared
// by an enclosing schema (allOf siblings, or the object around a union) that
// count as known when deciding whether a key is unknown. strict switches the
// unknown-key check on; it is off for allOf members, whose keys are judged
// together by the composing schema.
func (v *validator) check(value interface{}, proxy *base.SchemaProxy, path string, allowed map[string]bool, strict bool) {
	if proxy == nil {
		return
	}
	sch := proxy.Schema()
	if sch == nil {
		return
	}

	if len(sch.Type) > 0 && !typeMatches(value, sch.Type) {
		if !(value == nil && boolVal(sch.Nullable)) {
			v.add(path, "expected %s, got %s", describeTypes(sch.Type), jsonType(value))
			return
		}
	}

	if enum := enumValues(sch); len(enum) > 0 && !inEnum(value, enum) {
		msg := fmt.Sprintf("must be one of %s, got %s", renderEnum(enum), renderValue(value))
		if s, ok := value.(string); ok {
			if near := suggest(s, stringValues(enum)); near != "" {
				msg += fmt.Sprintf(" (did you mean %q?)", near)
			}
		}
		v.add(path, "%s", msg)
	}
	if sch.Const != nil {
		if c := normalizeJSON(decodeNode(sch.Const)); !jsonEqual(value, c) {
			v.add(path, "must be %s, got %s", renderValue(c), renderValue(value))
		}
	}

	switch val := value.(type) {
	case string:
		v.checkString(val, sch, path)
	case float64:
		v.checkNumber(val, sch, path)
	case []interface{}:
		v.checkArray(val, sch, path)
	case map[string]interface{}:
		v.checkObject(val, sch, path, allowed, strict)
	}

	// allOf: every member applies to the same value, and a key declared by any
	// member is known to all of them. Plain members don't judge unknown keys;
	// checkObject has already done that against the union of their properties.
	// A member that holds a union (allOf: [common fields, oneOf: [email, slack]])
	// is the exception: only its branches know their keys, so the strict check
	// moves down into them, with the composite's own properties counted as known.
	if len(sch.AllOf) > 0 {
		known := map[string]bool{}
		for k := range allowed {
			known[k] = true
		}
		for k := range declaredProperties(sch) {
			known[k] = true
		}
		for _, m := range sch.AllOf {
			v.check(value, m, path, known, strict && hasUnion(m.Schema()))
		}
	}

	if len(sch.AnyOf) > 0 {
		v.checkUnion(value, sch.AnyOf, sch, path, allowed, strict)
	}
	if len(sch.OneOf) > 0 {
		v.checkUnion(value, sch.OneOf, sch, path, allowed, strict)
	}

	if sch.Not != nil && len(sub(value, sch.Not, path, nil, false)) == 0 {
		v.add(path, "%s", notMessage(sch.Not.Schema()))
	}
}

func (v *validator) checkString(s string, sch *base.Schema, path string) {
	// The API counts length in UTF-16 code units (JavaScript's String.length),
	// JSON Schema in code points. Use whichever reading lets the value pass, so
	// an emoji near a limit is never rejected locally.
	runes := int64(utf8.RuneCountInString(s))
	units := int64(len(utf16.Encode([]rune(s))))
	if sch.MinLength != nil && units < *sch.MinLength {
		v.add(path, "must be at least %d characters, got %d", *sch.MinLength, units)
	}
	if sch.MaxLength != nil && runes > *sch.MaxLength {
		v.add(path, "must be %d characters or fewer, got %d", *sch.MaxLength, runes)
	}
	if sch.Pattern != "" {
		if re, err := regexp.Compile(sch.Pattern); err == nil && !re.MatchString(s) {
			v.add(path, "must match pattern %s", sch.Pattern)
		}
	}
	if sch.Format == "uuid" && !uuidPattern.MatchString(s) {
		v.add(path, "must be a UUID, got %s", renderValue(s))
	}
}

func (v *validator) checkNumber(n float64, sch *base.Schema, path string) {
	if sch.Minimum != nil && n < *sch.Minimum {
		v.add(path, "must be at least %s, got %s", fmtNum(*sch.Minimum), fmtNum(n))
	}
	if sch.Maximum != nil && n > *sch.Maximum {
		v.add(path, "must be at most %s, got %s", fmtNum(*sch.Maximum), fmtNum(n))
	}
	if b, ok := exclusiveBound(sch.ExclusiveMinimum, sch.Minimum); ok && n <= b {
		v.add(path, "must be greater than %s, got %s", fmtNum(b), fmtNum(n))
	}
	if b, ok := exclusiveBound(sch.ExclusiveMaximum, sch.Maximum); ok && n >= b {
		v.add(path, "must be less than %s, got %s", fmtNum(b), fmtNum(n))
	}
}

// exclusiveBound reads exclusiveMinimum/Maximum in either dialect: a number
// (OpenAPI 3.1 / JSON Schema 2020-12) or a boolean modifying minimum/maximum
// (OpenAPI 3.0).
func exclusiveBound(dv *base.DynamicValue[bool, float64], inclusive *float64) (float64, bool) {
	if dv == nil {
		return 0, false
	}
	if dv.IsB() {
		return dv.B, true
	}
	if dv.A && inclusive != nil {
		return *inclusive, true
	}
	return 0, false
}

func (v *validator) checkArray(arr []interface{}, sch *base.Schema, path string) {
	n := int64(len(arr))
	if sch.MinItems != nil && n < *sch.MinItems {
		v.add(path, "must have at least %d items, got %d", *sch.MinItems, n)
	}
	if sch.MaxItems != nil && n > *sch.MaxItems {
		v.add(path, "must have at most %d items, got %d", *sch.MaxItems, n)
	}
	if sch.Items != nil && sch.Items.IsA() {
		for i, item := range arr {
			v.check(item, sch.Items.A, fmt.Sprintf("%s[%d]", path, i), nil, true)
		}
	}
}

func (v *validator) checkObject(obj map[string]interface{}, sch *base.Schema, path string, allowed map[string]bool, strict bool) {
	props := declaredProperties(sch)

	required := map[string]bool{}
	for _, name := range sch.Required {
		required[name] = true
	}
	for _, name := range sortedKeys(required) {
		if _, ok := obj[name]; !ok {
			v.add(joinPath(path, name), "missing required field %q", name)
		}
	}

	for _, key := range sortedKeys(obj) {
		val := obj[key]
		if p, ok := props[key]; ok {
			if ps := p.Schema(); ps != nil && boolVal(ps.ReadOnly) {
				v.add(joinPath(path, key), "%q is read-only; the API sets it, so leave it out of the request", key)
				continue
			}
			if ownProperty(sch, key) {
				v.check(val, p, joinPath(path, key), nil, true)
			}
			continue
		}
		if allowed[key] {
			continue
		}
		if ap := sch.AdditionalProperties; ap != nil {
			if ap.IsA() {
				v.check(val, ap.A, joinPath(path, key), nil, true)
				continue
			}
			if ap.B {
				continue
			}
		}
		// A union decides on its own branches which keys exist; and an object
		// that declares no properties at all is a free-form map.
		if !strict || hasUnion(sch) || (len(props) == 0 && sch.AdditionalProperties == nil) {
			continue
		}
		known := make([]string, 0, len(props)+len(allowed))
		for name := range props {
			known = append(known, name)
		}
		for name := range allowed {
			known = append(known, name)
		}
		msg := fmt.Sprintf("unknown field %q", key)
		if s := suggest(key, known); s != "" {
			msg += fmt.Sprintf(" (did you mean %q?)", s)
		}
		v.add(joinPath(path, key), "%s", msg)
	}
}

// checkUnion passes if any branch accepts the value. Otherwise it reports the
// branch the caller most plausibly meant: the one a discriminator value selects,
// else the only branch of the right JSON type, else the type-compatible branch
// with the fewest problems. Keys declared on the object around the union are
// known inside every branch.
func (v *validator) checkUnion(value interface{}, branches []*base.SchemaProxy, parent *base.Schema, path string, allowed map[string]bool, strict bool) {
	inherited := map[string]bool{}
	for k := range allowed {
		inherited[k] = true
	}
	for k := range declaredProperties(parent) {
		inherited[k] = true
	}

	results := make([][]Violation, len(branches))
	for i, b := range branches {
		results[i] = sub(value, b, path, inherited, strict)
		if len(results[i]) == 0 {
			return
		}
	}

	if i := discriminatedBranch(value, branches); i >= 0 {
		v.out = append(v.out, results[i]...)
		return
	}

	best := -1
	for i, b := range branches {
		bs := b.Schema()
		if bs == nil || (len(bs.Type) > 0 && !typeMatches(value, bs.Type)) {
			continue
		}
		if best < 0 || len(results[i]) < len(results[best]) {
			best = i
		}
	}
	if best >= 0 {
		v.out = append(v.out, results[best]...)
		return
	}

	var types []string
	for _, b := range branches {
		if bs := b.Schema(); bs != nil {
			types = append(types, bs.Type...)
		}
	}
	if len(types) > 0 {
		v.add(path, "expected %s, got %s", describeTypes(dedupe(types)), jsonType(value))
		return
	}
	v.add(path, "does not match any of the %d allowed shapes", len(branches))
}

// discriminatedBranch returns the index of the branch whose single-valued
// property (an enum of one value or a const, the shape a discriminator takes)
// equals the value's own, or -1.
func discriminatedBranch(value interface{}, branches []*base.SchemaProxy) int {
	obj, ok := value.(map[string]interface{})
	if !ok {
		return -1
	}
	for i, b := range branches {
		bs := b.Schema()
		if bs == nil {
			continue
		}
		for name, p := range declaredProperties(bs) {
			ps := p.Schema()
			if ps == nil {
				continue
			}
			var pinned interface{}
			if ps.Const != nil {
				pinned = normalizeJSON(decodeNode(ps.Const))
			} else if e := enumValues(ps); len(e) == 1 {
				pinned = e[0]
			} else {
				continue
			}
			if got, ok := obj[name]; ok && jsonEqual(got, pinned) {
				return i
			}
		}
	}
	return -1
}

// declaredProperties returns the properties a schema declares itself or
// through allOf members (recursively), which is the set of known keys for the
// unknown-key check.
func declaredProperties(sch *base.Schema) map[string]*base.SchemaProxy {
	out := map[string]*base.SchemaProxy{}
	var walk func(s *base.Schema, depth int)
	walk = func(s *base.Schema, depth int) {
		if s == nil || depth > 16 {
			return
		}
		for _, m := range s.AllOf {
			walk(m.Schema(), depth+1)
		}
		if s.Properties != nil {
			for pair := s.Properties.First(); pair != nil; pair = pair.Next() {
				out[pair.Key()] = pair.Value()
			}
		}
	}
	walk(sch, 0)
	return out
}

// hasUnion reports whether the schema, or any allOf member under it, is an
// anyOf/oneOf. Such an object's full key set is only known per branch.
func hasUnion(sch *base.Schema) bool {
	var walk func(s *base.Schema, depth int) bool
	walk = func(s *base.Schema, depth int) bool {
		if s == nil || depth > 16 {
			return false
		}
		if len(s.AnyOf) > 0 || len(s.OneOf) > 0 {
			return true
		}
		for _, m := range s.AllOf {
			if walk(m.Schema(), depth+1) {
				return true
			}
		}
		return false
	}
	return walk(sch, 0)
}

func ownProperty(sch *base.Schema, key string) bool {
	if sch.Properties == nil {
		return false
	}
	_, ok := sch.Properties.Get(key)
	return ok
}

func notMessage(not *base.Schema) string {
	if not != nil && len(not.Required) > 1 && not.Properties == nil && len(not.Type) == 0 {
		quoted := make([]string, len(not.Required))
		for i, r := range not.Required {
			quoted[i] = strconv.Quote(r)
		}
		return fmt.Sprintf("%s cannot be used together", strings.Join(quoted, " and "))
	}
	if not != nil && not.Pattern != "" && len(not.Type) == 0 {
		return fmt.Sprintf("must not match pattern %s", not.Pattern)
	}
	return "matches a shape this field does not allow"
}

func typeMatches(value interface{}, types []string) bool {
	got := jsonType(value)
	for _, t := range types {
		switch {
		case t == got:
			return true
		case t == "number" && got == "integer":
			return true
		}
	}
	return false
}

// jsonType names a decoded JSON value's type; whole numbers are "integer".
func jsonType(value interface{}) string {
	switch val := value.(type) {
	case nil:
		return "null"
	case bool:
		return "boolean"
	case float64:
		if val == math.Trunc(val) && !math.IsInf(val, 0) {
			return "integer"
		}
		return "number"
	case string:
		return "string"
	case []interface{}:
		return "array"
	case map[string]interface{}:
		return "object"
	}
	return fmt.Sprintf("%T", value)
}

func describeTypes(types []string) string {
	if len(types) == 1 {
		return types[0]
	}
	return strings.Join(types, " or ")
}

func enumValues(sch *base.Schema) []interface{} {
	vals := decodeNodes(sch.Enum)
	for i := range vals {
		vals[i] = normalizeJSON(vals[i])
	}
	return vals
}

func stringValues(vals []interface{}) []string {
	var out []string
	for _, v := range vals {
		if s, ok := v.(string); ok {
			out = append(out, s)
		}
	}
	return out
}

func inEnum(value interface{}, enum []interface{}) bool {
	for _, e := range enum {
		if jsonEqual(value, e) {
			return true
		}
	}
	return false
}

func renderEnum(enum []interface{}) string {
	parts := make([]string, len(enum))
	for i, e := range enum {
		parts[i] = renderValue(e)
	}
	return strings.Join(parts, ", ")
}

// renderValue shows a value as JSON, unescaped so "<id>" reads as written,
// and shortened so a large object can't swamp the message.
func renderValue(v interface{}) string {
	var buf bytes.Buffer
	enc := json.NewEncoder(&buf)
	enc.SetEscapeHTML(false)
	if err := enc.Encode(v); err != nil {
		return fmt.Sprintf("%v", v)
	}
	s := strings.TrimSuffix(buf.String(), "\n")
	if r := []rune(s); len(r) > 60 {
		s = string(r[:57]) + "..."
	}
	return s
}

func fmtNum(f float64) string {
	return strconv.FormatFloat(f, 'f', -1, 64)
}

// normalizeJSON converts decoded JSON (json.Number) and YAML (int, uint64)
// numbers to float64 so values from the body and from the spec compare equal.
func normalizeJSON(v interface{}) interface{} {
	switch val := v.(type) {
	case json.Number:
		// Out-of-range numbers come back as ±Inf with an error; keep the
		// infinity so the value is still judged as a number.
		f, err := val.Float64()
		if err != nil && !math.IsInf(f, 0) {
			return val.String()
		}
		return f
	case int:
		return float64(val)
	case int64:
		return float64(val)
	case uint64:
		return float64(val)
	case float32:
		return float64(val)
	case []interface{}:
		for i := range val {
			val[i] = normalizeJSON(val[i])
		}
		return val
	case map[string]interface{}:
		for k := range val {
			val[k] = normalizeJSON(val[k])
		}
		return val
	}
	return v
}

func jsonEqual(a, b interface{}) bool {
	ab, err1 := json.Marshal(a)
	bb, err2 := json.Marshal(b)
	return err1 == nil && err2 == nil && bytes.Equal(ab, bb)
}

var plainKey = regexp.MustCompile(`^[A-Za-z_$][A-Za-z0-9_$-]*$`)

// joinPath appends an object key to a violation path: body.query.limit, or
// body.query.filters["orders.status"] when the key itself contains dots.
func joinPath(path, key string) string {
	if plainKey.MatchString(key) {
		return path + "." + key
	}
	return path + "[" + strconv.Quote(key) + "]"
}

func dedupe(in []string) []string {
	seen := map[string]bool{}
	var out []string
	for _, s := range in {
		if !seen[s] {
			seen[s] = true
			out = append(out, s)
		}
	}
	return out
}

// suggest returns the known name closest to key: a case- or separator-only
// difference first, then the smallest edit distance. Empty if nothing is close.
func suggest(key string, known []string) string {
	fold := func(s string) string {
		return strings.NewReplacer("_", "", "-", "").Replace(strings.ToLower(s))
	}
	sort.Strings(known)
	for _, k := range known {
		if fold(k) == fold(key) {
			return k
		}
	}
	// Within a third of the key's length (at least 2), but never so far that
	// the whole key is rewritten: "b" is not a typo of "a".
	limit := min(max(len(key)/3, 2), len([]rune(key))-1)
	best, bestD := "", limit+1
	for _, k := range known {
		if d := editDistance(strings.ToLower(key), strings.ToLower(k)); d < bestD {
			best, bestD = k, d
		}
	}
	return best
}

func editDistance(a, b string) int {
	ar, br := []rune(a), []rune(b)
	prev := make([]int, len(br)+1)
	cur := make([]int, len(br)+1)
	for j := range prev {
		prev[j] = j
	}
	for i := 1; i <= len(ar); i++ {
		cur[0] = i
		for j := 1; j <= len(br); j++ {
			cost := 1
			if ar[i-1] == br[j-1] {
				cost = 0
			}
			cur[j] = min(prev[j]+1, cur[j-1]+1, prev[j-1]+cost)
		}
		prev, cur = cur, prev
	}
	return prev[len(br)]
}

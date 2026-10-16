package output

import (
	"fmt"
	"math"
	"regexp"
	"strconv"
	"strings"
	"time"

	"github.com/exploreomni/omni-cli/internal/result"
)

// numFormat is a decoded model format: a documented name or an Excel-style pattern.
type numFormat struct {
	kind     string // number, percent, id, big, billions, millions, thousands, currency, accounting, financial, pattern
	decimals int    // -1 when the format leaves it open
	// pattern only: decimals past this many are dropped when they're zeros (#)
	minDecimals int
	minInt      int    // pattern only: integer digits always shown (00000)
	symbol      string // currency family only
	compact     bool   // bigcurrency, bigaccounting, bigfinancial

	// pattern only
	prefix, suffix string
	group, scale   bool
	divide         float64 // trailing commas: ÷1,000 each
	exponent       bool    // 0.00E+00
	negative, zero *numFormat
}

// namedFormat: a numeric kind, or a currency category with optional big and
// currency-code prefixes (bigusdcurrency_2), plus an optional _<decimals>.
var namedFormat = regexp.MustCompile(`(?i)^(?:(number|percent|id|big|billions|millions|thousands)|(big)?(usd|eur|gbp|aud|jpy|brl)?(currency|accounting|financial))(?:_(\d))?$`)

var currencySymbols = map[string]string{"usd": "$", "eur": "€", "gbp": "£", "aud": "A$", "jpy": "¥", "brl": "R$"}

func parseFormat(f string) (numFormat, bool) {
	f = strings.TrimSpace(f)
	m := namedFormat.FindStringSubmatch(f)
	if m == nil {
		return parsePattern(f)
	}
	nf := numFormat{decimals: -1, symbol: "$"}
	if m[1] != "" {
		nf.kind = strings.ToLower(m[1])
	} else {
		nf.kind = strings.ToLower(m[4])
		nf.compact = m[2] != ""
		if code := strings.ToLower(m[3]); code != "" {
			nf.symbol = currencySymbols[code]
		}
	}
	if m[5] != "" {
		nf.decimals = int(m[5][0] - '0')
	}
	return nf, true
}

// parsePattern reads an Excel-style pattern: quoted/escaped literals, a bare
// % scales by 100, trailing commas divide by 1000, E+00, and pos;neg;zero
// sections. {{field}} references need data the result lacks and are declined.
func parsePattern(p string) (numFormat, bool) {
	if p == "" || strings.Contains(p, "{{") {
		return numFormat{}, false
	}
	sections := splitSections(p)
	nf, ok := parseSection(sections[0])
	if !ok {
		return numFormat{}, false
	}
	if len(sections) > 1 {
		if neg, ok := parseSection(sections[1]); ok {
			nf.negative = &neg
		}
	}
	if len(sections) > 2 {
		if zero, ok := parseSection(sections[2]); ok {
			nf.zero = &zero
		}
	}
	return nf, true
}

func splitSections(p string) []string {
	var out []string
	var cur strings.Builder
	quoted := false
	for i := 0; i < len(p); i++ {
		switch {
		case p[i] == '"':
			quoted = !quoted
		case p[i] == ';' && !quoted:
			out = append(out, cur.String())
			cur.Reset()
			continue
		}
		cur.WriteByte(p[i])
	}
	return append(out, cur.String())
}

func parseSection(p string) (numFormat, bool) {
	nf := numFormat{kind: "pattern", divide: 1}
	var core strings.Builder
	inCore, afterCore := false, false
	for i := 0; i < len(p); {
		c := p[i]
		switch {
		case c == '"':
			end := strings.IndexByte(p[i+1:], '"')
			if end < 0 {
				return numFormat{}, false
			}
			nf.addLiteral(p[i+1:i+1+end], inCore)
			i += end + 2
		case c == '\\' && i+1 < len(p):
			nf.addLiteral(p[i+1:i+2], inCore)
			i += 2
		case c == '#' || c == '0' || c == '.':
			if afterCore {
				return numFormat{}, false
			}
			inCore = true
			core.WriteByte(c)
			i++
		case c == ',':
			if !inCore || afterCore {
				return numFormat{}, false
			}
			if j := i + 1; j < len(p) && (p[j] == '#' || p[j] == '0') {
				nf.group = true
			} else {
				nf.divide *= 1000
			}
			i++
		case c == 'E' && strings.HasPrefix(p[i:], "E+00"):
			nf.exponent = true
			afterCore = true
			i += 4
		case c == '%':
			nf.scale = true
			nf.addLiteral("%", inCore)
			i++
		case c == '$' || c == ' ' || c == '-' || c == '+' || c == '(' || c == ')':
			nf.addLiteral(string(c), inCore)
			i++
		case currencyPrefix(p[i:]) != "":
			sym := currencyPrefix(p[i:])
			nf.addLiteral(sym, inCore)
			i += len(sym)
		default:
			return numFormat{}, false
		}
		if inCore && !(c == '#' || c == '0' || c == '.' || c == ',') {
			afterCore = true
		}
	}
	digits := core.String()
	if digits == "" {
		return numFormat{}, false
	}
	intDigits := digits
	if dot := strings.IndexByte(digits, '.'); dot >= 0 {
		// 0 is a digit always shown, # one shown only when it isn't a trailing zero.
		nf.minDecimals = strings.Count(digits[dot+1:], "0")
		nf.decimals = nf.minDecimals + strings.Count(digits[dot+1:], "#")
		intDigits = digits[:dot]
	}
	nf.minInt = strings.Count(intDigits, "0")
	return nf, true
}

func currencyPrefix(s string) string {
	for _, sym := range []string{"€", "£", "¥"} {
		if strings.HasPrefix(s, sym) {
			return sym
		}
	}
	return ""
}

func (nf *numFormat) addLiteral(s string, afterCore bool) {
	if afterCore {
		nf.suffix += s
	} else {
		nf.prefix += s
	}
}

// FormatValue renders one cell in its column's model format.
func FormatValue(v any, col result.Column) string {
	return singleLine(formatValue(v, col))
}

func formatValue(v any, col result.Column) string {
	switch x := v.(type) {
	case nil:
		return "-"
	case time.Time:
		if strings.Contains(col.Format, "%") {
			return strftime(x, col.Format)
		}
		return formatTime(x, col.DataType)
	case bool:
		if x {
			return "true"
		}
		return "false"
	case string:
		x = singleLine(x)
		if x == "" {
			return "-"
		}
		// Temporal dimensions arrive as ISO text; a strftime format is ours to apply.
		if strings.Contains(col.Format, "%") && isTemporal(col.DataType) {
			if t, ok := parseISO(x); ok {
				return strftime(t, col.Format)
			}
		}
		return x
	}
	nf, formatted := parseFormat(col.Format)
	if ex, ok := asExact(v); ok {
		if !formatted {
			return ex.plain()
		}
		if s, ok := nf.renderExact(ex); ok {
			return s
		}
	}
	f, ok := asFloat(v)
	if !ok {
		return singleLine(fmt.Sprint(v))
	}
	if !formatted {
		return formatNumber(f)
	}
	return nf.render(f)
}

// exact is an integer or decimal kept as digits, never rounded through float64.
type exact struct {
	neg    bool
	digits string // the unsigned coefficient
	scale  int
}

func asExact(v any) (exact, bool) {
	var s string
	scale := 0
	switch x := v.(type) {
	case int64:
		s = strconv.FormatInt(x, 10)
	case int:
		s = strconv.Itoa(x)
	case result.Decimal:
		s, scale = x.Coef.String(), int(x.Scale)
	default:
		return exact{}, false
	}
	if strings.HasPrefix(s, "-") {
		return exact{neg: true, digits: s[1:], scale: scale}, true
	}
	return exact{digits: s, scale: scale}, true
}

func (e exact) isZero() bool {
	return strings.Trim(e.digits, "0") == ""
}

// fixed rounds half away from zero; neg is false when that rounds to zero.
func (e exact) fixed(decimals, minInt int, group bool) (body string, neg bool) {
	digits := e.digits
	if decimals < e.scale {
		cut := len(digits) - (e.scale - decimals)
		roundUp := cut >= 0 && digits[cut] >= '5'
		digits = digits[:max(cut, 0)]
		if roundUp {
			digits = incrementDigits(digits)
		}
	} else {
		digits += strings.Repeat("0", decimals-e.scale)
	}
	if len(digits) <= decimals {
		digits = strings.Repeat("0", decimals-len(digits)+1) + digits
	}
	intPart, frac := digits[:len(digits)-decimals], digits[len(digits)-decimals:]
	intPart = strings.TrimLeft(intPart, "0")
	if len(intPart) < max(minInt, 1) {
		intPart = strings.Repeat("0", max(minInt, 1)-len(intPart)) + intPart
	}
	if group {
		intPart = groupDigits(intPart)
	}
	if frac != "" {
		intPart += "." + frac
	}
	return intPart, e.neg && strings.Trim(digits, "0") != ""
}

func incrementDigits(d string) string {
	b := []byte(d)
	for i := len(b) - 1; i >= 0; i-- {
		if b[i] < '9' {
			b[i]++
			return string(b)
		}
		b[i] = '0'
	}
	return "1" + string(b)
}

// plain matches formatNumber: trailing zeros dropped, grouped from five digits.
func (e exact) plain() string {
	body, neg := e.fixed(e.scale, 1, false)
	intPart, frac := body, ""
	if i := strings.IndexByte(body, '.'); i >= 0 {
		intPart, frac = body[:i], strings.TrimRight(body[i:], "0")
		if frac == "." {
			frac = ""
		}
	}
	if len(intPart) >= 5 {
		intPart = groupDigits(intPart)
	}
	return signed(intPart+frac, neg)
}

func signed(s string, neg bool) string {
	if neg {
		return "-" + s
	}
	return s
}

func (nf numFormat) renderExact(e exact) (string, bool) {
	switch nf.kind {
	case "id":
		return signed(e.fixed(0, 1, false)), true
	case "number":
		return signed(e.fixed(defaultDecimals(nf, 2), 1, true)), true
	case "currency", "accounting", "financial":
		if nf.compact {
			return "", false
		}
		body, neg := e.fixed(defaultDecimals(nf, 2), 1, true)
		if nf.kind == "financial" {
			if neg {
				body = "(" + body + ")"
			}
			return body, true
		}
		if neg {
			if nf.kind == "currency" {
				return "-" + nf.symbol + body, true
			}
			body = "(" + body + ")"
		}
		return nf.symbol + body, true
	case "pattern":
		sign := false
		if e.neg && !e.isZero() {
			if nf.negative != nil {
				nf = *nf.negative
			} else {
				sign = true
			}
		} else if e.isZero() && nf.zero != nil {
			nf = *nf.zero
		}
		if nf.scale || nf.divide != 1 || nf.exponent {
			return "", false
		}
		body, neg := e.fixed(nf.decimals, nf.minInt, nf.group)
		if nf.minDecimals < nf.decimals {
			body = trimDecimals(body, nf.minDecimals)
		}
		return signed(nf.prefix+body+nf.suffix, sign && neg), true
	}
	return "", false
}

func (nf numFormat) render(f float64) string {
	if math.IsNaN(f) || math.IsInf(f, 0) {
		return fmt.Sprintf("%g", f)
	}
	abs := math.Abs(f)
	switch nf.kind {
	case "percent":
		return fixed(f*100, defaultDecimals(nf, 1), true) + "%"
	case "id":
		return fixed(f, 0, false)
	case "big":
		return bigNumber(f, defaultDecimals(nf, 2))
	case "billions":
		return fixed(f/1e9, defaultDecimals(nf, 2), true) + "B"
	case "millions":
		return fixed(f/1e6, defaultDecimals(nf, 1), true) + "M"
	case "thousands":
		return fixed(f/1e3, defaultDecimals(nf, 2), true) + "K"
	case "currency", "accounting", "financial":
		d := defaultDecimals(nf, 2)
		var body string
		if nf.compact {
			body = bigNumber(abs, d)
		} else {
			body = fixed(abs, d, true)
		}
		switch nf.kind {
		case "currency":
			if f < 0 {
				return "-" + nf.symbol + body
			}
			return nf.symbol + body
		case "accounting":
			if f < 0 {
				return nf.symbol + "(" + body + ")"
			}
			return nf.symbol + body
		default: // financial: parentheses, no mark
			if f < 0 {
				return "(" + body + ")"
			}
			return body
		}
	case "pattern":
		if f < 0 && nf.negative != nil {
			return nf.negative.renderSection(abs)
		}
		if f == 0 && nf.zero != nil {
			return nf.zero.renderSection(0)
		}
		s := nf.renderSection(abs)
		if f < 0 {
			return "-" + s
		}
		return s
	}
	// number
	if nf.decimals < 0 {
		return fixed(f, 2, true)
	}
	return fixed(f, nf.decimals, true)
}

func (nf numFormat) renderSection(abs float64) string {
	if nf.scale {
		abs *= 100
	}
	abs /= nf.divide
	var s string
	if nf.exponent {
		s = strings.ToUpper(fmt.Sprintf("%.*e", nf.decimals, abs))
	} else {
		s = padInteger(fixed(abs, nf.decimals, false), nf.minInt)
		if nf.group {
			intPart, frac, _ := strings.Cut(s, ".")
			s = groupDigits(intPart)
			if frac != "" {
				s += "." + frac
			}
		}
		if nf.minDecimals < nf.decimals {
			s = trimDecimals(s, nf.minDecimals)
		}
	}
	return nf.prefix + s + nf.suffix
}

func defaultDecimals(nf numFormat, d int) int {
	if nf.decimals < 0 {
		return d
	}
	return nf.decimals
}

// fixed renders f with exactly d decimals, optionally grouping thousands.
func fixed(f float64, d int, group bool) string {
	s := fmt.Sprintf("%.*f", d, f)
	sign := ""
	if strings.HasPrefix(s, "-") {
		sign, s = "-", s[1:]
	}
	intPart, frac := s, ""
	if i := strings.IndexByte(s, '.'); i >= 0 {
		intPart, frac = s[:i], s[i:]
	}
	if group {
		intPart = groupDigits(intPart)
	}
	return sign + intPart + frac
}

func padInteger(s string, minInt int) string {
	intLen := strings.IndexByte(s, '.')
	if intLen < 0 {
		intLen = len(s)
	}
	if intLen >= minInt {
		return s
	}
	return strings.Repeat("0", minInt-intLen) + s
}

func groupDigits(digits string) string {
	if len(digits) <= 3 {
		return digits
	}
	var b strings.Builder
	for i := range digits {
		if i > 0 && (len(digits)-i)%3 == 0 {
			b.WriteByte(',')
		}
		b.WriteByte(digits[i])
	}
	return b.String()
}

// trimDecimals drops trailing zero decimals from a fixed rendering, keeping keep of them.
func trimDecimals(s string, keep int) string {
	dot := strings.IndexByte(s, '.')
	if dot < 0 {
		return s
	}
	end := len(s)
	for end > dot+1+keep && s[end-1] == '0' {
		end--
	}
	if end == dot+1 {
		end = dot
	}
	return s[:end]
}

// bigNumber picks the unit after rounding, so 999,999.9 reads 1.00M, not 1000.00K.
func bigNumber(f float64, d int) string {
	abs := math.Abs(f)
	pow := math.Pow(10, float64(d))
	for _, u := range []struct {
		scale  float64
		suffix string
	}{{1e12, "T"}, {1e9, "B"}, {1e6, "M"}, {1e3, "K"}} {
		if math.Round(abs/u.scale*pow)/pow >= 1 {
			return fixed(f/u.scale, d, false) + u.suffix
		}
	}
	return fixed(f, d, false)
}

func isTemporal(dataType string) bool {
	u := strings.ToUpper(dataType)
	return strings.Contains(u, "DATE") || strings.Contains(u, "TIME")
}

func parseISO(s string) (time.Time, bool) {
	for _, layout := range []string{"2006-01-02 15:04:05.000", "2006-01-02 15:04:05", "2006-01-02T15:04:05Z07:00", "2006-01-02T15:04:05", "2006-01-02"} {
		if t, err := time.Parse(layout, s); err == nil {
			return t, true
		}
	}
	return time.Time{}, false
}

// strftime handles the common directives; unknown ones are left in place.
func strftime(t time.Time, pattern string) string {
	var b strings.Builder
	for i := 0; i < len(pattern); i++ {
		if pattern[i] != '%' || i+1 >= len(pattern) {
			b.WriteByte(pattern[i])
			continue
		}
		i++
		switch pattern[i] {
		case 'Y':
			b.WriteString(t.Format("2006"))
		case 'y':
			b.WriteString(t.Format("06"))
		case 'm':
			b.WriteString(t.Format("01"))
		case 'd':
			b.WriteString(t.Format("02"))
		case 'e':
			b.WriteString(t.Format("_2"))
		case 'b':
			b.WriteString(t.Format("Jan"))
		case 'B':
			b.WriteString(t.Format("January"))
		case 'a':
			b.WriteString(t.Format("Mon"))
		case 'A':
			b.WriteString(t.Format("Monday"))
		case 'H':
			b.WriteString(t.Format("15"))
		case 'I':
			b.WriteString(t.Format("03"))
		case 'M':
			b.WriteString(t.Format("04"))
		case 'S':
			b.WriteString(t.Format("05"))
		case 'p':
			b.WriteString(t.Format("PM"))
		case 'j':
			fmt.Fprintf(&b, "%03d", t.YearDay())
		case '%':
			b.WriteByte('%')
		default:
			b.WriteByte('%')
			b.WriteByte(pattern[i])
		}
	}
	return b.String()
}

func formatTime(t time.Time, dataType string) string {
	dateOnly := strings.EqualFold(dataType, "DATE") ||
		(t.Hour() == 0 && t.Minute() == 0 && t.Second() == 0 && t.Nanosecond() == 0)
	if dateOnly {
		return t.Format("Jan 2, 2006")
	}
	return t.Format("Jan 2, 2006 15:04")
}

func asFloat(v any) (float64, bool) {
	switch x := v.(type) {
	case int64:
		return float64(x), true
	case float64:
		return x, true
	case int:
		return float64(x), true
	case result.Decimal:
		return x.Float64(), true
	}
	return 0, false
}

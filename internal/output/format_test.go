package output

import (
	"math"
	"math/big"
	"testing"
	"time"

	"github.com/exploreomni/omni-cli/internal/result"
)

func TestFormatValue_ExactIntegers(t *testing.T) {
	for _, tc := range []struct {
		value        int64
		format, want string
	}{
		{9007199254740993, "id", "9007199254740993"},
		{math.MaxInt64, "id", "9223372036854775807"},
		{math.MinInt64, "id", "-9223372036854775808"},
		{9007199254740993, "", "9,007,199,254,740,993"},
		{9007199254740993, "NUMBER_0", "9,007,199,254,740,993"},
		{9007199254740993, "number", "9,007,199,254,740,993.00"},
		{9007199254740993, "currency_2", "$9,007,199,254,740,993.00"},
		{math.MinInt64, "accounting_0", "$(9,223,372,036,854,775,808)"},
		{-9007199254740993, "financial_0", "(9,007,199,254,740,993)"},
		{9007199254740993, "#,##0.0#", "9,007,199,254,740,993.0"},
		{math.MinInt64, "#,##0;(#,##0)", "(9,223,372,036,854,775,808)"},
		{-9007199254740993, "0", "-9007199254740993"},
		{12345, "big_1", "12.3K"},
		{12, "percent_0", "1,200%"},
		{1234, "0.0,", "1.2"},
		{-1234, "0.0,;(0.0,)", "(1.2)"},
		{1234, "0.00E+00", "1.23E+03"},
	} {
		if got := FormatValue(tc.value, result.Column{Format: tc.format}); got != tc.want {
			t.Errorf("FormatValue(%d, %q) = %q, want %q", tc.value, tc.format, got, tc.want)
		}
	}
}

func dec(t *testing.T, s string, scale int32) result.Decimal {
	t.Helper()
	coef, ok := new(big.Int).SetString(s, 10)
	if !ok {
		t.Fatalf("bad coefficient %q", s)
	}
	return result.Decimal{Coef: coef, Scale: scale}
}

func TestFormatValue_ExactDecimals(t *testing.T) {
	for _, tc := range []struct {
		value        result.Decimal
		format, want string
	}{
		{dec(t, "12345678901234567891", 2), "", "123,456,789,012,345,678.91"},
		{dec(t, "1250", 2), "", "12.5"},
		{dec(t, "-100", 2), "", "-1"},
		{dec(t, "5", 3), "", "0.005"},
		{dec(t, "18446744073709551615", 0), "id", "18446744073709551615"},
		{dec(t, "18446744073709551615", 0), "", "18,446,744,073,709,551,615"},
		{dec(t, "12345678901234567891", 2), "number_2", "123,456,789,012,345,678.91"},
		{dec(t, "12345678901234567895", 3), "number_2", "12,345,678,901,234,567.90"},
		{dec(t, "999995", 4), "number_1", "100.0"},
		{dec(t, "5", 1), "id", "1"},
		{dec(t, "-4", 3), "number_2", "0.00"},
		{dec(t, "-12345678901234567891", 2), "currency_2", "-$123,456,789,012,345,678.91"},
		{dec(t, "-12345678901234567891", 2), "financial_0", "(123,456,789,012,345,679)"},
		{dec(t, "12345678901234567891", 2), "#,##0.0#", "123,456,789,012,345,678.91"},
		{dec(t, "1250", 2), "0.0#", "12.5"},
		{dec(t, "1250", 2), "percent_0", "1,250%"},
	} {
		if got := FormatValue(tc.value, result.Column{Format: tc.format}); got != tc.want {
			t.Errorf("FormatValue(%s, %q) = %q, want %q", tc.value, tc.format, got, tc.want)
		}
	}
}

func TestFormatValue_ZeroPaddedPatterns(t *testing.T) {
	for _, tc := range []struct {
		value        any
		format, want string
	}{
		{int64(12), "00000", "00012"},
		{int64(-12), "00000", "-00012"},
		{int64(123456), "00000", "123456"},
		{12.6, "00000", "00013"},
		{3.14159, "000.00", "003.14"},
		{int64(1234), "#,##0000", "1,234"},
		{int64(12), "#,000,000", "000,012"},
		{dec(t, "125", 1), "0000.0", "0012.5"},
		{0.5, "#.##", "0.5"},
	} {
		if got := FormatValue(tc.value, result.Column{Format: tc.format}); got != tc.want {
			t.Errorf("FormatValue(%v, %q) = %q, want %q", tc.value, tc.format, got, tc.want)
		}
	}
}

func TestFormatValue_SanitizesModelFormats(t *testing.T) {
	for _, tc := range []struct {
		value        any
		format, want string
	}{
		{int64(1), "0\"\x1b]0;changed\a\n\t\"", "1]0;changed  "},
		{1.5, "0.0\"\x1b]0;changed\a\"", "1.5]0;changed"},
		{time.Date(2026, 9, 14, 0, 0, 0, 0, time.UTC), "%Y\x1b[2J\n", "2026[2J "},
		{"2026-09-14", "%Y\x1b[2J\n", "2026[2J "},
	} {
		if got := FormatValue(tc.value, result.Column{Format: tc.format, DataType: "DATE"}); got != tc.want {
			t.Errorf("FormatValue(%v, %q) = %q, want %q", tc.value, tc.format, got, tc.want)
		}
	}
}

func TestFormatValue_ModelFormats(t *testing.T) {
	tests := []struct {
		format string
		in     any
		want   string
	}{
		// Numeric names, with the documented default decimals.
		{"number", 1234.5, "1,234.50"},
		{"NUMBER_0", int64(12526), "12,526"},
		{"NUMBER_0", 838.7, "839"},
		{"NUMBER_2", 1602513.8052, "1,602,513.81"},
		{"number_1", 0.26, "0.3"},
		{"percent", 0.244, "24.4%"},
		{"PERCENT_0", 0.449, "45%"},
		{"percent_2", 0.3992495609133003, "39.92%"},
		{"PERCENT_1", -0.0512, "-5.1%"},
		{"id", int64(123450), "123450"},
		{"ID", 9007199254740992.0, "9007199254740992"},
		{"big", 5.6e6, "5.60M"},
		{"big_1", 1284220.5, "1.3M"},
		{"big_2", 9120.0, "9.12K"},
		{"big_0", 3.4e9, "3B"},
		{"big_1", 42.0, "42.0"},
		{"billions", 1.2e9, "1.20B"},
		{"millions", 5.6e6, "5.6M"},
		{"thousands", 8900.0, "8.90K"},
		// Currency families: category, optional currency prefix, decimals.
		{"currency", 1234.5, "$1,234.50"},
		{"currency_0", -987654.0, "-$987,654"},
		{"usdcurrency_2", int64(5), "$5.00"},
		{"USDCURRENCY_0", 150000.0, "$150,000"},
		{"gbpcurrency_2", -1234.5, "-£1,234.50"},
		{"eurcurrency_2", 1234.5, "€1,234.50"},
		{"jpycurrency_0", 1234.0, "¥1,234"},
		{"brlcurrency_2", 10.0, "R$10.00"},
		{"accounting", -1234.5, "$(1,234.50)"},
		{"ACCOUNTING_0", 150000.0, "$150,000"},
		{"usdaccounting_2", -1234.5, "$(1,234.50)"},
		{"jpyaccounting_0", -1234.0, "¥(1,234)"},
		{"audfinancial_2", -1234.5, "(1,234.50)"},
		{"financial_0", 1234.6, "1,235"},
		{"bigusdcurrency_2", 5.6e6, "$5.60M"},
		{"bigeurcurrency_2", -5.6e6, "-€5.60M"},
		{"bigaccounting_1", -2.5e3, "$(2.5K)"},
		{"bigfinancial_0", 3.4e9, "3B"},
		// Excel-style patterns, from the docs and from production models.
		{"#,##0", 1234.0, "1,234"},
		{"#,##0.00", 1234.56, "1,234.56"},
		{"0%", 0.75, "75%"},
		{"0.00%", 0.75, "75.00%"},
		{"0.0%", 0.3992, "39.9%"},
		{`0.00"%"`, 63.4567, "63.46%"},
		{`0.00"%"`, -2.0, "-2.00%"},
		{"0.00E+00", 1234.0, "1.23E+03"},
		{`#,##0 "units"`, 1234.0, "1,234 units"},
		{`#,##0.00 "kg"`, 1234.5, "1,234.50 kg"},
		{"#,##0.0,", 1234.0, "1.2"},
		{`#,##0.0,,"M"`, 1234567.0, "1.2M"},
		{"$#,##0.00", 1234.5, "$1,234.50"},
		{"$#,##0.00", -1234.5, "-$1,234.50"},
		{`\$0.0`, 2.26, "$2.3"},
		// £ and ¥ are two bytes, € three: each is consumed whole, wherever it sits.
		{"£0.00", 1234.5, "£1234.50"},
		{"£#,##0", 1234.0, "£1,234"},
		{"¥#,##0", 1234.0, "¥1,234"},
		{"0.00£", 1234.5, "1234.50£"},
		{"#,##0€", 1234.0, "1,234€"},
		// # decimals show only when they aren't trailing zeros.
		{"#,##0.##", 1234.567, "1,234.57"},
		{"#,##0.##", 1234.5, "1,234.5"},
		{"#,##0.##", 1234.0, "1,234"},
		{"0.0#", 2.0, "2.0"},
		// The unit is picked after rounding.
		{"big", 999999.9, "1.00M"},
		{"big_0", 999.6, "1K"},
		// Parenthesised negatives: the standard accounting shape.
		{"#,##0;(#,##0)", -1234.0, "(1,234)"},
		{"#,##0;(#,##0)", 1234.0, "1,234"},
		{"$#,##0.00;($#,##0.00)", -1234.5, "($1,234.50)"},
		{`"🚀 "0.0;"📉 "-0.0;0`, 1.5, "🚀 1.5"},
		{`"🚀 "0.0;"📉 "-0.0;0`, -1.5, "📉 -1.5"},
		{`"🚀 "0.0;"📉 "-0.0;0`, 0.0, "0"},
		// Beyond a client's reach: field references need other columns.
		{`#,##0.00 "{{orders.currency_symbol.value}}"`, 1234.5, "1234.5"},
		// Unknown names and patterns fall back to the readable default.
		{"[h]:mm:ss", 3661.0, "3661"},
		{"DURATION", 3661.0, "3661"},
		{"", 1602513.8052352013, "1,602,513.8052352013"},
		{"", int64(2026), "2026"},
	}
	for _, tc := range tests {
		got := FormatValue(tc.in, result.Column{Format: tc.format})
		if got != tc.want {
			t.Errorf("FormatValue(%v, %q) = %q, want %q", tc.in, tc.format, got, tc.want)
		}
	}
}

func TestFormatValue_NonNumbers(t *testing.T) {
	d := time.Date(2026, 8, 10, 0, 0, 0, 0, time.UTC)
	ts := time.Date(2026, 8, 10, 14, 30, 0, 0, time.UTC)
	tests := []struct {
		in   any
		col  result.Column
		want string
	}{
		{nil, result.Column{}, "-"},
		{"", result.Column{}, "-"},
		{"Ireland", result.Column{}, "Ireland"},
		{true, result.Column{}, "true"},
		{d, result.Column{DataType: "DATE"}, "Aug 10, 2026"},
		{d, result.Column{DataType: "TIMESTAMP"}, "Aug 10, 2026"},
		{ts, result.Column{DataType: "TIMESTAMP"}, "Aug 10, 2026 14:30"},
		{ts, result.Column{DataType: "DATETIME"}, "Aug 10, 2026 14:30"},
		// A model date format applies to decoded timestamps too.
		{ts, result.Column{DataType: "TIMESTAMP", Format: "%Y-%m"}, "2026-08"},
		{d, result.Column{DataType: "DATE", Format: "%d/%m/%Y"}, "10/08/2026"},
		// Temporal dimensions arrive formatted by the model, in its reporting
		// timezone, and are shown as sent.
		{"2026-08-05 11:31:00.000", result.Column{DataType: "TIMESTAMP"}, "2026-08-05 11:31:00.000"},
		{"2026-08-10", result.Column{DataType: "DATE"}, "2026-08-10"},
		// A strftime pattern on a temporal dimension is applied to the ISO
		// text the API sends, the way the model asks.
		{"2026-09-12", result.Column{DataType: "TIMESTAMP", Format: "%d-%m-%Y"}, "12-09-2026"},
		{"2026-09-12 14:05:09.000", result.Column{DataType: "TIMESTAMP", Format: "%b %d, %Y %H:%M"}, "Sep 12, 2026 14:05"},
		{"not a date", result.Column{DataType: "TIMESTAMP", Format: "%d-%m-%Y"}, "not a date"},
	}
	for _, tc := range tests {
		if got := FormatValue(tc.in, tc.col); got != tc.want {
			t.Errorf("FormatValue(%v) = %q, want %q", tc.in, got, tc.want)
		}
	}
}

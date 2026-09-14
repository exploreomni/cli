package output

import (
	"testing"
	"time"

	"github.com/exploreomni/omni-cli/internal/result"
)

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

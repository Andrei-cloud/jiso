// root_inspector_state.go derives the FieldRow tree and its display
// strings: template provenance (auto keywords / {{data.*}}
// placeholders), the deterministic auto-preview pool (8 counter
// variants per STAN/RRN/TS field, generated once at state-build time so
// `r` cycles locally), PAN-family masking, and composite subfield
// walking.
//
// Masking contract (E5-A5): every PARSED/DISPLAY surface is masked
// root-side before it crosses to a page — the §C fields tree, its
// auto-preview pool, composite subfields, the §D pane rows (display AND
// hex), and the §C raw-json tab's parsed view. The PACKED WIRE DUMP is
// raw BY DESIGN (InspectorState.PackedHex / the view's packed_hex): it
// is the on-the-wire bytes, identical to `jiso inspect --json`, and
// masking it would defeat the tool's purpose.
package tui

import (
	"encoding/json"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/moov-io/iso8583/field"

	"jiso/internal/tui/pages"
)

// autoPoolSize is how many preview variants root generates per auto
// counter field (the `r` re-roll pool).
const autoPoolSize = 8

// autoKeywords mirror transactions.isReservedAutoKeywordString (that
// helper is unexported; this is the same set — a new keyword must be
// added here too so the page marks the row auto).
var autoKeywords = map[string]bool{
	"auto": true, "$auto": true,
	"stan": true, "$stan": true, "gen_stan": true,
	"rrn": true, "$rrn": true, "gen_rrn": true,
	"auth_code": true, "$auth_code": true, "gen_auth_code": true,
	"datetime": true, "$datetime": true, "date": true, "time": true,
	"random": true, "$random": true,
}

// templateValue renders a declared field value for RawPreview.
func templateValue(v any) string {
	switch v := v.(type) {
	case string:
		return v
	case nil:
		return ""
	default:
		b, err := json.Marshal(v)
		if err != nil {
			return ""
		}

		return string(b)
	}
}

// isAutoTemplate reports whether a template value is runtime-generated:
// an auto keyword or a {{...}} dataset placeholder.
func isAutoTemplate(raw string) bool {
	if raw == "" {
		return false
	}
	if autoKeywords[strings.ToLower(raw)] {
		return true
	}

	return strings.Contains(raw, "{{") && strings.Contains(raw, "}}")
}

// fieldRow renders one composed field (and its subfield tree).
func fieldRow(n int, f field.Field, raw string, depth int) pages.FieldRow {
	row := pages.FieldRow{Num: strconv.Itoa(n)}
	if spec := f.Spec(); spec != nil {
		row.Name = spec.Description
	}
	value, verr := f.String()
	if verr != nil {
		row.Error = verr.Error()
	}
	row.Display = value
	if isAutoTemplate(raw) {
		row.Auto = true
		row.RawPreview = raw
		row.AutoPreviews = autoPool(n, row.Name, value)
	}
	if isPANField(n, row.Name) && value != "" {
		row.Masked = true
		row.Display = maskPAN(value)
		// The pool renders INSTEAD of Display (poolValue), so its
		// entries are masked too — one behavior: mask, never omit.
		for i := range row.AutoPreviews {
			row.AutoPreviews[i] = maskPAN(row.AutoPreviews[i])
		}
	}
	if depth < fieldTreeDepth {
		row.Children = subfieldRows(f, depth+1)
	}

	return row
}

// subfieldRows renders a composite field's present subfields in numeric
// tag order (the same order utils.Describe shows).
func subfieldRows(f field.Field, depth int) []pages.FieldRow {
	c, ok := f.(interface{ GetSubfields() map[string]field.Field })
	if !ok {
		return nil
	}
	subs := c.GetSubfields()
	keys := make([]string, 0, len(subs))
	for k := range subs {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	sort.SliceStable(keys, func(i, j int) bool { return subkeyLess(keys[i], keys[j]) })

	out := make([]pages.FieldRow, 0, len(subs))
	for _, k := range keys {
		sub := subs[k]
		row := pages.FieldRow{Num: k}
		if spec := sub.Spec(); spec != nil {
			row.Name = spec.Description
		}
		if v, err := sub.String(); err == nil {
			row.Display = v
		} else {
			row.Error = err.Error()
		}
		// Subfields carry names, not top-level field numbers: mask by
		// name (account-number / track / PIN / EMV subfields).
		if isSensitiveName(row.Name) && row.Display != "" {
			row.Masked = true
			row.Display = maskPAN(row.Display)
		}
		row.Children = subfieldRows(sub, depth+1)
		out = append(out, row)
	}

	return out
}

// subkeyLess orders subfield tags numerically when both parse.
func subkeyLess(a, b string) bool {
	na, ea := strconv.Atoi(a)
	nb, eb := strconv.Atoi(b)
	if ea == nil && eb == nil {
		return na < nb
	}

	return a < b
}

// sensitiveFieldNums mirrors moov's DefaultFilters (field_filter.go at
// iso8583 v0.26.0): 2 PAN, 20 PAN extended, 35/36/45 track 2/3/1,
// 52 PIN, 55 EMV. A new moov default filter must be added here too.
var sensitiveFieldNums = map[int]bool{
	2: true, 20: true, 35: true, 36: true, 45: true, 52: true, 55: true,
}

// isPANField marks a field whose value carries PAN-family data for
// masking: the moov default-filter set by number, or a spec description
// naming an account number / track / PIN / EMV.
func isPANField(n int, name string) bool {
	if sensitiveFieldNums[n] {
		return true
	}

	return isSensitiveName(name)
}

// isSensitiveName matches spec descriptions naming PAN-family data:
// account numbers (top-level or composite subfields), track data, PIN
// data, and EMV/ICC tag data. PIN/EMV match as whole words so "Mapping
// Key" style names stay unmasked.
func isSensitiveName(name string) bool {
	l := strings.ToLower(name)
	if strings.Contains(l, "account number") || strings.Contains(l, "track") {
		return true
	}

	return containsWord(l, "pin") || containsWord(l, "emv")
}

// containsWord reports whether word appears in s delimited by
// non-letters ("pin data" matches "pin", "mapping" does not).
func containsWord(s, word string) bool {
	for i := 0; ; {
		j := strings.Index(s[i:], word)
		if j < 0 {
			return false
		}
		j += i
		end := j + len(word)
		before := j == 0 || !isASCIILetter(s[j-1])
		after := end >= len(s) || !isASCIILetter(s[end])
		if before && after {
			return true
		}
		i = end
	}
}

// isASCIILetter reports an ASCII letter (word-boundary test partner).
func isASCIILetter(b byte) bool {
	return (b >= 'a' && b <= 'z') || (b >= 'A' && b <= 'Z')
}

// maskPAN keeps the first/last 4 digits and hides the middle with the
// • glyph (the page substitutes * under theme.ASCII).
func maskPAN(v string) string {
	if len(v) <= 8 {
		return strings.Repeat("•", len(v))
	}

	return v[:4] + strings.Repeat("•", len(v)-8) + v[len(v)-4:]
}

// autoPool generates autoPoolSize deterministic preview variants for a
// composed auto value: numeric counters increment, transmission
// timestamps (MMDDhhmmss) advance by one minute, anything non-derivable
// yields no pool (the page's `r` is then a no-op on that row).
func autoPool(n int, name, value string) []string {
	if value == "" {
		return nil
	}
	if n == 7 || strings.Contains(name, "Transmission") {
		if pool := tsPool(value); pool != nil {
			return pool
		}
	}
	if isDigits(value) {
		return incrPool(value)
	}
	if pool := tailDigitPool(value); pool != nil {
		return pool
	}

	return nil
}

// tsPool advances a MMDDhhmmss transmission timestamp by one minute per
// variant (year-less layout, like utils.GetTrxnDateTime).
func tsPool(value string) []string {
	t, err := time.Parse("0102150405", value)
	if err != nil || len(value) != 10 {
		return nil
	}
	out := make([]string, 0, autoPoolSize)
	for i := range autoPoolSize {
		out = append(out, t.Add(time.Duration(i)*time.Minute).Format("0102150405"))
	}

	return out
}

// incrPool increments an all-digit value (STAN/RRN counters), keeping
// the zero padding width.
func incrPool(value string) []string {
	v, err := strconv.ParseUint(value, 10, 64)
	if err != nil {
		return nil
	}
	out := make([]string, 0, autoPoolSize)
	for i := range autoPoolSize {
		out = append(out, padDigits(v+uint64(i), len(value)))
	}

	return out
}

// tailDigitPool increments the trailing digit run of a mixed value
// (RRN-style prefixes), returning nil when it does not end in a digit.
func tailDigitPool(value string) []string {
	if value == "" || value[len(value)-1] < '0' || value[len(value)-1] > '9' {
		return nil
	}
	start := len(value)
	for start > 0 && value[start-1] >= '0' && value[start-1] <= '9' {
		start--
	}

	return incrPool(value[start:])
}

// padDigits formats v zero-padded to width w.
func padDigits(v uint64, w int) string {
	s := strconv.FormatUint(v, 10)
	if len(s) >= w {
		return s
	}

	return strings.Repeat("0", w-len(s)) + s
}

// isDigits reports an all-digit non-empty string.
func isDigits(s string) bool {
	if s == "" {
		return false
	}
	for _, r := range s {
		if r < '0' || r > '9' {
			return false
		}
	}

	return true
}

// fieldNumMentioned extracts the first "field <n>" number cited in a
// validation error ("required field 2 (...) is missing"); 0 when none.
func fieldNumMentioned(text string) int {
	i := strings.Index(text, "field ")
	if i < 0 {
		return 0
	}
	rest := text[i+len("field "):]
	j := 0
	for j < len(rest) && rest[j] >= '0' && rest[j] <= '9' {
		j++
	}
	if j == 0 {
		return 0
	}
	n, err := strconv.Atoi(rest[:j])
	if err != nil {
		return 0
	}

	return n
}

// attachErrorAt red-lines the top-level row numbered n (recursing into
// children when the number only appears there).
func attachErrorAt(rows []pages.FieldRow, n int, text string) {
	if n == 0 {
		return
	}
	for i := range rows {
		if rows[i].Num == strconv.Itoa(n) {
			if rows[i].Error == "" {
				rows[i].Error = text
			}

			return
		}
	}
}

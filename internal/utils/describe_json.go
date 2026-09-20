package utils

import (
	"encoding/hex"
	"fmt"
	"io"
	"strconv"
	"strings"
	"text/tabwriter"

	"github.com/moov-io/iso8583"
	"github.com/moov-io/iso8583/field"
	moovsort "github.com/moov-io/iso8583/sort"
)

// DescribeStoredJSON writes a session-recorded message JSON in the same
// readable form Describe produces: spec header, MTI, bitmap and one row per
// field, composite values rendered as nested SUBFIELDS blocks. Unlike
// Describe — which reads live moov fields — this renders the recorded
// values, so the tree stays complete and correctly structured even when the
// currently-resolved spec is a different dialect than the message spoke
// (repacking a stamped Visa record with a session-config flex spec used to
// stringify composites as Go map dumps and fail fixed-length packs).
// Descriptions come from the resolved spec when it knows the field; the
// values always stay as recorded.
func DescribeStoredJSON(w io.Writer, mti string, fields map[string]any, spec *iso8583.MessageSpec) {
	name := defaultSpecName
	if spec != nil && spec.Name != "" {
		name = spec.Name
	}
	_, _ = fmt.Fprintf(w, "%s Message:\n", name)

	tw := tabwriter.NewWriter(w, 0, 0, 2, '.', 0)
	_, _ = fmt.Fprintf(tw, "MTI\t: %s\n", mti)
	writeRecordBitmap(tw, fields)

	keys := make([]string, 0, len(fields))
	for k := range fields {
		if k == "1" { // the bitmap is reported as its own pair of lines
			continue
		}
		keys = append(keys, k)
	}
	moovsort.StringsByInt(keys)
	for _, k := range keys {
		writeRecordField(tw, "", k, fields[k], recordFieldSpec(spec, k))
	}
	_ = tw.Flush()
}

// recordFieldSpec resolves the field spec (description + subfield specs)
// behind a recorded top-level field id, nil when the spec does not know it.
func recordFieldSpec(spec *iso8583.MessageSpec, id string) *field.Spec {
	if spec == nil {
		return nil
	}
	n, err := strconv.Atoi(id)
	if err != nil {
		return nil
	}
	f := spec.Fields[n]
	if f == nil {
		return nil
	}

	return f.Spec()
}

// writeRecordField renders one recorded value through the shared tabwriter:
// a plain row, or for a composite a SUBFIELDS header, nested rows, and a
// closing rule — the exact shape DescribeFieldContainer uses for composite
// fields.
func writeRecordField(tw *tabwriter.Writer, indent, id string, v any, fs *field.Spec) {
	desc := ""
	if fs != nil {
		desc = fs.Description
	}
	children, composite := v.(map[string]any)
	if !composite {
		_, _ = fmt.Fprintf(tw, "%sF%-3s %s\t: %v\n", indent, id, desc, v)
		return
	}
	_, _ = fmt.Fprintf(tw, "%sF%-3s %s SUBFIELDS:\n", indent, id, desc)
	_, _ = fmt.Fprintf(tw, "%s----------------------------------------\n", indent)
	for _, k := range SortedSubfieldKeys(children) {
		var sub *field.Spec
		if fs != nil && fs.Subfields != nil {
			if sf, ok := fs.Subfields[k]; ok && sf != nil {
				sub = sf.Spec()
			}
		}
		writeRecordField(tw, indent+"  ", k, children[k], sub)
	}
	_, _ = fmt.Fprintf(tw, "%s----------------------------------------\n", indent)
}

// writeRecordBitmap synthesises the bitmap lines from the recorded field
// ids: a bit is set iff that field was carried, and bit 1 opens the second
// block whenever a field above 64 is present.
func writeRecordBitmap(tw *tabwriter.Writer, fields map[string]any) {
	bits := make([]byte, 128)
	for i := range bits {
		bits[i] = '0'
	}
	secondBlock := false
	for k := range fields {
		n, err := strconv.Atoi(k)
		if err != nil || n < 2 || n > 128 {
			continue
		}
		bits[n-1] = '1'
		if n > 64 {
			secondBlock = true
		}
	}
	if !secondBlock {
		bits = bits[:64]
	} else {
		bits[0] = '1'
	}
	raw := make([]byte, len(bits)/8)
	for i, b := range bits {
		if b == '1' {
			raw[i/8] |= 1 << (7 - uint(i%8))
		}
	}
	_, _ = fmt.Fprintf(tw, "Bitmap HEX\t: %s\n", strings.ToUpper(hex.EncodeToString(raw)))
	_, _ = fmt.Fprintf(tw, "Bitmap bits\t:\n%s\n", splitAndAnnotate(string(bits)))
}

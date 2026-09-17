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

var defaultSpecName = "ISO 8583"

// FieldContainer should be implemented by the type to be described.
// We use GetSubfields as a common method to get subfields.
// While Message doesn't implement FieldContainer directly, we wrap it with MessageWrapper.
type FieldContainer interface {
	GetSubfields() map[string]field.Field
}

// ContainerWithBitmap is anything that can show its bitmap apart from its fields,
// which is how Describe reports which fields the bitmap actually set.
type ContainerWithBitmap interface {
	Bitmap() *field.Bitmap
}

// FilterFunc names iso8583's filter predicate for callers of Describe. An alias
// rather than a wrapper, so a filter written against iso8583 works unchanged.
type FilterFunc = iso8583.FilterFunc

// FieldFilter names iso8583's filter-set builder: a function that adds the fields
// Describe should leave out of its output.
type FieldFilter = iso8583.FieldFilter

var (
	// DefaultFilters is what Describe applies when the caller passes no filter: the
	// fields iso8583 declines to decode inline.
	DefaultFilters = iso8583.DefaultFilters
	// DoNotFilterFields decodes every field Describe knows about, for the operator
	// who wants the whole message including the parts the default set leaves encoded.
	DoNotFilterFields = iso8583.DoNotFilterFields
	// NoOpFilter decodes nothing and skips nothing, the filter set to pass when
	// Describe should show fields without interpreting any of them.
	NoOpFilter = iso8583.NoOpFilter
	// EMVFilter leaves the EMV tag sequence (field 55) encoded, because decoding it
	// needs the tag registry the describe path does not carry.
	EMVFilter = iso8583.EMVFilter
	// PINFilter leaves the PIN block (field 52) encoded, so the describe output
	// never renders something that must not be shown.
	PINFilter = iso8583.PINFilter
	// PANFilter leaves the primary account number (field 2) as the operator
	// entered it rather than reformatting it.
	PANFilter = iso8583.PANFilter
	// Track1Filter leaves track 1 data (field 35) encoded.
	Track1Filter = iso8583.Track1Filter
	// Track2Filter leaves track 2 data (field 36) encoded.
	Track2Filter = iso8583.Track2Filter
	// Track3Filter leaves track 3 data (field 45) encoded.
	Track3Filter = iso8583.Track3Filter
)

// FilterField is how a caller says "describe everything except these fields".
var FilterField = iso8583.FilterField

// MessageWrapper implements FieldContainer for the iso8583.Message, since it has
// GetFields but not GetSubfields and returns map[int]field.Field.
type MessageWrapper struct {
	*iso8583.Message
}

// GetSubfields returns the message's fields keyed by decimal string, the keying a
// FieldContainer uses where iso8583.Message keys them by int. That single
// difference is the reason MessageWrapper exists.
func (m *MessageWrapper) GetSubfields() map[string]field.Field {
	fields := m.GetFields()
	result := make(map[string]field.Field, len(fields))
	for k, v := range fields {
		result[strconv.Itoa(k)] = v
	}
	return result
}

// Describe writes a human-readable description of an ISO8583 message.
func Describe(message *iso8583.Message, w io.Writer, filters ...FieldFilter) error {
	specName := defaultSpecName
	if spec := message.GetSpec(); spec != nil && spec.Name != "" {
		specName = spec.Name
	}
	_, _ = fmt.Fprintf(w, "%s Message:\n", specName)

	tw := tabwriter.NewWriter(w, 0, 0, 2, '.', 0)

	mti, err := message.GetMTI()
	if err != nil {
		return fmt.Errorf("getting MTI: %w", err)
	}
	_, _ = fmt.Fprintf(tw, "MTI\t: %s\n", mti)

	if len(filters) == 0 {
		filters = DefaultFilters()
	}

	err = DescribeFieldContainer(&MessageWrapper{message}, tw, "", filters...)
	if err != nil {
		return fmt.Errorf("describing message: %w", err)
	}

	return tw.Flush()
}

// DescribeFieldContainer describes the FieldContainer (for example, a wrapped message or a composite field).
func DescribeFieldContainer(container FieldContainer, w io.Writer, indent string, filters ...FieldFilter) error {
	filterMap := make(map[string]FilterFunc)
	for _, filter := range filters {
		filter(filterMap)
	}

	var errorList []string

	var bitmap *field.Bitmap
	if container, ok := container.(ContainerWithBitmap); ok {
		bitmap = container.Bitmap()
	}

	if bitmap != nil {
		if err := describeBitmap(w, indent, bitmap); err != nil {
			return err
		}
	}

	fields := container.GetSubfields()

	for _, i := range sortFieldIDs(fields) {
		f := fields[i]

		if f == bitmap {
			continue
		}

		desc := ""
		if f.Spec() != nil {
			desc = f.Spec().Description
		}

		if container, ok := f.(FieldContainer); ok {
			_, _ = fmt.Fprintf(w, "%sF%-3s %s SUBFIELDS:\n", indent, i, desc)
			_, _ = fmt.Fprintf(w, "%s----------------------------------------\n", indent)
			if err := DescribeFieldContainer(container, w, indent+"  ", filters...); err != nil {
				return err
			}
			_, _ = fmt.Fprintf(w, "%s----------------------------------------\n", indent)
			continue
		}

		str, err := f.String()
		if err != nil {
			errorList = append(errorList, err.Error())
			continue
		}

		if filter, existed := filterMap[i]; existed {
			str = filter(str, fields[i])
		}

		_, _ = fmt.Fprintf(w, "%sF%-3s %s\t: %s\n", indent, i, desc, str)
	}

	if len(errorList) > 0 {
		_, _ = fmt.Fprintf(w, "\nUnpacking Errors:\n")
		for _, err := range errorList {
			_, _ = fmt.Fprintf(w, "- %s:\n", err)
		}
		return fmt.Errorf("displaying fields: %s", strings.Join(errorList, ","))
	}

	return nil
}

// describeBitmap writes the bitmap's hex and annotated bit-string representations.
func describeBitmap(w io.Writer, indent string, bitmap *field.Bitmap) error {
	bitmapRaw, err := bitmap.Bytes()
	if err != nil {
		return fmt.Errorf("getting bitmap bytes: %w", err)
	}
	_, _ = fmt.Fprintf(w, "%sBitmap HEX\t: %s\n", indent, strings.ToUpper(hex.EncodeToString(bitmapRaw)))

	bits, err := bitmap.String()
	if err != nil {
		return fmt.Errorf("getting bitmap: %w", err)
	}
	_, _ = fmt.Fprintf(w, "%sBitmap bits\t:\n%s\n", indent, splitAndAnnotate(bits))

	return nil
}

func sortFieldIDs(fields map[string]field.Field) []string {
	keys := make([]string, 0, len(fields))
	for k := range fields {
		keys = append(keys, k)
	}

	moovsort.StringsByInt(keys)
	return keys
}

// splitAndAnnotate splits bit blocks and annotates them with bit numbers.
func splitAndAnnotate(bits string) string {
	if bits == "" {
		return ""
	}

	bitBlocks := strings.Split(bits, " ")
	annotatedBits := make([]string, len(bitBlocks))
	bitsCount := len(bitBlocks[0])

	pad := 0
	if len(bitBlocks) > 4 {
		pad = 9
	}

	for i, block := range bitBlocks {
		startBit := i*bitsCount + 1
		endBit := (i + 1) * bitsCount
		pos := fmt.Sprintf("[%d-%d]", startBit, endBit)
		annotatedBits[i] = fmt.Sprintf("%*s%s", pad, pos, block)

		isLastBlock := i == len(bitBlocks)-1
		isEndOf32Bits := endBit%32 == 0
		if isEndOf32Bits && !isLastBlock {
			annotatedBits[i] += "\n"
		} else if !isLastBlock {
			annotatedBits[i] += " "
		}
	}

	return strings.Join(annotatedBits, "")
}

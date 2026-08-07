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
// We use GetSubfields() as a common method to get subfields.
// While Message doesn't implement FieldContainer directly, we wrap it with MessageWrapper.
type FieldContainer interface {
	GetSubfields() map[string]field.Field
}

type ContainerWithBitmap interface {
	Bitmap() *field.Bitmap
}

type FilterFunc = iso8583.FilterFunc

type FieldFilter = iso8583.FieldFilter

var DefaultFilters = iso8583.DefaultFilters
var DoNotFilterFields = iso8583.DoNotFilterFields
var NoOpFilter = iso8583.NoOpFilter
var EMVFilter = iso8583.EMVFilter
var PINFilter = iso8583.PINFilter
var PANFilter = iso8583.PANFilter
var Track1Filter = iso8583.Track1Filter
var Track2Filter = iso8583.Track2Filter
var Track3Filter = iso8583.Track3Filter

var FilterField = iso8583.FilterField

// MessageWrapper implements FieldContainer for the iso8583.Message, since it has
// GetFields() but not GetSubfields() and returns map[int]field.Field.
type MessageWrapper struct {
	*iso8583.Message
}

func (m *MessageWrapper) GetSubfields() map[string]field.Field {
	fields := m.Message.GetFields()
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
	fmt.Fprintf(w, "%s Message:\n", specName)

	tw := tabwriter.NewWriter(w, 0, 0, 2, '.', 0)

	mti, err := message.GetMTI()
	if err != nil {
		return fmt.Errorf("getting MTI: %w", err)
	}
	fmt.Fprintf(tw, "MTI\t: %s\n", mti)

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
		bitmapRaw, err := bitmap.Bytes()
		if err != nil {
			return fmt.Errorf("getting bitmap bytes: %w", err)
		}
		fmt.Fprintf(w, "%sBitmap HEX\t: %s\n", indent, strings.ToUpper(hex.EncodeToString(bitmapRaw)))

		bits, err := bitmap.String()
		if err != nil {
			return fmt.Errorf("getting bitmap: %w", err)
		}
		fmt.Fprintf(w, "%sBitmap bits\t:\n%s\n", indent, splitAndAnnotate(bits))
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
			fmt.Fprintf(w, "%sF%-3s %s SUBFIELDS:\n", indent, i, desc)
			fmt.Fprintf(w, "%s----------------------------------------\n", indent)
			if err := DescribeFieldContainer(container, w, indent+"  ", filters...); err != nil {
				return err
			}
			fmt.Fprintf(w, "%s----------------------------------------\n", indent)
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

		fmt.Fprintf(w, "%sF%-3s %s\t: %s\n", indent, i, desc, str)
	}

	if len(errorList) > 0 {
		fmt.Fprintf(w, "\nUnpacking Errors:\n")
		for _, err := range errorList {
			fmt.Fprintf(w, "- %s:\n", err)
		}
		return fmt.Errorf("displaying fields: %s", strings.Join(errorList, ","))
	}

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

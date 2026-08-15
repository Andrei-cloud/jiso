package analyzer

import (
	"fmt"
	"sort"
	"strconv"

	"github.com/moov-io/iso8583"
	"github.com/moov-io/iso8583/field"

	"jiso/internal/utils"
)

func buildMessageTemplateFields(msg *iso8583.Message, spec *iso8583.MessageSpec, unsecure bool) map[string]interface{} {
	txFields := make(map[string]interface{})
	if msg == nil {
		return txFields
	}

	var fIDs []int
	for i, f := range msg.GetFields() {
		if f == nil || i == 1 { // Skip DE 1 (Bitmap)
			continue
		}
		_, ok := extractFieldValueForTemplate(f)
		if !ok {
			continue
		}
		fIDs = append(fIDs, i)
	}
	sort.Ints(fIDs)

	for _, i := range fIDs {
		f := msg.GetField(i)
		if f == nil {
			continue
		}
		extracted, ok := extractFieldValueForTemplate(f)
		if !ok {
			continue
		}
		extracted = AnonymizeFieldValue(i, extracted, unsecure)
		fieldKey := fmt.Sprintf("%d", i)

		if i == 7 || i == 11 || i == 37 || i == 38 {
			txFields[fieldKey] = "auto"
		} else if isNumericField(spec, i) && i != 0 {
			if strVal, isStr := extracted.(string); isStr {
				if num, err := strconv.ParseInt(strVal, 10, 64); err == nil {
					txFields[fieldKey] = num
				} else {
					txFields[fieldKey] = strVal
				}
			} else {
				txFields[fieldKey] = extracted
			}
		} else {
			txFields[fieldKey] = extracted
		}
	}

	return txFields
}

// extractFieldValueForTemplate converts field values into JSON-friendly values.
// Composite fields are expanded into nested maps of subfield values.
func extractFieldValueForTemplate(f field.Field, specField ...*field.Spec) (interface{}, bool) {
	var sf *field.Spec
	if len(specField) > 0 {
		sf = specField[0]
	}
	return utils.ExtractFieldData(f, sf)
}

func buildPlaceholderValue(prefix string, value interface{}) interface{} {
	nested, ok := value.(map[string]interface{})
	if !ok {
		return fmt.Sprintf("{{data.%s}}", prefix)
	}

	result := make(map[string]interface{}, len(nested))
	keys := sortedNumericOrStringKeys(nested)

	for _, key := range keys {
		result[key] = buildPlaceholderValue(prefix+"_"+key, nested[key])
	}

	return result
}

func flattenValueForDataset(prefix string, value interface{}, row map[string]string) {
	nested, ok := value.(map[string]interface{})
	if !ok {
		row[prefix] = fmt.Sprintf("%v", value)
		return
	}

	keys := sortedNumericOrStringKeys(nested)

	for _, key := range keys {
		flattenValueForDataset(prefix+"_"+key, nested[key], row)
	}
}

func sortedNumericOrStringKeys[V any](m map[string]V) []string {
	keys := make([]string, 0, len(m))
	for k := range m {
		keys = append(keys, k)
	}
	sort.Slice(keys, func(i, j int) bool {
		ai, errI := strconv.Atoi(keys[i])
		aj, errJ := strconv.Atoi(keys[j])
		if errI == nil && errJ == nil {
			return ai < aj
		}
		return keys[i] < keys[j]
	})
	return keys
}

func mergeStructuredValues(dst, src map[string]interface{}) map[string]interface{} {
	if dst == nil {
		dst = make(map[string]interface{})
	}
	for key, srcVal := range src {
		srcNested, srcIsMap := srcVal.(map[string]interface{})
		dstVal, exists := dst[key]
		dstNested, dstIsMap := dstVal.(map[string]interface{})

		switch {
		case srcIsMap && dstIsMap:
			dst[key] = mergeStructuredValues(dstNested, srcNested)
		case srcIsMap:
			dst[key] = mergeStructuredValues(make(map[string]interface{}), srcNested)
		case !exists:
			dst[key] = srcVal
		}
	}
	return dst
}

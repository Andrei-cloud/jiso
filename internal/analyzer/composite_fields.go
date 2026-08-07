package analyzer

import (
	"fmt"
	"sort"
	"strconv"

	"github.com/moov-io/iso8583/field"
)

// extractFieldValueForTemplate converts field values into JSON-friendly values.
// Composite fields are expanded into nested maps of subfield values.
func extractFieldValueForTemplate(f field.Field) (interface{}, bool) {
	if f == nil {
		return nil, false
	}

	composite, ok := f.(*field.Composite)
	if !ok {
		v, err := f.String()
		if err != nil || v == "" {
			return nil, false
		}
		return v, true
	}

	subfields := composite.GetSubfields()
	if len(subfields) == 0 {
		v, err := f.String()
		if err != nil || v == "" {
			return nil, false
		}
		return v, true
	}

	result := make(map[string]interface{})
	keys := make([]string, 0, len(subfields))
	for key := range subfields {
		keys = append(keys, key)
	}

	sort.Slice(keys, func(i, j int) bool {
		ai, errI := strconv.Atoi(keys[i])
		aj, errJ := strconv.Atoi(keys[j])
		if errI == nil && errJ == nil {
			return ai < aj
		}
		return keys[i] < keys[j]
	})

	for _, key := range keys {
		if key == "0" {
			continue
		}
		v, ok := extractFieldValueForTemplate(subfields[key])
		if !ok {
			continue
		}
		result[key] = v
	}

	if len(result) == 0 {
		v, err := f.String()
		if err != nil || v == "" {
			return nil, false
		}
		return v, true
	}

	return result, true
}

func buildPlaceholderValue(prefix string, value interface{}) interface{} {
	nested, ok := value.(map[string]interface{})
	if !ok {
		return fmt.Sprintf("{{data.%s}}", prefix)
	}

	result := make(map[string]interface{}, len(nested))
	keys := make([]string, 0, len(nested))
	for key := range nested {
		keys = append(keys, key)
	}
	sort.Slice(keys, func(i, j int) bool {
		ai, errI := strconv.Atoi(keys[i])
		aj, errJ := strconv.Atoi(keys[j])
		if errI == nil && errJ == nil {
			return ai < aj
		}
		return keys[i] < keys[j]
	})

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

	keys := make([]string, 0, len(nested))
	for key := range nested {
		keys = append(keys, key)
	}
	sort.Slice(keys, func(i, j int) bool {
		ai, errI := strconv.Atoi(keys[i])
		aj, errJ := strconv.Atoi(keys[j])
		if errI == nil && errJ == nil {
			return ai < aj
		}
		return keys[i] < keys[j]
	})

	for _, key := range keys {
		flattenValueForDataset(prefix+"_"+key, nested[key], row)
	}
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

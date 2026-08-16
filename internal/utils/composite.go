package utils

import (
	"fmt"
	"math"
	"sort"
	"strconv"
	"strings"

	"github.com/moov-io/iso8583"
	"github.com/moov-io/iso8583/field"
)

// SetCompositeFieldValue resolves dynamic values and packs a map[string]interface{} into a composite field of an iso8583.Message.
func SetCompositeFieldValue(msg *iso8583.Message, spec *iso8583.MessageSpec, fieldID int, value map[string]interface{}) error {
	if spec == nil || spec.Fields == nil {
		return fmt.Errorf("spec is required for composite field %d", fieldID)
	}

	specField, exists := spec.Fields[fieldID]
	if !exists || specField == nil {
		return fmt.Errorf("field %d is not defined in spec", fieldID)
	}

	instance := field.NewInstanceOf(specField)
	composite, ok := instance.(*field.Composite)
	if !ok {
		return msg.Field(fieldID, fmt.Sprintf("%v", value))
	}

	resolvedValue := resolveDynamicCompositeValue(value)

	if err := applyCompositePaths(composite, resolvedValue, ""); err != nil {
		return fmt.Errorf("failed to apply composite field %d: %w", fieldID, err)
	}

	packed, err := composite.Bytes()
	if err != nil {
		return fmt.Errorf("failed to pack composite field %d: %w", fieldID, err)
	}

	return msg.BinaryField(fieldID, packed)
}

func resolveDynamicCompositeValue(value map[string]interface{}) map[string]interface{} {
	res := make(map[string]interface{}, len(value))
	for k, v := range value {
		switch val := v.(type) {
		case string:
			cleanVal := strings.TrimSpace(strings.ToLower(val))
			switch cleanVal {
			case "auth_code", "$auth_code", "gen_auth_code":
				res[k] = RandString(6)
			case "stan", "$stan", "gen_stan":
				res[k] = GetCounter().GetStan()
			case "rrn", "$rrn", "gen_rrn":
				res[k] = GetRRNInstance().GetRRN()
			case "datetime", "$datetime":
				res[k] = GetTrxnDateTime()
			default:
				res[k] = val
			}
		case map[string]interface{}:
			res[k] = resolveDynamicCompositeValue(val)
		default:
			res[k] = v
		}
	}
	return res
}

func applyCompositePaths(composite *field.Composite, value map[string]interface{}, prefix string) error {
	for key, raw := range value {
		path := key
		if prefix != "" {
			path = prefix + "." + key
		}

		if nested, ok := raw.(map[string]interface{}); ok {
			if err := applyCompositePaths(composite, nested, path); err != nil {
				return err
			}
			continue
		}

		normalized := normalizeCompositeScalar(raw)
		if err := composite.MarshalPath(path, normalized); err != nil {
			return err
		}
	}

	return nil
}

func normalizeCompositeScalar(v interface{}) interface{} {
	switch val := v.(type) {
	case string:
		return val
	case float64:
		if math.Mod(val, 1) == 0 {
			return strconv.FormatInt(int64(val), 10)
		}
		return strconv.FormatFloat(val, 'f', -1, 64)
	case int:
		return strconv.Itoa(val)
	case int64:
		return strconv.FormatInt(val, 10)
	case bool:
		return strconv.FormatBool(val)
	default:
		return fmt.Sprintf("%v", val)
	}
}

// ExtractFieldData extracts data from an iso8583 field into JSON-serializable values (scalar or nested map for composites).
func ExtractFieldData(f field.Field, specField *field.Spec) (interface{}, bool) {
	if f == nil {
		return nil, false
	}

	if composite, ok := f.(*field.Composite); ok && composite != nil {
		subfields := composite.GetSubfields()
		if len(subfields) > 0 {
			res := make(map[string]interface{})
			for _, k := range SortedSubfieldKeys(subfields) {
				if k == "0" { // Skip bitmap subfield in composite
					continue
				}
				var subSpec *field.Spec
				if specField != nil && specField.Subfields != nil {
					if sf, ok := specField.Subfields[k]; ok && sf != nil {
						subSpec = sf.Spec()
					}
				}
				if val, ok := ExtractFieldData(subfields[k], subSpec); ok && val != nil {
					res[k] = val
				}
			}
			if len(res) > 0 {
				return res, true
			}
		}
	}

	// If the field is not a *field.Composite, but the spec defines it as a Composite, attempt to unpack raw bytes
	if specField != nil && len(specField.Subfields) > 0 {
		if rawBytes, err := f.Bytes(); err == nil && len(rawBytes) > 0 {
			comp := field.NewComposite(specField)
			if _, err := comp.Unpack(rawBytes); err == nil {
				return ExtractFieldData(comp, specField)
			}
		}
	}

	str, err := f.String()
	if err != nil || str == "" {
		return nil, false
	}
	return str, true
}

// SortedSubfieldKeys returns numeric-first sorted keys of subfields
func SortedSubfieldKeys[T any](m map[string]T) []string {
	keys := make([]string, 0, len(m))
	for k := range m {
		keys = append(keys, k)
	}
	sort.Slice(keys, func(i, j int) bool {
		numI, errI := strconv.Atoi(keys[i])
		numJ, errJ := strconv.Atoi(keys[j])
		if errI == nil && errJ == nil {
			return numI < numJ
		}
		if errI == nil {
			return true
		}
		if errJ == nil {
			return false
		}
		return keys[i] < keys[j]
	})
	return keys
}

// ExtractMessageFields extracts all fields (and subfields) from an iso8583.Message into a structured map based on spec.
func ExtractMessageFields(msg *iso8583.Message, spec *iso8583.MessageSpec) map[string]interface{} {
	if msg == nil {
		return nil
	}
	fields := make(map[string]interface{})
	for i := 2; i <= 128; i++ {
		f := msg.GetField(i)
		if f == nil {
			continue
		}
		var fieldSpec *field.Spec
		if spec != nil && spec.Fields != nil {
			if sf, ok := spec.Fields[i]; ok && sf != nil {
				fieldSpec = sf.Spec()
			}
		}
		if val, ok := ExtractFieldData(f, fieldSpec); ok && val != nil {
			fields[strconv.Itoa(i)] = val
		}
	}
	return fields
}


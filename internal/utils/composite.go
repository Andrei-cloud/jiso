package utils

import (
	"fmt"
	"math"
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

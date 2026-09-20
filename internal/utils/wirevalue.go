package utils

import (
	"strings"

	"github.com/moov-io/iso8583/field"
)

// WireValue renders a primitive field's value the way Pack puts it on the
// wire. moov's Numeric keeps an int64, so the leading zero a fixed-length
// field actually carries ("0920160705") is invisible to String(); the packer
// re-derives it from the spec's padding at Pack time. Mirroring that padding
// here keeps recorded field JSON, describe trees and route matching from
// losing zeros the link did carry. Specs without a padder reject short fixed
// values at pack, so a value that did travel over the wire was padded with
// zeros by its spec; left-filling digits mirrors what the bytes said.
func WireValue(f field.Field) (string, error) {
	if f == nil {
		return "", nil
	}
	str, err := f.String()
	if err != nil || str == "" {
		return str, err
	}
	_, numeric := f.(*field.Numeric)
	if !numeric {
		if _, ok := f.(*field.String); !ok {
			return str, nil
		}
	}
	spec := f.Spec()
	if spec == nil || spec.Length <= 0 || len(str) >= spec.Length {
		return str, nil
	}
	if spec.Pad != nil {
		if b := spec.Pad.Pad([]byte(str), spec.Length); len(b) == spec.Length {
			return string(b), nil
		}
		return str, nil
	}
	// Numeric without a padder: a value that did travel a fixed-length link
	// was zero-filled by the wire even if this spec omits the padder.
	if numeric && strings.TrimLeft(str, "0123456789") == "" {
		return strings.Repeat("0", spec.Length-len(str)) + str, nil
	}

	return str, nil
}

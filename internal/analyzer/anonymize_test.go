package analyzer

import (
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestAnonymizePAN(t *testing.T) {
	// Standard 16-digit card number
	orig16 := "9876543210987654"
	anon16 := AnonymizePAN(orig16)
	assert.Equal(t, 16, len(anon16))
	assert.Equal(t, "98765432", anon16[:8], "First 8 digits (BIN) must be preserved")
	assert.Equal(t, "000", anon16[13:16], "Positions 14, 15, 16 must be '000'")
	assert.NotEqual(t, orig16, anon16, "Original PAN must be masked")

	// 19-digit card number
	orig19 := "1234567890123456789"
	anon19 := AnonymizePAN(orig19)
	assert.Equal(t, 19, len(anon19))
	assert.Equal(t, "12345678", anon19[:8])
	assert.Equal(t, "000", anon19[13:16])

	// Short string under 9 digits
	short := "1234567"
	assert.Equal(t, "1234567", AnonymizePAN(short))
}

func TestAnonymizeTrack2(t *testing.T) {
	// Track 2 with '=' separator
	tr2Equals := "9876543210987654=2601123456789"
	anonTr2 := AnonymizeTrack2(tr2Equals)
	parts := strings.Split(anonTr2, "=")
	require.Len(t, parts, 2)
	assert.Equal(t, "98765432", parts[0][:8])
	assert.Equal(t, "000", parts[0][13:16])
	assert.Equal(t, "2601123456789", parts[1])

	// Track 2 with 'D' separator
	tr2D := "9876543210987654D2601123456789"
	anonTr2D := AnonymizeTrack2(tr2D)
	dParts := strings.Split(anonTr2D, "D")
	require.Len(t, dParts, 2)
	assert.Equal(t, "98765432", dParts[0][:8])
	assert.Equal(t, "000", dParts[0][13:16])
}

func TestAnonymizeTrack1(t *testing.T) {
	tr1 := "%B9876543210987654^SMITH/JOHN^260112345"
	anonTr1 := AnonymizeTrack1(tr1)
	assert.True(t, strings.HasPrefix(anonTr1, "%B98765432"))
	assert.True(t, strings.Contains(anonTr1, "000^SMITH/JOHN^260112345"))
}

func TestAnonymizeFieldValueUnsecureFlag(t *testing.T) {
	origPAN := "9876543210987654"

	// Unsecure = true -> should keep original clear PAN
	unsecRes := AnonymizeFieldValue(2, origPAN, true)
	assert.Equal(t, origPAN, unsecRes)

	// Unsecure = false (Secure mode) -> should anonymize
	secRes := AnonymizeFieldValue(2, origPAN, false)
	assert.NotEqual(t, origPAN, secRes)
	assert.Equal(t, "98765432", secRes.(string)[:8])
	assert.Equal(t, "000", secRes.(string)[13:16])
}

func TestAnonymizeCompositeChipFields(t *testing.T) {
	compMap := map[string]interface{}{
		"57":   "9876543210987654D2601123456789",
		"9F26": "11223344",
	}

	anonMap := AnonymizeFieldValue(55, compMap, false).(map[string]interface{})
	tr2Val := anonMap["57"].(string)
	assert.True(t, strings.HasPrefix(tr2Val, "98765432"))
	assert.True(t, strings.Contains(tr2Val, "000D2601123456789"))
	assert.Equal(t, "11223344", anonMap["9F26"])
}

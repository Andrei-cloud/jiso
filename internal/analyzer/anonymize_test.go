package analyzer

import (
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestAnonymizer_DeterministicAndConsistent(t *testing.T) {
	t.Parallel()

	anon := NewAnonymizer(false)
	cardA := "4000123456789012"
	cardB := "4000987654321098"

	// Calling multiple times on cardA produces the exact same masked result
	res1 := anon.AnonymizePAN(cardA)
	res2 := anon.AnonymizePAN(cardA)
	assert.Equal(t, res1, res2, "Anonymized PAN must be 100% deterministic and identical across repeated calls")

	// Calling on cardB produces a different result from cardA
	resB := anon.AnonymizePAN(cardB)
	assert.NotEqual(t, res1, resB, "Different cards must yield different masked results")

	// Consistency with Track 2 and Track 1
	tr2 := cardA + "=2612101000000"
	anonTr2 := anon.AnonymizeTrack2(tr2)
	assert.True(t, strings.HasPrefix(anonTr2, res1), "Track 2 masked PAN must match DE 2 masked PAN")

	tr1 := "%B" + cardA + "^CARDHOLDER/TEST^2612101000000"
	anonTr1 := anon.AnonymizeTrack1(tr1)
	assert.True(t, strings.HasPrefix(anonTr1, "%B"+res1), "Track 1 masked PAN must match DE 2 masked PAN")

	// Consistency with EMV tag in composite field
	emvMap := map[string]any{
		"57": tr2,
	}
	emvRes := anon.AnonymizeFieldValue(55, emvMap)
	anonEMV, ok := emvRes.(map[string]any)
	if !ok {
		t.Fatalf("AnonymizeFieldValue(55, map) = %T, want map[string]any", emvRes)
	}
	assert.Equal(t, anonTr2, anonEMV["57"], "EMV tag 57 must match Track 2 anonymized value")
}

func TestFormatProcCode(t *testing.T) {
	t.Parallel()

	assert.Equal(t, "000000", FormatProcCode("0"))
	assert.Equal(t, "000000", FormatProcCode("000000"))
	assert.Equal(t, "100000", FormatProcCode("100000"))
	assert.Equal(t, "200000", FormatProcCode("200000"))
	assert.Equal(t, "003000", FormatProcCode("3000"))
	assert.Equal(t, "011000", FormatProcCode("11000"))
	assert.Equal(t, "", FormatProcCode(""))
}

func TestAnonymizeTrack2(t *testing.T) {
	t.Parallel()

	// Track 2 with '=' separator
	tr2Equals := "9876543210987654=2601123456789"
	anon := NewAnonymizer(false)
	anonTr2 := anon.AnonymizeTrack2(tr2Equals)
	parts := strings.Split(anonTr2, "=")
	require.Len(t, parts, 2)
	assert.Equal(t, "98765432", parts[0][:8])
	assert.Equal(t, "000", parts[0][13:16])
	assert.Equal(t, "2601123456789", parts[1])

	// Track 2 with 'D' separator
	tr2D := "9876543210987654D2601123456789"
	anonTr2D := anon.AnonymizeTrack2(tr2D)
	dParts := strings.Split(anonTr2D, "D")
	require.Len(t, dParts, 2)
	assert.Equal(t, "98765432", dParts[0][:8])
	assert.Equal(t, "000", dParts[0][13:16])
}

func TestAnonymizeTrack1(t *testing.T) {
	t.Parallel()

	tr1 := "%B9876543210987654^SMITH/JOHN^260112345"
	anon := NewAnonymizer(false)
	anonTr1 := anon.AnonymizeTrack1(tr1)
	assert.True(t, strings.HasPrefix(anonTr1, "%B98765432"))
	assert.True(t, strings.Contains(anonTr1, "000^SMITH/JOHN^260112345"))
}

func TestAnonymizeFieldValueUnsecureFlag(t *testing.T) {
	t.Parallel()

	origPAN := "9876543210987654"

	// Unsecure = true -> should keep original clear PAN
	unsecRes := AnonymizeFieldValue(2, origPAN, true)
	assert.Equal(t, origPAN, unsecRes)

	// Unsecure = false (Secure mode) -> should anonymize
	secRes := AnonymizeFieldValue(2, origPAN, false)
	assert.NotEqual(t, origPAN, secRes)
	secStr, ok := secRes.(string)
	if !ok {
		t.Fatalf("AnonymizeFieldValue(2, string, false) = %T, want string", secRes)
	}
	assert.Equal(t, "98765432", secStr[:8])
	assert.Equal(t, "000", secStr[13:16])
}

func TestAnonymizeCompositeChipFields(t *testing.T) {
	t.Parallel()

	compMap := map[string]any{
		"57":   "9876543210987654D2601123456789",
		"9F26": "11223344",
	}

	compRes := AnonymizeFieldValue(55, compMap, false)
	anonMap, ok := compRes.(map[string]any)
	if !ok {
		t.Fatalf("AnonymizeFieldValue(55, map, false) = %T, want map[string]any", compRes)
	}
	tr2Val, ok := anonMap["57"].(string)
	if !ok {
		t.Fatalf("anonMap[\"57\"] = %T, want string", anonMap["57"])
	}
	assert.True(t, strings.HasPrefix(tr2Val, "98765432"))
	assert.True(t, strings.Contains(tr2Val, "000D2601123456789"))
	assert.Equal(t, "11223344", anonMap["9F26"])
}

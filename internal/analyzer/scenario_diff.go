// scenario_diff.go answers the question a mock route is built from: given the
// captured request and its captured response, which fields should the route echo
// back, and which does the response actually carry. It compares two ISO8583
// messages; it does not know anything about scenarios or routes.
package analyzer

import (
	"bytes"
	"fmt"
	"sort"
	"strings"

	json "github.com/goccy/go-json"
	"github.com/moov-io/iso8583"

	"jiso/internal/utils"
)

func extractEchoAndResponseFields(reqMsg, respMsg *iso8583.Message, unsecure bool, anon ...*Anonymizer) ([]int, map[string]any) {
	responseFields := make(map[string]any)
	if respMsg == nil {
		return nil, responseFields
	}

	var a *Anonymizer
	if len(anon) > 0 && anon[0] != nil {
		a = anon[0]
	} else {
		a = NewAnonymizer(unsecure)
	}

	echoFields, echoSet := detectEchoFields(reqMsg, respMsg)

	var respFIDs []int
	for i, f := range respMsg.GetFields() {
		if f == nil || i == 0 || i == 1 || echoSet[i] {
			continue
		}
		respFIDs = append(respFIDs, i)
	}
	sort.Ints(respFIDs)

	for _, i := range respFIDs {
		f := respMsg.GetField(i)
		if f == nil {
			continue
		}
		extracted, ok := extractFieldValueForTemplate(f)
		if !ok {
			continue
		}
		extracted = a.AnonymizeFieldValue(i, extracted)
		fieldKey := fmt.Sprintf("%d", i)

		// Four fields are values the responder generates on every run, so the
		// captured value is replaced by the keyword the composer expands; anything
		// else is carried through as captured. The echoSet guards this chain used to
		// repeat are gone: respFIDs above already dropped every echoed field, so a
		// field reaching here has not been echoed by definition.
		switch i {
		case 38:
			responseFields[fieldKey] = utils.KeywordAuthCode
		case 11:
			responseFields[fieldKey] = utils.KeywordSTAN
		case 37:
			responseFields[fieldKey] = utils.KeywordRRN
		case 7:
			responseFields[fieldKey] = utils.KeywordDateTime
		default:
			responseFields[fieldKey] = extracted
		}
	}

	if rc := getFieldString(respMsg, 39); rc != "" && !echoSet[39] {
		responseFields["39"] = rc
	}
	if f38 := respMsg.GetField(38); f38 != nil && !echoSet[38] {
		if _, has38 := responseFields["38"]; !has38 {
			responseFields["38"] = utils.KeywordAuthCode
		}
	}

	return echoFields, responseFields
}

func valuesEqual(v1, v2 any) bool {
	if v1 == nil && v2 == nil {
		return true
	}
	if v1 == nil || v2 == nil {
		return false
	}
	s1 := fmt.Sprintf("%v", v1)
	s2 := fmt.Sprintf("%v", v2)
	if s1 == s2 || strings.TrimSpace(s1) == strings.TrimSpace(s2) {
		return true
	}
	b1, err1 := json.Marshal(v1)
	b2, err2 := json.Marshal(v2)
	if err1 == nil && err2 == nil && bytes.Equal(b1, b2) {
		return true
	}
	return false
}

// detectEchoFields returns the field numbers whose captured request and response
// values are equal -- the fields a route can echo back -- sorted, plus a set of
// them for O(1) membership in the caller.
func detectEchoFields(reqMsg, respMsg *iso8583.Message) ([]int, map[int]bool) {
	echoSet := make(map[int]bool)
	var echoFields []int
	if reqMsg == nil {
		return echoFields, echoSet
	}

	for i := 2; i <= 128; i++ {
		reqF := reqMsg.GetField(i)
		respF := respMsg.GetField(i)
		if reqF == nil || respF == nil {
			continue
		}
		reqVal, ok1 := extractFieldValueForTemplate(reqF)
		respVal, ok2 := extractFieldValueForTemplate(respF)
		if !ok1 || !ok2 {
			continue
		}
		if valuesEqual(reqVal, respVal) {
			echoFields = append(echoFields, i)
			echoSet[i] = true
		}
	}
	sort.Ints(echoFields)

	return echoFields, echoSet
}

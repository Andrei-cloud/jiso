package server

import (
	"fmt"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/moov-io/iso8583"
	"github.com/moov-io/iso8583/field"

	"jiso/internal/config"
	"jiso/internal/utils"
)

// regexCache memoizes compiled route regex patterns: matchFieldValue runs
// per message per route on the serve hot path, and regexp.MatchString
// recompiled the pattern on every call. An invalid pattern caches a nil
// entry and never matches (same semantics as the old error branch).
var regexCache sync.Map // string -> *regexp.Regexp (nil = invalid pattern)

func matchCachedRegex(pattern, val string) bool {
	v, ok := regexCache.Load(pattern)
	if !ok {
		re, err := regexp.Compile(pattern)
		if err != nil {
			v, _ = regexCache.LoadOrStore(pattern, (*regexp.Regexp)(nil))
		} else {
			v, _ = regexCache.LoadOrStore(pattern, re)
		}
	}
	re, _ := v.(*regexp.Regexp)
	if re == nil {
		return false
	}
	return re.MatchString(val)
}

// Matcher evaluates incoming ISO8583 messages against mock routes
type Matcher struct {
	routes []config.MockRouteConfig
}

// NewMatcher copies the configured routes and orders them most-specific-first, so
// the first route a request matches is the one that constrains the most fields,
// and the caller's slice stays exactly as configured. Sorting once here rather
// than per request is the reason the matcher exists.
func NewMatcher(routes []config.MockRouteConfig) *Matcher {
	sortedRoutes := make([]config.MockRouteConfig, len(routes))
	copy(sortedRoutes, routes)
	sort.SliceStable(sortedRoutes, func(i, j int) bool {
		return calculateRouteSpecificity(&sortedRoutes[i]) > calculateRouteSpecificity(&sortedRoutes[j])
	})
	return &Matcher{routes: sortedRoutes}
}

func calculateRouteSpecificity(r *config.MockRouteConfig) int {
	score := len(r.MatchFields) * 10
	if _, hasPAN := r.MatchFields["2"]; hasPAN {
		score += 50 // Prioritize card-level routes over generic routes
	}
	if _, hasSTAN := r.MatchFields["11"]; hasSTAN {
		score += 40
	}
	if _, hasRRN := r.MatchFields["37"]; hasRRN {
		score += 40
	}
	if _, hasAmount := r.MatchFields["4"]; hasAmount {
		score += 30
	}
	if _, hasProcCode := r.MatchFields["3"]; hasProcCode {
		score += 20
	}
	if _, hasNM := r.MatchFields["70"]; hasNM {
		score += 20
	}
	return score
}

// MatchAndCompose matches request message against flexible mock route field criteria and composes response
func (m *Matcher) MatchAndCompose(req *iso8583.Message, spec *iso8583.MessageSpec) (*config.MockRouteConfig, *iso8583.Message, error) {
	if req == nil {
		return nil, nil, fmt.Errorf("nil request message")
	}

	mti, err := req.GetMTI()
	if err != nil {
		return nil, nil, fmt.Errorf("getting MTI: %w", err)
	}

	var matchedRoute *config.MockRouteConfig
	for i := range m.routes {
		r := &m.routes[i]
		if matchRoute(req, r) {
			matchedRoute = r
			break
		}
	}

	// Calculate latency and jitter delay
	if matchedRoute != nil {
		delay := matchedRoute.GetTotalDelay()
		if delay > 0 {
			time.Sleep(delay)
		}
	}

	resp := iso8583.NewMessage(spec)

	if matchedRoute != nil {
		respMTI := matchedRoute.ResponseMTI
		if respMTI == "" {
			respMTI = utils.ResponseMTI(mti)
		}
		resp.MTI(respMTI)

		// Validate mandatory/required fields
		missingRequired := false
		for _, reqF := range matchedRoute.RequiredFields {
			if _, exists := extractFieldValue(req, reqF); !exists {
				missingRequired = true
				break
			}
		}

		// Echo requested fields from request
		for _, fNum := range matchedRoute.EchoFields {
			echoField(resp, req, spec, fNum)
		}

		// Inject response fields (supporting auto/dynamic keywords like auth_code, stan, rrn, datetime, and composite fields)
		for fKey, fVal := range matchedRoute.ResponseFields {
			if fNum, err := strconv.Atoi(fKey); err == nil {
				_ = setResponseFieldValue(resp, spec, fNum, fVal)
			}
		}

		if missingRequired {
			// ISO Response Code "30" = Format Error / Missing Mandatory Field (Visa Standard)
			_ = resp.Field(39, "30")
		}

		return matchedRoute, resp, nil
	}

	// Catch-all fallback response if no mock route matches explicitly
	respMTI := utils.ResponseMTI(mti)
	resp.MTI(respMTI)

	// Echo standard ISO8583 fields if present
	for _, fNum := range []int{7, 11, 25, 32, 37, 41, 42, 63, 115} {
		echoField(resp, req, spec, fNum)
	}
	_ = resp.Field(39, "12") // Default response code: "12" (Invalid Transaction / Fallback)

	return nil, resp, nil
}

// matchRoute checks if an incoming request satisfies all field match conditions in a mock route config
func matchRoute(req *iso8583.Message, r *config.MockRouteConfig) bool {
	if len(r.MatchFields) == 0 {
		return false
	}

	for fieldKey, targetCondition := range r.MatchFields {
		val, exists := extractFieldValue(req, fieldKey)
		if !matchFieldValue(val, exists, targetCondition) {
			return false
		}
	}

	return true
}

// extractFieldValue retrieves field/subfield values using dot notation (e.g., "0" for MTI, "3", "34.01.C0")
func extractFieldValue(req *iso8583.Message, fieldKey string) (string, bool) {
	if fieldKey == "0" || strings.EqualFold(fieldKey, "mti") {
		mti, err := req.GetMTI()
		if err != nil || mti == "" {
			return "", false
		}
		return mti, true
	}

	parts := strings.Split(fieldKey, ".")
	topNum, err := strconv.Atoi(parts[0])
	if err != nil {
		return "", false
	}

	f := req.GetField(topNum)
	if f == nil {
		return "", false
	}

	// Drill into subfields if requested
	for i := 1; i < len(parts); i++ {
		subID := parts[i]
		composite, ok := f.(*field.Composite)
		if !ok || composite == nil {
			return "", false
		}

		subFields := composite.GetSubfields()
		if len(subFields) == 0 {
			return "", false
		}

		// Look up subfield matching subID
		var matchedSub field.Field
		if s, ok := subFields[subID]; ok {
			matchedSub = s
		}

		if matchedSub == nil {
			return "", false
		}
		f = matchedSub
	}

	val, err := f.String()
	if err != nil {
		return "", false
	}
	return val, true
}

// matchFieldValue evaluates expected condition against extracted field value
// matchFieldValue evaluates expected condition against extracted field value
func matchFieldValue(val string, exists bool, condition any) bool {
	switch c := condition.(type) {
	case string:
		return exists && matchString(val, c)
	case float64:
		return exists && matchFloat64(val, c)
	case int:
		return exists && matchInt(val, c)
	case int64:
		return exists && matchInt64(val, c)
	case bool:
		return exists == c
	case []any:
		return exists && matchAny(val, exists, c)
	case []string:
		return exists && matchAny(val, exists, toAny(c))
	case []int:
		return exists && matchAny(val, exists, toAny(c))
	case []int64:
		return exists && matchAny(val, exists, toAny(c))
	case []float64:
		return exists && matchAny(val, exists, toAny(c))
	case map[string]any:
		return matchRule(val, exists, c)
	}

	return false
}

// matchString compares a field value to an expected string: exact, whitespace-
// trimmed, or numerically equal despite leading-zero differences.
func matchString(val, expected string) bool {
	if val == expected || strings.TrimSpace(val) == strings.TrimSpace(expected) {
		return true
	}
	numVal, err1 := strconv.ParseInt(strings.TrimSpace(val), 10, 64)
	numC, err2 := strconv.ParseInt(strings.TrimSpace(expected), 10, 64)

	return err1 == nil && err2 == nil && numVal == numC
}

// matchFloat64 compares a field value to an expected float, exact or numerically.
func matchFloat64(val string, expected float64) bool {
	if val == fmt.Sprintf("%.0f", expected) || val == strconv.FormatFloat(expected, 'f', 0, 64) {
		return true
	}
	numVal, err := strconv.ParseInt(strings.TrimSpace(val), 10, 64)

	return err == nil && numVal == int64(expected)
}

// matchInt compares a field value to an expected int, exact or numerically.
func matchInt(val string, expected int) bool {
	if val == strconv.Itoa(expected) {
		return true
	}
	numVal, err := strconv.ParseInt(strings.TrimSpace(val), 10, 64)

	return err == nil && numVal == int64(expected)
}

// matchInt64 compares a field value to an expected int64, exact or numerically.
func matchInt64(val string, expected int64) bool {
	if val == strconv.FormatInt(expected, 10) {
		return true
	}
	numVal, err := strconv.ParseInt(strings.TrimSpace(val), 10, 64)

	return err == nil && numVal == expected
}

// matchAny reports whether any element of a condition list matches.
func matchAny(val string, exists bool, items []any) bool {
	for _, item := range items {
		if matchFieldValue(val, exists, item) {
			return true
		}
	}

	return false
}

// toAny lifts a typed slice into []any for the shared element matcher.
func toAny[T any](items []T) []any {
	out := make([]any, len(items))
	for i, item := range items {
		out[i] = item
	}

	return out
}

// matchRule evaluates an advanced rule object ({"equals", "regex", "exists",
// "prefix", "suffix", "in", "one_of", "not_in"}): every present clause holds.
func matchRule(val string, exists bool, c map[string]any) bool {
	if existCond, ok := c["exists"].(bool); ok && exists != existCond {
		return false
	}
	if !exists {
		return false
	}
	if eqCond, ok := c["equals"].(string); ok && val != eqCond {
		return false
	}
	if inCond, ok := c["in"]; ok && !matchFieldValue(val, exists, inCond) {
		return false
	}
	if oneOfCond, ok := c["one_of"]; ok && !matchFieldValue(val, exists, oneOfCond) {
		return false
	}
	if notInCond, ok := c["not_in"]; ok && matchFieldValue(val, exists, notInCond) {
		return false
	}
	if rxCond, ok := c["regex"].(string); ok && !matchCachedRegex(rxCond, val) {
		return false
	}
	if preCond, ok := c["prefix"].(string); ok && !strings.HasPrefix(val, preCond) {
		return false
	}
	if sufCond, ok := c["suffix"].(string); ok && !strings.HasSuffix(val, sufCond) {
		return false
	}

	return true
}

func setResponseFieldValue(msg *iso8583.Message, spec *iso8583.MessageSpec, fieldID int, value any) error {
	switch v := value.(type) {
	case string:
		cleanVal := strings.TrimSpace(strings.ToLower(v))
		switch cleanVal {
		case utils.KeywordAuthCode, "$auth_code", "gen_auth_code":
			return msg.Field(fieldID, utils.RandString(6))
		case "stan", "$stan", "gen_stan":
			return msg.Field(fieldID, utils.GetCounter().GetStan())
		case "rrn", "$rrn", "gen_rrn":
			return msg.Field(fieldID, utils.GetRRNInstance().GetRRN())
		case "datetime", "$datetime":
			return msg.Field(fieldID, utils.GetTrxnDateTime())
		default:
			return msg.Field(fieldID, v)
		}
	case map[string]any:
		return utils.SetCompositeFieldValue(msg, spec, fieldID, v)
	default:
		return msg.Field(fieldID, fmt.Sprintf("%v", v))
	}
}

// echoField copies one requested field into the response. A composite that
// yields a map is echoed structurally; anything else is echoed as its string
// form. The guards keep that composite/string fork flat rather than nested.
func echoField(resp, req *iso8583.Message, spec *iso8583.MessageSpec, fNum int) {
	reqField := req.GetField(fNum)
	if reqField == nil {
		return
	}
	if composite, ok := reqField.(*field.Composite); ok && composite != nil {
		var fieldSpec *field.Spec
		if spec != nil && spec.Fields != nil && spec.Fields[fNum] != nil {
			fieldSpec = spec.Fields[fNum].Spec()
		}
		if data, ok := utils.ExtractFieldData(composite, fieldSpec); ok {
			if dataMap, isMap := data.(map[string]any); isMap {
				_ = utils.SetCompositeFieldValue(resp, spec, fNum, dataMap)

				return
			}
		}
	}
	if val, err := reqField.String(); err == nil {
		_ = resp.Field(fNum, val)
	}
}

package server

import (
	"fmt"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/moov-io/iso8583"
	"github.com/moov-io/iso8583/field"

	"jiso/internal/config"
	"jiso/internal/utils"
)

// Matcher evaluates incoming ISO8583 messages against mock routes
type Matcher struct {
	routes []config.MockRouteConfig
}

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
			if reqField := req.GetField(fNum); reqField != nil {
				if composite, ok := reqField.(*field.Composite); ok && composite != nil {
					var fieldSpec *field.Spec
					if spec != nil && spec.Fields != nil && spec.Fields[fNum] != nil {
						fieldSpec = spec.Fields[fNum].Spec()
					}
					if data, ok := utils.ExtractFieldData(composite, fieldSpec); ok {
						if dataMap, isMap := data.(map[string]interface{}); isMap {
							_ = utils.SetCompositeFieldValue(resp, spec, fNum, dataMap)
							continue
						}
					}
				}
				if val, err := reqField.String(); err == nil {
					_ = resp.Field(fNum, val)
				}
			}
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
		if reqField := req.GetField(fNum); reqField != nil {
			if composite, ok := reqField.(*field.Composite); ok && composite != nil {
				var fieldSpec *field.Spec
				if spec != nil && spec.Fields != nil && spec.Fields[fNum] != nil {
					fieldSpec = spec.Fields[fNum].Spec()
				}
				if data, ok := utils.ExtractFieldData(composite, fieldSpec); ok {
					if dataMap, isMap := data.(map[string]interface{}); isMap {
						_ = utils.SetCompositeFieldValue(resp, spec, fNum, dataMap)
						continue
					}
				}
			}
			if val, err := reqField.String(); err == nil {
				_ = resp.Field(fNum, val)
			}
		}
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
func matchFieldValue(val string, exists bool, condition interface{}) bool {
	switch c := condition.(type) {
	case string:
		if !exists {
			return false
		}
		if val == c || strings.TrimSpace(val) == strings.TrimSpace(c) {
			return true
		}
		// Match numeric values with leading zero differences (e.g. "0" vs "000000" or "100" vs "0100")
		if numVal, err1 := strconv.ParseInt(strings.TrimSpace(val), 10, 64); err1 == nil {
			if numC, err2 := strconv.ParseInt(strings.TrimSpace(c), 10, 64); err2 == nil {
				return numVal == numC
			}
		}
		return false
	case float64:
		if !exists {
			return false
		}
		if val == fmt.Sprintf("%.0f", c) || val == strconv.FormatFloat(c, 'f', 0, 64) {
			return true
		}
		if numVal, err := strconv.ParseInt(strings.TrimSpace(val), 10, 64); err == nil {
			return numVal == int64(c)
		}
		return false
	case int:
		if !exists {
			return false
		}
		if val == strconv.Itoa(c) {
			return true
		}
		if numVal, err := strconv.ParseInt(strings.TrimSpace(val), 10, 64); err == nil {
			return numVal == int64(c)
		}
		return false
	case int64:
		if !exists {
			return false
		}
		if val == strconv.FormatInt(c, 10) {
			return true
		}
		if numVal, err := strconv.ParseInt(strings.TrimSpace(val), 10, 64); err == nil {
			return numVal == c
		}
		return false
	case bool:
		return exists == c
	case []interface{}:
		if !exists {
			return false
		}
		for _, item := range c {
			if matchFieldValue(val, exists, item) {
				return true
			}
		}
		return false
	case []string:
		if !exists {
			return false
		}
		for _, item := range c {
			if matchFieldValue(val, exists, item) {
				return true
			}
		}
		return false
	case []int:
		if !exists {
			return false
		}
		for _, item := range c {
			if matchFieldValue(val, exists, item) {
				return true
			}
		}
		return false
	case []int64:
		if !exists {
			return false
		}
		for _, item := range c {
			if matchFieldValue(val, exists, item) {
				return true
			}
		}
		return false
	case []float64:
		if !exists {
			return false
		}
		for _, item := range c {
			if matchFieldValue(val, exists, item) {
				return true
			}
		}
		return false
	case map[string]interface{}:
		// Advanced matching object with rules like {"equals": "...", "regex": "...", "exists": true, "prefix": "...", "in": [...], "not_in": [...]}
		if existCond, ok := c["exists"].(bool); ok {
			if exists != existCond {
				return false
			}
		}
		if !exists {
			return false
		}

		if eqCond, ok := c["equals"].(string); ok && val != eqCond {
			return false
		}
		if inCond, ok := c["in"]; ok {
			if !matchFieldValue(val, exists, inCond) {
				return false
			}
		}
		if oneOfCond, ok := c["one_of"]; ok {
			if !matchFieldValue(val, exists, oneOfCond) {
				return false
			}
		}
		if notInCond, ok := c["not_in"]; ok {
			if matchFieldValue(val, exists, notInCond) {
				return false
			}
		}
		if rxCond, ok := c["regex"].(string); ok {
			matched, err := regexp.MatchString(rxCond, val)
			if err != nil || !matched {
				return false
			}
		}
		if preCond, ok := c["prefix"].(string); ok && !strings.HasPrefix(val, preCond) {
			return false
		}
		if sufCond, ok := c["suffix"].(string); ok && !strings.HasSuffix(val, sufCond) {
			return false
		}
		return true
	default:
		return false
	}
}

func setResponseFieldValue(msg *iso8583.Message, spec *iso8583.MessageSpec, fieldID int, value interface{}) error {
	switch v := value.(type) {
	case string:
		cleanVal := strings.TrimSpace(strings.ToLower(v))
		switch cleanVal {
		case "auth_code", "$auth_code", "gen_auth_code":
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
	case map[string]interface{}:
		return utils.SetCompositeFieldValue(msg, spec, fieldID, v)
	default:
		return msg.Field(fieldID, fmt.Sprintf("%v", v))
	}
}

package server

import (
	"fmt"
	"regexp"
	"strconv"
	"strings"
	"time"

	"jiso/internal/config"
	"jiso/internal/utils"

	"github.com/moov-io/iso8583"
	"github.com/moov-io/iso8583/field"
)

// Matcher evaluates incoming ISO8583 messages against mock routes
type Matcher struct {
	routes []config.MockRouteConfig
}

func NewMatcher(routes []config.MockRouteConfig) *Matcher {
	return &Matcher{routes: routes}
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
				if val, err := reqField.String(); err == nil {
					resp.Field(fNum, val)
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
			resp.Field(39, "30")
		}

		return matchedRoute, resp, nil
	}

	// Catch-all fallback response if no mock route matches explicitly
	respMTI := utils.ResponseMTI(mti)
	resp.MTI(respMTI)

	// Echo standard ISO8583 fields if present
	for _, fNum := range []int{7, 11, 25, 32, 37, 41, 42, 63, 115} {
		if reqField := req.GetField(fNum); reqField != nil {
			if val, err := reqField.String(); err == nil {
				resp.Field(fNum, val)
			}
		}
	}
	resp.Field(39, "12") // Default response code: "12" (Invalid Transaction / Fallback)

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
		return exists && val == c
	case float64:
		return exists && val == fmt.Sprintf("%.0f", c)
	case int:
		return exists && val == strconv.Itoa(c)
	case bool:
		return exists == c
	case map[string]interface{}:
		// Advanced matching object with rules like {"equals": "...", "regex": "...", "exists": true, "prefix": "..."}
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


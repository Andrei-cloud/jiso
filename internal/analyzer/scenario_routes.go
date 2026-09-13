package analyzer

import (
	"fmt"
	"sort"

	json "github.com/goccy/go-json"
	"github.com/moov-io/iso8583"

	"jiso/internal/config"
	"jiso/internal/utils"
)

// scenario_routes.go is the second half of scaffolding: the response behaviours
// collected while walking the captured pairs, grouped by request shape and turned
// into mock routes. It used to be the tail of Build, whose accumulator types were
// declared inside that function -- which is exactly why the whole thing lived in one
// 530-line function: no other function could name those types.

// mockRouteAccumulator is one distinct response behaviour seen for a request shape:
// what to match, what to echo back, the cards it was seen with, and how often it
// happened.
type mockRouteAccumulator struct {
	baseMatchFields map[string]any
	echoFields      []int
	responseMTI     string
	responseFields  map[string]any
	reqMTI          string
	reqDE3          string
	respDE39        string
	cards           []string
	seenCards       map[string]bool
	count           int
}

// routeGroup is the set of behaviours that answer the same request shape.
type routeGroup struct {
	groupKey     string
	reqMTI       string
	reqDE3       string
	responseMTI  string
	accumulators []*mockRouteAccumulator
}

// copyMatchFields copies an accumulator's base match fields. The copy is the point:
// every accumulator in a group shares one base map, so writing a route's card filter
// into it would leak that filter into every other route built from the group.
func copyMatchFields(acc *mockRouteAccumulator) map[string]any {
	mf := make(map[string]any)
	for k, v := range acc.baseMatchFields {
		mf[k] = v
	}

	return mf
}

// appendPrimaryRoutes turns each primary route group into mock routes. A group with
// one behaviour becomes a clean generic route, matched on the request fields only; a
// group with several is sorted so a "00" response comes first and the most-seen
// behaviour follows, because the first matching route wins at runtime.
func appendPrimaryRoutes(result *ScenarioScaffoldResult, order []string, groups map[string]*routeGroup) {
	for _, gKey := range order {
		grp := groups[gKey]
		if grp == nil || len(grp.accumulators) == 0 {
			continue
		}

		if len(grp.accumulators) == 1 {
			// Single behavior -> clean generic route (no card filter needed)
			acc := grp.accumulators[0]
			name := fmt.Sprintf("Mock Route %s DE3=%s", acc.responseMTI, acc.reqDE3)
			if acc.respDE39 != "" && acc.respDE39 != "00" {
				name = fmt.Sprintf("Mock Route %s DE3=%s RC=%s", acc.responseMTI, acc.reqDE3, acc.respDE39)
			}
			desc := fmt.Sprintf("Auto-generated mock route for response flow %s DE3 %s", acc.responseMTI, acc.reqDE3)
			result.MockRoutes = append(result.MockRoutes, mockRouteItem(name, desc, copyMatchFields(acc), acc))

			continue
		}

		// Multiple behaviors -> Sort accumulators: "00" first, then by frequency
		sort.SliceStable(grp.accumulators, func(i, j int) bool {
			if grp.accumulators[i].respDE39 == "00" && grp.accumulators[j].respDE39 != "00" {
				return true
			}
			if grp.accumulators[i].respDE39 != "00" && grp.accumulators[j].respDE39 == "00" {
				return false
			}

			return grp.accumulators[i].count > grp.accumulators[j].count
		})

		for idx, acc := range grp.accumulators {
			mf := copyMatchFields(acc)
			addCards(mf, acc)

			var name string
			if acc.respDE39 != "" {
				name = fmt.Sprintf("Mock Route %s DE3=%s RC=%s #%d", acc.responseMTI, acc.reqDE3, acc.respDE39, idx+1)
			} else {
				name = fmt.Sprintf("Mock Route %s DE3=%s #%d", acc.responseMTI, acc.reqDE3, idx+1)
			}
			desc := fmt.Sprintf("Auto-generated mock route for response flow %s DE3 %s (RC: %s)", acc.responseMTI, acc.reqDE3, acc.respDE39)
			result.MockRoutes = append(result.MockRoutes, mockRouteItem(name, desc, mf, acc))
		}
	}
}

// appendReversalRoutes is the same for the reversal (0400/0410) groups, named by the
// reversal request MTI rather than the response MTI, since that is what an operator
// recognises in the route list.
func appendReversalRoutes(result *ScenarioScaffoldResult, order []string, groups map[string]*routeGroup) {
	for _, gKey := range order {
		grp := groups[gKey]
		if grp == nil || len(grp.accumulators) == 0 {
			continue
		}

		if len(grp.accumulators) == 1 {
			acc := grp.accumulators[0]
			revReqMTI := fmt.Sprintf("%v", acc.baseMatchFields["0"])
			name := fmt.Sprintf("Mock Reversal Route %s DE3=%s", revReqMTI, acc.reqDE3)
			desc := fmt.Sprintf("Auto-generated mock route for reversal flow MTI %s DE3 %s", revReqMTI, acc.reqDE3)
			result.MockRoutes = append(result.MockRoutes, mockRouteItem(name, desc, copyMatchFields(acc), acc))

			continue
		}

		for idx, acc := range grp.accumulators {
			mf := copyMatchFields(acc)
			addCards(mf, acc)

			revReqMTI := fmt.Sprintf("%v", acc.baseMatchFields["0"])
			name := fmt.Sprintf("Mock Reversal Route %s DE3=%s #%d", revReqMTI, acc.reqDE3, idx+1)
			desc := fmt.Sprintf("Auto-generated mock route for reversal flow MTI %s DE3 %s", revReqMTI, acc.reqDE3)
			result.MockRoutes = append(result.MockRoutes, mockRouteItem(name, desc, mf, acc))
		}
	}
}

// mockRouteItem assembles one mock-route config item from an accumulator's
// response shape.
func mockRouteItem(name, description string, mf map[string]any, acc *mockRouteAccumulator) config.Item {
	return config.Item{
		Type:           config.TypeMockRoute,
		Name:           name,
		Description:    description,
		MatchFields:    mf,
		EchoFields:     acc.echoFields,
		ResponseMTI:    acc.responseMTI,
		ResponseFields: acc.responseFields,
		LatencyMs:      10,
		JitterMs:       5,
	}
}

// addCards narrows a route's match to the accumulator's observed card(s): a lone
// card as the DE 2 match, several as a set.
func addCards(mf map[string]any, acc *mockRouteAccumulator) {
	if len(acc.cards) == 1 {
		mf["2"] = acc.cards[0]
	} else if len(acc.cards) > 1 {
		mf["2"] = acc.cards
	}
}

// recordRoute accumulates one observed response under its shape group. It
// reuses an accumulator whose (match, response MTI, response fields, echo
// fields) signature equals this one's -- so N identical responses collapse into
// a single route -- creating the group and accumulator on first sight, and
// recording each distinct card value once. It returns the (possibly extended)
// group order so callers keep deterministic primary/reversal emission order.
func recordRoute(
	groupMap map[string]*routeGroup,
	groupOrder []string,
	reqMTI, reqDE3, responseMTI string,
	baseMatch map[string]any,
	echoFields []int,
	responseFields map[string]any,
	respDE39, cardVal string,
) []string {
	sigBytes, _ := json.Marshal([]any{baseMatch, responseMTI, responseFields, echoFields})
	sig := string(sigBytes)

	groupKey := fmt.Sprintf("%s_%s", reqMTI, reqDE3)
	grp, exists := groupMap[groupKey]
	if !exists {
		grp = &routeGroup{
			groupKey:    groupKey,
			reqMTI:      reqMTI,
			reqDE3:      reqDE3,
			responseMTI: responseMTI,
		}
		groupMap[groupKey] = grp
		groupOrder = append(groupOrder, groupKey)
	}

	var acc *mockRouteAccumulator
	for _, existingAcc := range grp.accumulators {
		existSigBytes, _ := json.Marshal([]any{existingAcc.baseMatchFields, existingAcc.responseMTI, existingAcc.responseFields, existingAcc.echoFields})
		if string(existSigBytes) == sig {
			acc = existingAcc
			break
		}
	}

	if acc == nil {
		acc = &mockRouteAccumulator{
			baseMatchFields: baseMatch,
			echoFields:      echoFields,
			responseMTI:     responseMTI,
			responseFields:  responseFields,
			reqMTI:          reqMTI,
			reqDE3:          reqDE3,
			respDE39:        respDE39,
			cards:           make([]string, 0),
			seenCards:       make(map[string]bool),
			count:           0,
		}
		grp.accumulators = append(grp.accumulators, acc)
	}

	acc.count++
	if cardVal != "" && !acc.seenCards[cardVal] {
		acc.seenCards[cardVal] = true
		acc.cards = append(acc.cards, cardVal)
	}

	return groupOrder
}

// accumulatePrimaryRoute derives the primary response's shape (match fields,
// response MTI, response/echo fields, card value) from a request/response pair
// and folds it into the primary route groups via recordRoute.
func (sb *ScenarioBuilder) accumulatePrimaryRoute(
	reqMsg, respMsg *iso8583.Message,
	anon *Anonymizer,
	unsecure bool,
	reqMTI, reqDE3 string,
	groupMap map[string]*routeGroup,
	groupOrder []string,
) []string {
	reqDE2 := getFieldString(reqMsg, 2)

	respMTI, _ := respMsg.GetMTI()
	if respMTI == "" {
		respMTI = utils.ResponseMTI(reqMTI)
	}
	respDE39 := getFieldString(respMsg, 39)
	if respDE39 == "" {
		respDE39 = "00"
	}

	baseMatch := map[string]any{"0": reqMTI}
	if reqDE3 != "" {
		baseMatch["3"] = reqDE3
	}
	if reqDE70 := getFieldString(reqMsg, 70); reqDE70 != "" {
		baseMatch["70"] = reqDE70
	}
	if reqDE22 := getFieldString(reqMsg, 22); reqDE22 != "" {
		baseMatch["22"] = reqDE22
	}
	if reqDE25 := getFieldString(reqMsg, 25); reqDE25 != "" {
		baseMatch["25"] = reqDE25
	}

	echoFields, responseFields := extractEchoAndResponseFields(reqMsg, respMsg, unsecure, anon)

	cardVal := ""
	if reqDE2 != "" {
		cardVal = anon.AnonymizePAN(reqDE2)
	}

	return recordRoute(groupMap, groupOrder, reqMTI, reqDE3, respMTI, baseMatch, echoFields, responseFields, respDE39, cardVal)
}

// accumulateReversalRoute derives a reversal's response shape from a correlated
// pair -- falling back to the request's DE3/DE2 and a default echo set when the
// capture carries no reversal response -- and folds it into the reversal route
// groups via recordRoute.
func (sb *ScenarioBuilder) accumulateReversalRoute(
	pair *CorrelatedPair,
	anon *Anonymizer,
	unsecure bool,
	groupMap map[string]*routeGroup,
	groupOrder []string,
) []string {
	revReqMTI := "0400"
	if pair.Reversal != nil && pair.Reversal.Message != nil {
		if mti, _ := pair.Reversal.Message.GetMTI(); mti != "" {
			revReqMTI = mti
		}
	}
	revRespMTI := utils.ResponseMTI(revReqMTI)
	if revRespMTI == "" {
		revRespMTI = "0410"
	}

	revDE3 := ""
	revDE2 := ""
	if pair.Reversal != nil && pair.Reversal.Message != nil {
		revDE3 = FormatProcCode(getFieldString(pair.Reversal.Message, 3))
		revDE2 = getFieldString(pair.Reversal.Message, 2)
	}
	if revDE3 == "" && pair.Request != nil && pair.Request.Message != nil {
		revDE3 = FormatProcCode(getFieldString(pair.Request.Message, 3))
	}
	if revDE2 == "" && pair.Request != nil && pair.Request.Message != nil {
		revDE2 = getFieldString(pair.Request.Message, 2)
	}
	if revDE3 == "" {
		revDE3 = "000000"
	}

	baseMatch := map[string]any{"0": revReqMTI}
	if revDE3 != "" {
		baseMatch["3"] = revDE3
	}

	shape := deriveReversalShape(pair, anon, unsecure)
	if shape.mti != "" {
		revRespMTI = shape.mti
	}
	respRC := shape.rc
	echoFields := shape.echoFields
	responseFields := shape.responseFields

	cardVal := ""
	if revDE2 != "" {
		cardVal = anon.AnonymizePAN(revDE2)
	}

	return recordRoute(groupMap, groupOrder, revReqMTI, revDE3, revRespMTI, baseMatch, echoFields, responseFields, respRC, cardVal)
}

// reversalResponseShape is the reversal response's observable shape: the MTI and
// response code captured from it (empty MTI when none was captured) and the
// echo/response fields derived from the reversal request and its response.
type reversalResponseShape struct {
	mti            string
	rc             string
	echoFields     []int
	responseFields map[string]any
}

// reversalResponseShape derives the reversal response's shape from a pair,
// falling back to a default echo set and a "00" response when the capture
// carried no reversal response. The reversal request side is the captured
// reversal when present, otherwise the original request.
func deriveReversalShape(pair *CorrelatedPair, anon *Anonymizer, unsecure bool) reversalResponseShape {
	shape := reversalResponseShape{rc: "00"}
	resp := pair.ReversalResp
	if resp == nil || resp.Message == nil {
		shape.echoFields = []int{2, 3, 4, 7, 11, 14, 22, 25, 32, 33, 37, 38, 41, 42, 49, 90}
		shape.responseFields = map[string]any{"39": "00"}

		return shape
	}
	shape.mti, _ = resp.Message.GetMTI()
	if rc := getFieldString(resp.Message, 39); rc != "" {
		shape.rc = rc
	}
	revReqMsg := pair.Request.Message
	if pair.Reversal != nil && pair.Reversal.Message != nil {
		revReqMsg = pair.Reversal.Message
	}
	shape.echoFields, shape.responseFields = extractEchoAndResponseFields(revReqMsg, resp.Message, unsecure, anon)

	return shape
}

package analyzer

import (
	"fmt"
	"sort"
	"strconv"
	"strings"

	json "github.com/goccy/go-json"
	"github.com/moov-io/iso8583"
	"github.com/moov-io/iso8583/field"

	"jiso/internal/config"
	"jiso/internal/utils"
)

// scenario_routes.go is the second half of scaffolding: the response behaviours
// collected while walking the captured pairs, grouped by request shape and turned
// into mock routes. It used to be the tail of Build, whose accumulator types were
// declared inside that function -- which is exactly why the whole thing lived in one
// 530-line function: no other function could name those types.

// mockRouteAccumulator is one distinct response behaviour seen for a
// request shape: what to match, what to echo back, and how often it
// happened. It deliberately tracks NO card data: a behaviour's identity is
// its request match and response shape, and card values never enter a
// scaffold route's match (F12.3 — card-specific replay is the §J matching
// wizard's explicit business, never the scaffold's automatic one).
type mockRouteAccumulator struct {
	baseMatchFields map[string]any
	echoFields      []int
	responseMTI     string
	responseFields  map[string]any
	reqMTI          string
	reqDE3          string
	respDE39        string
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
// every accumulator in a group shares one base map, so writing any route-local
// value into it would leak that value into every other route built from the group.
func copyMatchFields(acc *mockRouteAccumulator) map[string]any {
	mf := make(map[string]any)
	for k, v := range acc.baseMatchFields {
		mf[k] = v
	}

	return mf
}

// appendPrimaryRoutes turns each primary route group into mock routes. A group with
// one behaviour becomes a clean generic route, matched on the request fields only; a
// group with several is sorted by observed frequency (an approved code wins only
// ties), because the first matching route wins at runtime and the replay should
// answer the way the capture answered most often.
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

		// Multiple behaviors -> sort by the frequency of the ANSWER.
		// Behaviour signatures can differ only in response details
		// (per-file DE62 batch ids), which makes every signature rare
		// while the outcome the steps assert was overwhelmingly one
		// code (UAT echo-backs: 83 declines against 2 approvals all
		// counted one). The code the capture showed most often answers
		// first; an approved code wins only TIES - a mock must
		// reproduce the network's dominant behaviour.
		codeSeen := make(map[string]int, len(grp.accumulators))
		for _, acc := range grp.accumulators {
			codeSeen[acc.respDE39] += acc.count
		}
		sort.SliceStable(grp.accumulators, func(i, j int) bool {
			a, b := grp.accumulators[i], grp.accumulators[j]
			if ca, cb := codeSeen[a.respDE39], codeSeen[b.respDE39]; ca != cb {
				return ca > cb
			}
			if a.count != b.count {
				return a.count > b.count
			}

			return a.respDE39 == "00" && b.respDE39 != "00"
		})

		before := len(result.MockRoutes)
		for idx, acc := range grp.accumulators {
			mf := copyMatchFields(acc)

			// The name LEADS with a zero-padded emission rank: the
			// generated-items store persists items sorted by name, so
			// any order the answer frequency decides would be lost at
			// save time (an RC=00-suffixed name sorted alphabetically
			// before RC=06 - and the first-matching route wins at
			// runtime, so the rare approval silently beat 83 declines).
			// Rank #1 answers first, and the order survives the store.
			var name string
			if acc.respDE39 != "" {
				name = fmt.Sprintf("Mock Route #%04d %s DE3=%s RC=%s", idx+1, acc.responseMTI, acc.reqDE3, acc.respDE39)
			} else {
				name = fmt.Sprintf("Mock Route #%04d %s DE3=%s", idx+1, acc.responseMTI, acc.reqDE3)
			}
			desc := fmt.Sprintf("Auto-generated mock route for response flow %s DE3 %s (RC: %s)", acc.responseMTI, acc.reqDE3, acc.respDE39)
			result.MockRoutes = append(result.MockRoutes, mockRouteItem(name, desc, mf, acc))
		}
		if warn := scaffoldSharingWarning(result.MockRoutes[before:]); warn != "" {
			result.Warnings = append(result.Warnings, warn)
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

		before := len(result.MockRoutes)
		for idx, acc := range grp.accumulators {
			mf := copyMatchFields(acc)

			revReqMTI := fmt.Sprintf("%v", acc.baseMatchFields["0"])
			// Rank leads the name (see appendPrimaryRoutes): the store
			// sorts items by name, and the first matching route wins.
			name := fmt.Sprintf("Mock Reversal Route #%04d %s DE3=%s", idx+1, revReqMTI, acc.reqDE3)
			desc := fmt.Sprintf("Auto-generated mock route for reversal flow MTI %s DE3 %s", revReqMTI, acc.reqDE3)
			result.MockRoutes = append(result.MockRoutes, mockRouteItem(name, desc, mf, acc))
		}
		if warn := scaffoldSharingWarning(result.MockRoutes[before:]); warn != "" {
			result.Warnings = append(result.Warnings, warn)
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

// noteDroppedFields folds dropped-field keys into the scaffold's honest
// warnings, naming each field once: a capture of 300 pairs must not
// repeat the same sentence 300 times.
func noteDroppedFields(result *ScenarioScaffoldResult, dropped []string) {
	for _, key := range dropped {
		w := "scaffold dropped field " + key + ": captured value is longer than the spec maximum - the message could not pack otherwise"
		dup := false
		for _, have := range result.Warnings {
			if have == w {
				dup = true

				break
			}
		}
		if !dup {
			result.Warnings = append(result.Warnings, w)
		}
	}
}

// dropUnpackableFields removes from fields every string value the spec
// cannot encode: the capture may carry raw values longer than the
// field's declared maximum, and such a template fails the pack at
// scenario-run time (the UAT capture's 0400 carried a 36-char DE61
// against a spec maximum of 18 - the reversal steps could never send).
// Composer keyword values ({{...}}) expand at run time and are left
// alone; field shapes the guard cannot measure (composites, unbounded
// lengths) are kept. It returns the dropped keys sorted.
func dropUnpackableFields(spec *iso8583.MessageSpec, fields map[string]any) []string {
	var dropped []string
	for key, v := range fields {
		id, err := strconv.Atoi(key)
		if err != nil || id == 0 {
			continue
		}
		s, ok := v.(string)
		if !ok || strings.HasPrefix(s, "{{") {
			continue
		}
		if fieldExceedsSpecMax(spec, id, s) {
			delete(fields, key)
			dropped = append(dropped, key)
		}
	}
	sort.Strings(dropped)

	return dropped
}

// fieldExceedsSpecMax reports whether value cannot encode under the
// spec's definition for field id, counting what the composer actually
// packs: String/Numeric count characters; Binary values are packed as
// raw string bytes (the UAT capture's 0400 carried a 36-char DE61
// against an 18-byte Binary definition and could never pack); Hex
// values pack as their hex-decoded byte count. Composites and other
// shapes stay untouched.
func fieldExceedsSpecMax(spec *iso8583.MessageSpec, id int, value string) bool {
	if spec == nil {
		return false
	}
	fd := spec.Fields[id]
	if fd == nil || fd.Spec() == nil {
		return false
	}
	maxLen := fd.Spec().Length
	if maxLen <= 0 {
		return false
	}
	n := len(value)
	switch fd.(type) {
	case *field.String, *field.Numeric, *field.Binary:
	case *field.Hex:
		n = (n + 1) / 2
	default:
		return false
	}

	return n > maxLen
}

// scaffoldSharingWarning names the ordering fact a card-free scaffold leaves
// behind: when several routes replay different responses to one and the same
// request match, the server is first-full-match-wins, so the RC-first ordering
// above is load-bearing and a specific card's behaviour may never be reached.
// Card-specific replay is the §J matching wizard's explicit business (a PAN
// condition there is an operator's deliberate request), never the scaffold's.
func scaffoldSharingWarning(routes []config.Item) string {
	seen := make(map[string]int, len(routes))
	for _, r := range routes {
		b, err := json.Marshal(r.MatchFields)
		if err != nil {
			continue
		}
		seen[string(b)]++
	}
	for _, n := range seen {
		if n > 1 {
			return "several scaffold routes replay different responses to the same request match; the most-seen response answers first (approved wins only ties) and the first matching route wins at runtime - card-specific replay belongs to the §J matching wizard"
		}
	}

	return ""
}

// recordRoute accumulates one observed response under its shape group. It
// reuses an accumulator whose (match, response MTI, response fields, echo
// fields) signature equals this one's -- so N identical responses collapse into
// a single route -- creating the group and accumulator on first sight. It
// returns the (possibly extended) group order so callers keep deterministic
// primary/reversal emission order.
func recordRoute(
	groupMap map[string]*routeGroup,
	groupOrder []string,
	reqMTI, reqDE3, responseMTI string,
	baseMatch map[string]any,
	echoFields []int,
	responseFields map[string]any,
	respDE39 string,
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
			count:           0,
		}
		grp.accumulators = append(grp.accumulators, acc)
	}

	acc.count++

	return groupOrder
}

// accumulatePrimaryRoute derives the primary response's shape (match fields,
// response MTI, response/echo fields) from a request/response pair and folds
// it into the primary route groups via recordRoute. No card data is read:
// a scaffold route matches a request shape, never a card.
func (sb *ScenarioBuilder) accumulatePrimaryRoute(
	reqMsg, respMsg *iso8583.Message,
	anon *Anonymizer,
	unsecure bool,
	reqMTI, reqDE3 string,
	groupMap map[string]*routeGroup,
	groupOrder []string,
) []string {
	respMTI, _ := respMsg.GetMTI()
	if respMTI == "" {
		respMTI = utils.ResponseMTI(reqMTI)
	}
	respDE39 := getFieldString(respMsg, 39)
	if respDE39 == "" {
		respDE39 = "00"
	}

	// The match carries only fields the request actually sends: the
	// scaffold's own template drops an absent DE3 (admin 0302/0620
	// messages carry none), and a fabricated "3":"000000" could then
	// never match its own replayed request - every such message fell to
	// the server's RC-12 fallback instead of its route.
	baseMatch := map[string]any{"0": reqMTI}
	if reqDE3 := getFieldString(reqMsg, 3); reqDE3 != "" {
		baseMatch["3"] = FormatProcCode(reqDE3)
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

	return recordRoute(groupMap, groupOrder, reqMTI, reqDE3, respMTI, baseMatch, echoFields, responseFields, respDE39)
}

// accumulateReversalRoute derives a reversal's response shape from a correlated
// pair -- falling back to the request's DE3 and a default echo set when the
// capture carries no reversal response -- and folds it into the reversal route
// groups via recordRoute. Like the primary path it reads no card data.
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
	if pair.Reversal != nil && pair.Reversal.Message != nil {
		revDE3 = FormatProcCode(getFieldString(pair.Reversal.Message, 3))
	}
	if revDE3 == "" && pair.Request != nil && pair.Request.Message != nil {
		revDE3 = FormatProcCode(getFieldString(pair.Request.Message, 3))
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

	return recordRoute(groupMap, groupOrder, revReqMTI, revDE3, revRespMTI, baseMatch, echoFields, responseFields, respRC)
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

// matchspec.go is the operator-chosen match model for the §J routes
// wizard: which conditions a captured request/response pair must satisfy,
// and which fields' observed values split the survivors into routes.
// It mirrors server/matcher.go semantics (the written route files must
// match the same way at runtime) without importing it: the analyzer and
// the server stay independently buildable.
package analyzer

import (
	"regexp"
	"strconv"
	"strings"
	"sync"

	"github.com/moov-io/iso8583"
	"github.com/moov-io/iso8583/field"
)

// MatchCond is one operator-chosen condition on a captured pair. Side
// "req" (the empty default) tests the request; "resp" tests the paired
// response. Cond is the ladder: equals | exists | prefix | oneof | regex |
// notin. Value carries equals/exists/prefix/regex; Values carries the
// oneof/notin lists (comma-split at the editor). A resp-side cond fails
// when the pair has no response — except exists:false, which is exactly
// "the response is absent/field absent".
type MatchCond struct {
	Side   string
	Field  string
	Cond   string
	Value  string
	Values []string
}

// GroupField is one operator-chosen field whose distinct captured values
// split the matching pairs into routes. Side picks which half of the pair
// supplies the group key; only request-side group values enter a route's
// MatchFields (the server cannot know a response value before answering).
type GroupField struct {
	Field string
	Side  string
}

// MatchSpec is the whole wizard selection: conditions that must all hold,
// and the group-by fields.
type MatchSpec struct {
	Conditions []MatchCond
	GroupBy    []GroupField
}

// MatchPair is one correlated request/response the wizard evaluates.
// Response is nil for a request captured without its answer.
type MatchPair struct {
	Request  *iso8583.Message
	Response *iso8583.Message
}

// CondSideReq/CondSideResp are the two sides a condition or group field
// can read. The empty Side means req.
const (
	CondSideReq  = "req"
	CondSideResp = "resp"
)

// Matches reports whether the condition holds on the pair.
func (c MatchCond) Matches(req, resp *iso8583.Message) bool {
	msg := req
	if c.Side == CondSideResp {
		msg = resp
	}
	exists := false
	var val string
	if msg != nil {
		val, exists = condFieldValue(msg, c.Field)
	}
	switch c.Cond {
	case "exists":
		return exists != (c.Value == "false")
	case "equals":
		return exists && matchScalar(val, c.Value)
	case "prefix":
		return exists && strings.HasPrefix(val, c.Value)
	case "oneof":
		if !exists {
			return false
		}
		for _, w := range c.Values {
			if matchScalar(val, w) {
				return true
			}
		}
		return false
	case "regex":
		return exists && matchCondRegex(c.Value, val)
	case "notin":
		if !exists {
			return false
		}
		for _, w := range c.Values {
			if matchScalar(val, w) {
				return false
			}
		}
		return true
	default:
		return false
	}
}

// RenderMatchFields emits the route's match_fields for a spec and one
// group's request-side values: request-side conds only (a resp-side cond
// is a filter, never a match), each in the rule form the runtime matcher
// already parses, plus one equality per request-side group value. Response
// values never enter a match — the server cannot know a response value
// before it answers — so a response-side group leaves its routes sharing a
// match (BuildRoutesFromMatch names that in its warnings).
func RenderMatchFields(m MatchSpec, groupValues map[string]string) map[string]any {
	mf := make(map[string]any)
	for _, c := range m.Conditions {
		if c.Side == CondSideResp {
			continue
		}
		switch c.Cond {
		case "equals":
			mf[c.Field] = c.Value
		case "prefix":
			mf[c.Field] = map[string]any{"prefix": c.Value}
		case "oneof":
			mf[c.Field] = map[string]any{"in": append([]string{}, c.Values...)}
		case "regex":
			mf[c.Field] = map[string]any{"regex": c.Value}
		case "notin":
			mf[c.Field] = map[string]any{"not_in": append([]string{}, c.Values...)}
		case "exists":
			mf[c.Field] = map[string]any{"exists": c.Value != "false"}
		}
	}
	for _, g := range m.GroupBy {
		if g.Side == CondSideResp {
			continue
		}
		if v, ok := groupValues[g.Field]; ok {
			mf[g.Field] = v
		}
	}
	return mf
}

// condFieldValue resolves a field's string value with dot-path composite
// subfields ("55.1", "34.01.C0"), mirroring server.extractFieldValue:
// MTI via "0"/"mti", subfields by string id in GetSubfields.
func condFieldValue(msg *iso8583.Message, key string) (string, bool) {
	if key == "0" || strings.EqualFold(key, "mti") {
		mti, err := msg.GetMTI()
		if err != nil || mti == "" {
			return "", false
		}
		return mti, true
	}
	parts := strings.Split(key, ".")
	id, err := strconv.Atoi(parts[0])
	if err != nil {
		return "", false
	}
	f := msg.GetField(id)
	if f == nil {
		return "", false
	}
	for _, sub := range parts[1:] {
		comp, ok := f.(*field.Composite)
		if !ok || comp == nil {
			return "", false
		}
		sf := comp.GetSubfields()[sub]
		if sf == nil {
			return "", false
		}
		f = sf
	}
	s, err := f.String()
	if err != nil {
		return "", false
	}
	return s, true
}

// matchScalar is the matcher's string-condition semantics: exact,
// whitespace-trimmed equal, or numerically equal (leading zeros ignored).
func matchScalar(val, want string) bool {
	if val == want || strings.TrimSpace(val) == strings.TrimSpace(want) {
		return true
	}
	vi, verr := strconv.ParseInt(strings.TrimSpace(val), 10, 64)
	wi, werr := strconv.ParseInt(strings.TrimSpace(want), 10, 64)
	return verr == nil && werr == nil && vi == wi
}

// condRegexCache memoizes compiled patterns; an invalid pattern caches nil
// and never matches (the matcher's matchCachedRegex behaviour, so a bad
// edit can only ever mean "no route", never "wrong route").
var condRegexCache sync.Map

func matchCondRegex(pattern, val string) bool {
	if re, ok := condRegexCache.Load(pattern); ok {
		if re == nil {
			return false
		}
		return re.(*regexp.Regexp).MatchString(val)
	}
	re, err := regexp.Compile(pattern)
	if err != nil {
		condRegexCache.Store(pattern, nil)
		return false
	}
	condRegexCache.Store(pattern, re)
	return re.MatchString(val)
}

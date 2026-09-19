// matchroutes.go is the §J wizard's route builder: it filters the captured
// pairs by the operator's conditions, groups the survivors by the fields
// the operator picked, and emits one mock route per group. The condition
// model and rule rendering live in matchspec.go; this file owns only the
// grouping and the config.Item assembly.
package analyzer

import (
	"sort"
	"strconv"
	"strings"

	json "github.com/goccy/go-json"

	"jiso/internal/config"
)

// matchRoutesLatency/Jitter are the same route defaults the scaffold
// emits today (scenario_routes.go:152), so wizard and scaffold routes
// behave alike at the server.
const (
	matchRoutesLatency = 10
	matchRoutesJitter  = 5
)

// BuildRoutesFromMatch filters pairs by the spec's conditions (all must
// hold) and groups survivors by the GroupBy tuple. A request-side group
// value enters each route's match (routes stay distinct at match time); a
// response-side group value shapes only the replayed response — when that
// leaves routes sharing one match, the emission is deterministic
// (RC-first, then most-seen) and the sharing is named in warnings, because
// first-match-wins at runtime makes the order part of the answer.
// Saved match values on PAN/track fields are anonymized unless unsecure,
// so a written extract never carries live card data.
func BuildRoutesFromMatch(pairs []MatchPair, m MatchSpec, unsecure bool) ([]config.Item, []string) {
	var warnings []string
	anon := NewAnonymizer(unsecure)

	var survivors []MatchPair
	for _, p := range pairs {
		if p.Request == nil {
			continue
		}
		if matchesAll(m, p) {
			survivors = append(survivors, p)
		}
	}
	if len(survivors) == 0 {
		return nil, append(warnings, "no captured pairs match the conditions")
	}

	type routeGroup struct {
		values map[string]string // group field -> captured value (both sides)
		pairs  []MatchPair
	}
	var (
		order  []*routeGroup
		byKey  = map[string]*routeGroup{}
		groups []string
	)
	for _, p := range survivors {
		key, vals := groupKey(m, p)
		g := byKey[key]
		if g == nil {
			g = &routeGroup{values: vals}
			byKey[key] = g
			order = append(order, g)
			groups = append(groups, key)
		}
		g.pairs = append(g.pairs, p)
	}

	routes := make([]config.Item, 0, len(order))
	for idx, g := range order {
		route, warn := routeForGroup(m, g.values, g.pairs, idx, len(order), anon, unsecure)
		if warn != "" {
			warnings = append(warnings, warn)
			continue // a group with no captured response composes no route
		}
		routes = append(routes, route)
	}

	// RC-first among routes that share one match: the matcher is
	// first-full-match-wins, so the approval replay answers first.
	sort.SliceStable(routes, func(i, j int) bool {
		return routeRCHolds00(routes[i]) && !routeRCHolds00(routes[j])
	})
	if shared := sharedMatchWarning(routes); shared != "" {
		warnings = append(warnings, shared)
	}
	return routes, warnings
}

// matchesAll reports whether every condition holds on the pair.
func matchesAll(m MatchSpec, p MatchPair) bool {
	for _, c := range m.Conditions {
		if !c.Matches(p.Request, p.Response) {
			return false
		}
	}
	return true
}

// MatchPreview is the live line's cheap fold: how many pairs the
// conditions keep and how many groups the chosen fields split them into.
// It composes no routes (the run does), so the matching step can refresh
// on every keystroke without touching the capture again.
func MatchPreview(pairs []MatchPair, m MatchSpec) (surviving, groups int) {
	seen := map[string]bool{}
	for _, p := range pairs {
		if p.Request == nil || !matchesAll(m, p) {
			continue
		}
		surviving++
		key, _ := groupKey(m, p)
		seen[key] = true
	}

	return surviving, len(seen)
}

// HeadlineRequest reports the most frequent request MTI and, among the
// pairs carrying that MTI, the most frequent DE3 — the wizard's two seed
// conditions (empty strings when the scan is empty). Ties resolve to the
// first seen in capture order, so the seed is deterministic.
func HeadlineRequest(pairs []MatchPair) (mti, de3 string) {
	mtiCount := map[string]int{}
	var mtiOrder []string
	for _, p := range pairs {
		if p.Request == nil {
			continue
		}
		m, _ := condFieldValue(p.Request, "0")
		if m == "" {
			continue
		}
		if _, ok := mtiCount[m]; !ok {
			mtiOrder = append(mtiOrder, m)
		}
		mtiCount[m]++
	}
	if len(mtiOrder) == 0 {
		return "", ""
	}
	mti = bestOf(mtiOrder, mtiCount)

	de3Count := map[string]int{}
	var de3Order []string
	for _, p := range pairs {
		if p.Request == nil {
			continue
		}
		if m, _ := condFieldValue(p.Request, "0"); m != mti {
			continue
		}
		d, ok := condFieldValue(p.Request, "3")
		if !ok {
			continue
		}
		if _, seen := de3Count[d]; !seen {
			de3Order = append(de3Order, d)
		}
		de3Count[d]++
	}

	return mti, bestOf(de3Order, de3Count)
}

// bestOf picks the highest-count key, earliest seen first on ties.
func bestOf(order []string, count map[string]int) string {
	best := ""
	for _, k := range order {
		if best == "" || count[k] > count[best] {
			best = k
		}
	}

	return best
}

// groupKey builds the grouping key and the captured value map. An empty
// GroupBy collapses every survivor into one group.
func groupKey(m MatchSpec, p MatchPair) (string, map[string]string) {
	if len(m.GroupBy) == 0 {
		return "", map[string]string{}
	}
	vals := make(map[string]string, len(m.GroupBy))
	parts := make([]string, 0, len(m.GroupBy))
	for _, g := range m.GroupBy {
		msg := p.Request
		if g.Side == CondSideResp {
			msg = p.Response
		}
		v, ok := "", false
		if msg != nil {
			v, ok = condFieldValue(msg, g.Field)
		}
		if !ok {
			v = ""
		}
		vals[g.Field] = v
		side := ""
		if g.Side == CondSideResp {
			side = "r"
		}
		parts = append(parts, side+g.Field+"="+v)
	}
	return strings.Join(parts, "|"), vals
}

// routeForGroup composes one route from a group: the group's captured
// values are rendered into the match (request side), then PAN/track match
// values are anonymized so the saved extract carries no card data. The
// response shape comes from the group's first captured response; a group
// whose pairs never saw one is skipped with a warning naming that.
func routeForGroup(
	m MatchSpec,
	values map[string]string,
	pairs []MatchPair,
	idx, total int,
	anon *Anonymizer,
	unsecure bool,
) (config.Item, string) {
	var rep MatchPair
	for _, p := range pairs {
		if p.Response != nil {
			rep = p
			break
		}
	}
	if rep.Response == nil {
		return config.Item{}, "a route group had no captured response and was skipped"
	}
	respMTI, _ := rep.Response.GetMTI()

	mf := RenderMatchFields(m, values)
	if !unsecure {
		for key := range mf {
			if id, err := strconv.Atoi(key); err == nil && IsCardField(id) {
				mf[key] = anon.AnonymizeFieldValue(id, mf[key])
			}
		}
	}

	echo, respFields := extractEchoAndResponseFields(rep.Request, rep.Response, unsecure, anon)
	name := "Match Route " + respMTI
	if desc := groupDesc(values, anon, unsecure); desc != "" {
		name += " " + desc
	}
	if total > 1 {
		name += " #" + strconv.Itoa(idx+1)
	}

	return config.Item{
		Type:           config.TypeMockRoute,
		Name:           name,
		Description:    "Wizard route: " + strconv.Itoa(len(pairs)) + " captured pair(s)",
		MatchFields:    mf,
		EchoFields:     echo,
		ResponseMTI:    respMTI,
		ResponseFields: respFields,
		LatencyMs:      matchRoutesLatency,
		JitterMs:       matchRoutesJitter,
	}, ""
}

// groupDesc renders the group values for the route name; card-side values
// are anonymized for the same reason they are in the match — the name
// lands in the written file.
func groupDesc(values map[string]string, anon *Anonymizer, unsecure bool) string {
	var parts []string
	for _, g := range sortedKeys(values) {
		v := values[g]
		if !unsecure {
			if id, err := strconv.Atoi(g); err == nil && IsCardField(id) {
				v = anon.AnonymizePAN(v)
			}
		}
		parts = append(parts, g+"="+v)
	}
	if len(parts) == 0 {
		return ""
	}
	return strings.Join(parts, " ") + " "
}

func sortedKeys(m map[string]string) []string {
	keys := make([]string, 0, len(m))
	for k := range m {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	return keys
}

// routeRCHolds00 reports whether the route replays response code 00 — the
// sort puts the approval replay first among shared matches.
func routeRCHolds00(r config.Item) bool {
	return r.ResponseFields["39"] == "00"
}

// sharedMatchWarning names the sharing when several routes render the
// same match_fields JSON; empty when every match is distinct.
func sharedMatchWarning(routes []config.Item) string {
	seen := map[string]int{}
	for _, r := range routes {
		b, _ := json.Marshal(r.MatchFields)
		seen[string(b)]++
	}
	for _, n := range seen {
		if n > 1 {
			return "several routes share the same match (response-side grouping); the first matching route wins at runtime"
		}
	}
	return ""
}

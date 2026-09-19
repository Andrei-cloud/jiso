package analyzer

import (
	"testing"

	"github.com/stretchr/testify/require"

	"jiso/internal/config"
)

func matchRoutesFixturePairs(t *testing.T) []MatchPair {
	t.Helper()
	// Two requests (amounts 100/200, two cards) answered 00 and 05.
	rq1 := matchSpecMsg(t, "0200", map[int]string{2: "4111111111111111", 3: "000000", 4: "100"})
	rs1 := matchSpecMsg(t, "0210", map[int]string{39: "00"})
	rq2 := matchSpecMsg(t, "0200", map[int]string{2: "4222222222222222", 3: "000000", 4: "200"})
	rs2 := matchSpecMsg(t, "0210", map[int]string{39: "05"})
	return []MatchPair{{Request: rq1, Response: rs1}, {Request: rq2, Response: rs2}}
}

func TestBuildRoutesGroupByRequest(t *testing.T) {
	pairs := matchRoutesFixturePairs(t)
	m := MatchSpec{
		Conditions: []MatchCond{{Side: "req", Field: "0", Cond: "equals", Value: "0200"}},
		GroupBy:    []GroupField{{Field: "4", Side: "req"}},
	}
	routes, warnings := BuildRoutesFromMatch(pairs, m, false)

	require.Len(t, routes, 2, "two request amounts -> two distinct routes")
	require.Empty(t, warnings)
	byAmount := map[string]config.Item{}
	for _, r := range routes {
		require.Equal(t, config.TypeMockRoute, r.Type)
		require.Equal(t, "0200", r.MatchFields["0"])
		amt, _ := r.MatchFields["4"].(string)
		require.NotEmpty(t, amt)
		byAmount[amt] = r
	}
	require.Contains(t, byAmount, "100")
	require.Contains(t, byAmount, "200")
	// The two routes are distinct at match time.
	require.NotEqual(t, byAmount["100"].MatchFields["4"], byAmount["200"].MatchFields["4"])
}

func TestBuildRoutesGroupByResponseSharesMatch(t *testing.T) {
	pairs := matchRoutesFixturePairs(t)
	m := MatchSpec{
		Conditions: []MatchCond{{Side: "req", Field: "0", Cond: "equals", Value: "0200"}},
		GroupBy:    []GroupField{{Field: "39", Side: CondSideResp}},
	}
	routes, warnings := BuildRoutesFromMatch(pairs, m, false)

	require.Len(t, routes, 2, "two response codes -> two routes")
	// Response value must be in the replayed response, never the match.
	for _, r := range routes {
		if _, leaked := r.MatchFields["39"]; leaked {
			t.Errorf("response code leaked into match_fields: %v", r.MatchFields)
		}
		require.NotNil(t, r.ResponseFields["39"], "response must carry the code")
	}
	require.NotEmpty(t, warnings, "shared-match sharing must be named")
}

func TestBuildRoutesAnswerFrequencyOrdersSharedRoutes(t *testing.T) {
	// The capture answered 05 three times and 00 twice, each under a
	// unique response detail (per-file batch in DE48). Grouping by the
	// response code must hand the shared match to the DOMINANT answer
	// first - per-detail signature rarity must not smuggle the rare
	// approval back to the front.
	mk := func(rc, batch string) MatchPair {
		rq := matchSpecMsg(t, "0200", map[int]string{2: "4111111111111111", 3: "000000", 4: "100"})
		rs := matchSpecMsg(t, "0210", map[int]string{39: rc, 48: "BATCH-" + batch})

		return MatchPair{Request: rq, Response: rs}
	}
	pairs := []MatchPair{mk("00", "a1"), mk("05", "b1"), mk("05", "b2"), mk("05", "b3"), mk("00", "a2")}
	m := MatchSpec{
		Conditions: []MatchCond{{Side: "req", Field: "0", Cond: "equals", Value: "0200"}},
		GroupBy:    []GroupField{{Field: "39", Side: CondSideResp}},
	}
	routes, warnings := BuildRoutesFromMatch(pairs, m, false)

	require.Len(t, routes, 2)
	require.NotEmpty(t, warnings, "shared-match sharing must be named")
	require.Equal(t, "05", routes[0].ResponseFields["39"], "the dominant answer leads")
	require.Equal(t, "00", routes[1].ResponseFields["39"])
}

func TestBuildRoutesPANCondAnonymized(t *testing.T) {
	pairs := matchRoutesFixturePairs(t)
	pan := "4111111111111111"
	m := MatchSpec{Conditions: []MatchCond{{
		Side: "req", Field: "2", Cond: "equals", Value: pan,
	}}}
	routes, _ := BuildRoutesFromMatch(pairs, m, false)
	for _, r := range routes {
		got, _ := r.MatchFields["2"].(string)
		require.NotEqual(t, pan, got, "a matched PAN must be anonymized in the saved route")
		require.Len(t, got, 16)
	}

	// unsecure keeps the operator's typed value verbatim.
	insecure, _ := BuildRoutesFromMatch(pairs, m, true)
	require.Equal(t, pan, insecure[0].MatchFields["2"])
}

func TestBuildRoutesNoPairsMatch(t *testing.T) {
	pairs := matchRoutesFixturePairs(t)
	m := MatchSpec{Conditions: []MatchCond{{Side: "req", Field: "0", Cond: "equals", Value: "0300"}}}
	routes, warnings := BuildRoutesFromMatch(pairs, m, false)
	require.Nil(t, routes)
	require.NotEmpty(t, warnings)
}

func TestMatchPreviewAndSeed(t *testing.T) {
	pairs := matchRoutesFixturePairs(t)
	m := MatchSpec{Conditions: []MatchCond{{Side: "req", Field: "0", Cond: "equals", Value: "0200"}}}
	surv, groups := MatchPreview(pairs, m)
	require.Equal(t, 2, surv)
	require.Equal(t, 1, groups, "no GroupBy -> one group")

	surv, groups = MatchPreview(pairs, MatchSpec{
		Conditions: m.Conditions,
		GroupBy:    []GroupField{{Field: "4", Side: CondSideReq}},
	})
	require.Equal(t, 2, surv)
	require.Equal(t, 2, groups)

	surv, _ = MatchPreview(pairs, MatchSpec{Conditions: []MatchCond{{Side: "req", Field: "0", Cond: "equals", Value: "0300"}}})
	require.Equal(t, 0, surv)

	mti, de3 := HeadlineRequest(pairs)
	require.Equal(t, "0200", mti)
	require.Equal(t, "000000", de3)

	empty, _ := HeadlineRequest(nil)
	require.Empty(t, empty)
}

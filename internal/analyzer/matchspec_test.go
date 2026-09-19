package analyzer

import (
	"testing"

	"github.com/moov-io/iso8583"
	"github.com/stretchr/testify/require"

	"jiso/internal/utils"
)

// matchSpecMsg builds one spec-valid message for the condition predicate.
func matchSpecMsg(t *testing.T, mti string, fields map[int]string) *iso8583.Message {
	t.Helper()
	msg := iso8583.NewMessage(utils.GetDefaultSpec())
	msg.MTI(mti)
	for id, v := range fields {
		require.NoError(t, msg.Field(id, v))
	}
	return msg
}

func TestMatchCondMatches(t *testing.T) {
	req := matchSpecMsg(t, "0200", map[int]string{3: "000000", 4: "100", 37: "R1"})
	resp := matchSpecMsg(t, "0210", map[int]string{39: "05"})

	cases := []struct {
		name string
		cond MatchCond
		want bool
	}{
		{"req mti equals", MatchCond{Side: "req", Field: "0", Cond: "equals", Value: "0200"}, true},
		{"req equals numeric-tolerant", MatchCond{Side: "req", Field: "4", Cond: "equals", Value: "000000000100"}, true},
		{"req equals miss", MatchCond{Side: "req", Field: "4", Cond: "equals", Value: "200"}, false},
		{"req prefix", MatchCond{Side: "req", Field: "37", Cond: "prefix", Value: "R"}, true},
		{"req prefix miss", MatchCond{Side: "req", Field: "37", Cond: "prefix", Value: "X"}, false},
		{"req oneof", MatchCond{Side: "req", Field: "3", Cond: "oneof", Values: []string{"000000", "200000"}}, true},
		{"req regex", MatchCond{Side: "req", Field: "0", Cond: "regex", Value: "^02.0$"}, true},
		{"req notin", MatchCond{Side: "req", Field: "4", Cond: "notin", Values: []string{"999"}}, true},
		{"req notin present", MatchCond{Side: "req", Field: "4", Cond: "notin", Values: []string{"100"}}, false},
		{"req exists true", MatchCond{Side: "req", Field: "37", Cond: "exists", Value: "true"}, true},
		{"req exists false on absent", MatchCond{Side: "req", Field: "11", Cond: "exists", Value: "false"}, true},
		{"regex invalid never matches", MatchCond{Side: "req", Field: "0", Cond: "regex", Value: "[("}, false},
		{"resp side reads response", MatchCond{Side: "resp", Field: "39", Cond: "equals", Value: "05"}, true},
		{"resp exists true", MatchCond{Side: "resp", Field: "39", Cond: "exists", Value: "true"}, true},
		{"resp exists false with present resp", MatchCond{Side: "resp", Field: "39", Cond: "exists", Value: "false"}, false},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			if got := c.cond.Matches(req, resp); got != c.want {
				t.Errorf("%+v .Matches = %v, want %v", c.cond, got, c.want)
			}
		})
	}

	// A resp-side cond has nothing to read when the pair has no response:
	// any cond fails except exists:false.
	if (MatchCond{Side: "resp", Field: "39", Cond: "equals", Value: "05"}).Matches(req, nil) {
		t.Error("resp cond matched a pair with no response")
	}
	if (MatchCond{Side: "resp", Field: "39", Cond: "exists", Value: "true"}).Matches(req, nil) {
		t.Error("resp exists:true matched a pair with no response")
	}
	if !(MatchCond{Side: "resp", Field: "39", Cond: "exists", Value: "false"}).Matches(req, nil) {
		t.Error("resp exists:false must match a pair with no response")
	}
}

func TestRenderMatchFields(t *testing.T) {
	m := MatchSpec{
		Conditions: []MatchCond{
			{Side: "req", Field: "0", Cond: "equals", Value: "0200"},
			{Side: "req", Field: "22", Cond: "prefix", Value: "051"},
			{Side: "req", Field: "3", Cond: "oneof", Values: []string{"000000", "200000"}},
			{Side: "req", Field: "37", Cond: "regex", Value: "^R"},
			{Side: "req", Field: "41", Cond: "notin", Values: []string{"TERM9"}},
			{Side: "req", Field: "11", Cond: "exists", Value: "true"},
			{Side: "resp", Field: "39", Cond: "equals", Value: "05"}, // must be absent
		},
		GroupBy: []GroupField{{Field: "4", Side: "req"}, {Field: "39", Side: CondSideResp}},
	}
	got := RenderMatchFields(m, map[string]string{"4": "100"})

	// Response-side entries never enter the match.
	if _, ok := got["39"]; ok {
		t.Errorf("response group value leaked into match: %v", got["39"])
	}
	if _, ok := got["2"]; ok {
		t.Error("PAN must never be synthesized into a match")
	}
	if got["0"] != "0200" {
		t.Errorf("equals render = %#v, want scalar \"0200\"", got["0"])
	}
	if got["4"] != "100" {
		t.Errorf("request group value render = %#v, want \"100\"", got["4"])
	}
	if !ruleIs(got["22"], map[string]any{"prefix": "051"}) {
		t.Errorf("prefix render = %#v", got["22"])
	}
	if !ruleIs(got["3"], map[string]any{"in": []string{"000000", "200000"}}) {
		t.Errorf("oneof render = %#v", got["3"])
	}
	if !ruleIs(got["37"], map[string]any{"regex": "^R"}) {
		t.Errorf("regex render = %#v", got["37"])
	}
	if !ruleIs(got["41"], map[string]any{"not_in": []string{"TERM9"}}) {
		t.Errorf("notin render = %#v", got["41"])
	}
	if !ruleIs(got["11"], map[string]any{"exists": true}) {
		t.Errorf("exists render = %#v", got["11"])
	}
}

func ruleIs(got any, want map[string]any) bool {
	gm, isMap := got.(map[string]any)
	if !isMap {
		return false
	}
	for k, w := range want {
		switch wt := w.(type) {
		case string:
			if gm[k] != wt {
				return false
			}
		case []string:
			gl, ok := gm[k].([]string)
			if !ok || len(gl) != len(wt) {
				return false
			}
			for i := range wt {
				if gl[i] != wt[i] {
					return false
				}
			}
		case bool:
			if gm[k] != wt {
				return false
			}
		}
	}
	return true
}

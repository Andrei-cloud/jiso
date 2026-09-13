package analyzer

import (
	"fmt"
	"strings"

	"github.com/moov-io/iso8583"
	"github.com/moov-io/iso8583/field"

	"jiso/internal/utils"
)

// CorrelatedPair represents a matched request-response transaction and optional reversal
type CorrelatedPair struct {
	Request      *AnnotatedMessage
	Response     *AnnotatedMessage
	Reversal     *AnnotatedMessage // nil if no reversal detected
	ReversalResp *AnnotatedMessage // nil if no reversal response
	Label        string
}

// Correlator pairs request and response messages using STAN/RRN matching and DE90 for reversals
type Correlator struct {
	unsecure bool
}

// NewCorrelator creates a new Correlator instance
func NewCorrelator(unsecure ...bool) *Correlator {
	c := &Correlator{}
	if len(unsecure) > 0 {
		c.unsecure = unsecure[0]
	}
	return c
}

// Correlate matches request-response pairs and links any associated reversal transactions
// Correlate matches request-response pairs and links any associated reversal transactions
func (c *Correlator) Correlate(messages []*AnnotatedMessage) ([]*CorrelatedPair, error) {
	if len(messages) == 0 {
		return nil, fmt.Errorf("no messages to correlate")
	}

	classes := classifyMessages(messages)
	pairs, pairsByStan := c.pairRequests(classes.reqs, classes.resps)
	pairs = c.correlateReversals(classes.revReqs, classes.revResps, pairs, pairsByStan)

	return pairs, nil
}

// messageClasses buckets annotated messages by MTI into requests, responses,
// reversal requests, and reversal responses.
type messageClasses struct {
	reqs     []*AnnotatedMessage
	resps    []*AnnotatedMessage
	revReqs  []*AnnotatedMessage
	revResps []*AnnotatedMessage
}

// classifyMessages buckets each message by MTI: 04/14 MTIs are reversals, and
// each of those is further split into request and response halves.
func classifyMessages(messages []*AnnotatedMessage) messageClasses {
	var cls messageClasses
	for _, am := range messages {
		if am == nil || am.Message == nil {
			continue
		}
		mti, _ := am.Message.GetMTI()
		if mti == "" {
			continue
		}

		reversal := strings.HasPrefix(mti, "04") || strings.HasPrefix(mti, "14")
		switch {
		case reversal && utils.IsResponseMTI(mti):
			cls.revResps = append(cls.revResps, am)
		case reversal:
			cls.revReqs = append(cls.revReqs, am)
		case utils.IsResponseMTI(mti):
			cls.resps = append(cls.resps, am)
		default:
			cls.reqs = append(cls.reqs, am)
		}
	}

	return cls
}

// pairRequests matches each primary request to its response by STAN under the
// expected response MTI, falling back to RRN, and builds a correlated pair per
// request plus an STAN index of those pairs for reversal correlation.
func (c *Correlator) pairRequests(reqs, resps []*AnnotatedMessage) ([]*CorrelatedPair, map[string]*CorrelatedPair) {
	usedResps := make(map[*AnnotatedMessage]bool)
	respByStan := make(map[string][]*AnnotatedMessage, len(resps))
	respByRRN := make(map[string][]*AnnotatedMessage, len(resps))

	for _, respAM := range resps {
		respMTI, _ := respAM.Message.GetMTI()
		if stan := getFieldString(respAM.Message, 11); stan != "" {
			k := respMTI + ":" + stan
			respByStan[k] = append(respByStan[k], respAM)
		}
		if rrn := getFieldString(respAM.Message, 37); rrn != "" {
			k := respMTI + ":" + rrn
			respByRRN[k] = append(respByRRN[k], respAM)
		}
	}

	pairs := make([]*CorrelatedPair, 0, len(reqs))
	pairsByStan := make(map[string]*CorrelatedPair, len(reqs))
	for _, reqAM := range reqs {
		reqMTI, _ := reqAM.Message.GetMTI()
		reqSTAN := getFieldString(reqAM.Message, 11)
		reqRRN := getFieldString(reqAM.Message, 37)
		reqDE3 := FormatProcCode(getFieldString(reqAM.Message, 3))
		expectedRespMTI := utils.ResponseMTI(reqMTI)

		matchedResp := claimFirstUnused(respByStan[expectedRespMTI+":"+reqSTAN], usedResps)
		if matchedResp == nil {
			matchedResp = claimFirstUnused(respByRRN[expectedRespMTI+":"+reqRRN], usedResps)
		}

		label := fmt.Sprintf("Pair [%s → %s] STAN:%s DE3:%s", reqMTI, expectedRespMTI, reqSTAN, reqDE3)
		if matchedResp == nil {
			label += " (No Response Captured)"
		}
		p := &CorrelatedPair{Request: reqAM, Response: matchedResp, Label: label}
		pairs = append(pairs, p)
		if reqSTAN != "" {
			pairsByStan[reqSTAN] = p
		}
	}

	return pairs, pairsByStan
}

// correlateReversals links each reversal request to its primary pair (via the
// DE90 originals, falling back to the reversal's own STAN) and to a reversal
// response, attaching both to that pair or emitting a standalone reversal pair.
func (c *Correlator) correlateReversals(revReqs, revResps []*AnnotatedMessage, pairs []*CorrelatedPair, pairsByStan map[string]*CorrelatedPair) []*CorrelatedPair {
	usedRevResps := make(map[*AnnotatedMessage]bool)
	revRespByStan := make(map[string][]*AnnotatedMessage, len(revResps))
	for _, revRespAM := range revResps {
		mti := getFieldMTI(revRespAM.Message)
		if stan := getFieldString(revRespAM.Message, 11); stan != "" {
			k := mti + ":" + stan
			revRespByStan[k] = append(revRespByStan[k], revRespAM)
		}
	}

	for _, revReqAM := range revReqs {
		origMTI, origSTAN, _ := extractDE90Originals(revReqAM.Message)
		revSTAN := getFieldString(revReqAM.Message, 11)
		matchedPair := matchReversalPair(pairsByStan, origMTI, origSTAN, revSTAN)

		revRespMTI := utils.ResponseMTI(getFieldMTI(revReqAM.Message))
		matchedRevResp := claimFirstUnused(revRespByStan[revRespMTI+":"+revSTAN], usedRevResps)

		if matchedPair != nil {
			matchedPair.Reversal = revReqAM
			matchedPair.ReversalResp = matchedRevResp
			matchedPair.Label += " [Reversal Detected]"
		} else {
			revMTI := getFieldMTI(revReqAM.Message)
			pairs = append(pairs, &CorrelatedPair{
				Request:      revReqAM,
				Response:     matchedRevResp,
				Reversal:     revReqAM,
				ReversalResp: matchedRevResp,
				Label:        fmt.Sprintf("Standalone Reversal [%s] STAN:%s", revMTI, revSTAN),
			})
		}
	}

	return pairs
}

// matchReversalPair finds the pair a reversal belongs to: by the original STAN
// from DE90 when the original MTI agrees with the pair's request, falling back
// to the reversal's own STAN.
func matchReversalPair(pairsByStan map[string]*CorrelatedPair, origMTI, origSTAN, revSTAN string) *CorrelatedPair {
	if origSTAN != "" {
		if p, ok := pairsByStan[origSTAN]; ok && p != nil && p.Request != nil {
			pairMTI, _ := p.Request.Message.GetMTI()
			if origMTI == "" || origMTI == pairMTI {
				return p
			}
		}
	}
	if revSTAN != "" {
		if p, ok := pairsByStan[revSTAN]; ok {
			return p
		}
	}

	return nil
}

// claimFirstUnused returns the first candidate not yet claimed and marks it
// used, so a response is never matched to two requests.
func claimFirstUnused(candidates []*AnnotatedMessage, used map[*AnnotatedMessage]bool) *AnnotatedMessage {
	for _, candidate := range candidates {
		if !used[candidate] {
			used[candidate] = true

			return candidate
		}
	}

	return nil
}

func getFieldString(msg *iso8583.Message, fieldID int) string {
	if msg == nil {
		return ""
	}
	f := msg.GetField(fieldID)
	if f == nil {
		return ""
	}
	v, err := f.String()
	if err != nil {
		return ""
	}
	return strings.TrimSpace(v)
}

func getFieldMTI(msg *iso8583.Message) string {
	if msg == nil {
		return ""
	}
	mti, _ := msg.GetMTI()
	return mti
}

// extractDE90Originals extracts Original MTI, Original STAN, and Original Transmission Date & Time from DE 90
func extractDE90Originals(msg *iso8583.Message) (origMTI, origSTAN, origDateTime string) {
	if msg == nil {
		return "", "", ""
	}

	de90Field := msg.GetField(90)
	if de90Field == nil {
		return "", "", ""
	}

	// Try subfields if DE90 is a composite field
	if composite, ok := de90Field.(*field.Composite); ok && composite != nil {
		subfields := composite.GetSubfields()
		if sub1, ok := subfields["1"]; ok && sub1 != nil {
			origMTI, _ = sub1.String()
		}
		if sub2, ok := subfields["2"]; ok && sub2 != nil {
			origSTAN, _ = sub2.String()
		}
		if sub3, ok := subfields["3"]; ok && sub3 != nil {
			origDateTime, _ = sub3.String()
		}
		if origSTAN != "" {
			return origMTI, origSTAN, origDateTime
		}
	}

	// If raw string representation
	val, err := de90Field.String()
	if err == nil && len(val) >= 10 {
		if len(val) >= 4 {
			origMTI = val[:4]
		}
		if len(val) >= 10 {
			origSTAN = val[4:10]
		}
		if len(val) >= 20 {
			origDateTime = val[10:20]
		}
	}

	return origMTI, origSTAN, origDateTime
}

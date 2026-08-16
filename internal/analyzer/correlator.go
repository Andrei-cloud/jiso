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
func (c *Correlator) Correlate(messages []*AnnotatedMessage) ([]*CorrelatedPair, error) {
	if len(messages) == 0 {
		return nil, fmt.Errorf("no messages to correlate")
	}

	var reqs []*AnnotatedMessage
	var resps []*AnnotatedMessage
	var revReqs []*AnnotatedMessage
	var revResps []*AnnotatedMessage

	for _, am := range messages {
		if am == nil || am.Message == nil {
			continue
		}
		mti, _ := am.Message.GetMTI()
		if mti == "" {
			continue
		}

		if strings.HasPrefix(mti, "04") || strings.HasPrefix(mti, "14") {
			if utils.IsResponseMTI(mti) {
				revResps = append(revResps, am)
			} else {
				revReqs = append(revReqs, am)
			}
		} else if utils.IsResponseMTI(mti) {
			resps = append(resps, am)
		} else {
			reqs = append(reqs, am)
		}
	}

	usedResps := make(map[*AnnotatedMessage]bool)
	respByStan := make(map[string][]*AnnotatedMessage, len(resps))
	respByRRN := make(map[string][]*AnnotatedMessage, len(resps))

	for _, respAM := range resps {
		respMTI, _ := respAM.Message.GetMTI()
		stan := getFieldString(respAM.Message, 11)
		if stan != "" {
			k := respMTI + ":" + stan
			respByStan[k] = append(respByStan[k], respAM)
		}
		rrn := getFieldString(respAM.Message, 37)
		if rrn != "" {
			k := respMTI + ":" + rrn
			respByRRN[k] = append(respByRRN[k], respAM)
		}
	}

	var pairs []*CorrelatedPair
	pairsByStan := make(map[string]*CorrelatedPair, len(reqs))

	// Pair primary requests with responses
	for _, reqAM := range reqs {
		reqMTI, _ := reqAM.Message.GetMTI()
		reqSTAN := getFieldString(reqAM.Message, 11)
		reqRRN := getFieldString(reqAM.Message, 37)
		reqDE3 := FormatProcCode(getFieldString(reqAM.Message, 3))

		expectedRespMTI := utils.ResponseMTI(reqMTI)

		var matchedResp *AnnotatedMessage

		// 1. Match by STAN + Expected MTI via index
		if reqSTAN != "" {
			for _, candidate := range respByStan[expectedRespMTI+":"+reqSTAN] {
				if !usedResps[candidate] {
					matchedResp = candidate
					break
				}
			}
		}

		// 2. Fallback match by RRN if STAN match failed
		if matchedResp == nil && reqRRN != "" {
			for _, candidate := range respByRRN[expectedRespMTI+":"+reqRRN] {
				if !usedResps[candidate] {
					matchedResp = candidate
					break
				}
			}
		}

		if matchedResp != nil {
			usedResps[matchedResp] = true
		}

		label := fmt.Sprintf("Pair [%s → %s] STAN:%s DE3:%s", reqMTI, expectedRespMTI, reqSTAN, reqDE3)
		if matchedResp == nil {
			label += " (No Response Captured)"
		}

		p := &CorrelatedPair{
			Request:  reqAM,
			Response: matchedResp,
			Label:    label,
		}
		pairs = append(pairs, p)
		if reqSTAN != "" {
			pairsByStan[reqSTAN] = p
		}
	}

	// Reversal correlation using DE90 (Original Data Elements)
	usedRevResps := make(map[*AnnotatedMessage]bool)
	revRespByStan := make(map[string][]*AnnotatedMessage, len(revResps))
	for _, revRespAM := range revResps {
		mti := getFieldMTI(revRespAM.Message)
		stan := getFieldString(revRespAM.Message, 11)
		if stan != "" {
			k := mti + ":" + stan
			revRespByStan[k] = append(revRespByStan[k], revRespAM)
		}
	}

	for _, revReqAM := range revReqs {
		origMTI, origSTAN, _ := extractDE90Originals(revReqAM.Message)
		revSTAN := getFieldString(revReqAM.Message, 11)
		var matchedPair *CorrelatedPair

		// 1. Try matching against existing pairs using original STAN
		if origSTAN != "" {
			if p, ok := pairsByStan[origSTAN]; ok && p != nil && p.Request != nil {
				pairMTI, _ := p.Request.Message.GetMTI()
				if origMTI == "" || origMTI == pairMTI {
					matchedPair = p
				}
			}
		}

		// 2. Fallback matching using reversal's own STAN
		if matchedPair == nil && revSTAN != "" {
			if p, ok := pairsByStan[revSTAN]; ok {
				matchedPair = p
			}
		}

		// Find matching reversal response
		revRespMTI := utils.ResponseMTI(getFieldMTI(revReqAM.Message))
		var matchedRevResp *AnnotatedMessage
		if revSTAN != "" {
			for _, candidate := range revRespByStan[revRespMTI+":"+revSTAN] {
				if !usedRevResps[candidate] {
					matchedRevResp = candidate
					usedRevResps[candidate] = true
					break
				}
			}
		}

		if matchedPair != nil {
			matchedPair.Reversal = revReqAM
			matchedPair.ReversalResp = matchedRevResp
			matchedPair.Label += " [Reversal Detected]"
		} else {
			// Unmatched reversal standalone pair
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

	return pairs, nil
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
func extractDE90Originals(msg *iso8583.Message) (origMTI string, origSTAN string, origDateTime string) {
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

package server

import (
	"fmt"
	"strconv"
	"time"

	"jiso/internal/config"
	"jiso/internal/utils"

	"github.com/moov-io/iso8583"
)

// Matcher evaluates incoming ISO8583 messages against mock routes
type Matcher struct {
	routes []config.MockRouteConfig
}

func NewMatcher(routes []config.MockRouteConfig) *Matcher {
	return &Matcher{routes: routes}
}

// MatchAndCompose matches request MTI/DE3 against routes and composes response
func (m *Matcher) MatchAndCompose(req *iso8583.Message, spec *iso8583.MessageSpec) (*config.MockRouteConfig, *iso8583.Message, error) {
	if req == nil {
		return nil, nil, fmt.Errorf("nil request message")
	}

	mti, err := req.GetMTI()
	if err != nil {
		return nil, nil, fmt.Errorf("getting MTI: %w", err)
	}

	de3 := ""
	if f3 := req.GetField(3); f3 != nil {
		de3, _ = f3.String()
	}

	var matchedRoute *config.MockRouteConfig
	for i := range m.routes {
		r := &m.routes[i]
		if r.MatchMTI != "" && r.MatchMTI != mti {
			continue
		}
		if r.MatchDE3 != "" && r.MatchDE3 != de3 {
			continue
		}
		matchedRoute = r
		break
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

		// Echo requested fields from request
		for _, fNum := range matchedRoute.EchoFields {
			if reqField := req.GetField(fNum); reqField != nil {
				if val, err := reqField.String(); err == nil {
					resp.Field(fNum, val)
				}
			}
		}

		// Inject response fields
		for fKey, fVal := range matchedRoute.ResponseFields {
			if fNum, err := strconv.Atoi(fKey); err == nil {
				resp.Field(fNum, fVal)
			}
		}

		return matchedRoute, resp, nil
	}

	// Catch-all fallback response if no mock route matches explicitly
	respMTI := utils.ResponseMTI(mti)
	resp.MTI(respMTI)

	// Echo standard fields if present
	for _, fNum := range []int{7, 11, 37, 41, 49} {
		if reqField := req.GetField(fNum); reqField != nil {
			if val, err := reqField.String(); err == nil {
				resp.Field(fNum, val)
			}
		}
	}
	resp.Field(39, "12") // Default response code: "12" (Invalid Transaction / Fallback)

	return nil, resp, nil
}

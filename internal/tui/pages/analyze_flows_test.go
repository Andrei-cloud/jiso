// analyze_flows_test.go covers how the §J run step RENDERS its flow rows
// (the row identity, the per-direction inclusion marker, the peer-port
// origin), as distinct from the cursor/selection behaviour in
// analyze_test.go.
package pages

import (
	"strings"
	"testing"

	"github.com/charmbracelet/x/ansi"
)

// TestAnalyzeFlowRowPeerPort: a flow row names the peer port on the other end
// of the conversation, so the operator sees who originates from which port
// — the request half "from" the peer, the response half "to" it.
func TestAnalyzeFlowRowPeerPort(t *testing.T) {
	t.Parallel()

	st := analyzeFixtureState()
	st.Step = StepRun
	st.Status = AnalyzeStatusIdle
	st.Flows = []AnalyzeFlowRow{
		{Port: 4005, PeerPort: 52000, Direction: "dst", Msgs: 333, MTIs: "0110(209)", Selectable: true, Selected: true},
		{Port: 4005, PeerPort: 52000, Direction: "src", Msgs: 237, MTIs: "0100(113)", Selectable: true},
	}
	a := analyzePage(t, st, 130, 32)
	body := ansi.Strip(a.View().Content)
	if !strings.Contains(body, "-> dst :4005  from :52000") {
		t.Errorf("dst row must name the peer it originates from:\n%s", body)
	}
	if !strings.Contains(body, "<- src :4005  to :52000") {
		t.Errorf("src row must name the peer it sends to:\n%s", body)
	}
}

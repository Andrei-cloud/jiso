// analyzescan.go is the §J matching wizard's scan leg: it extracts and
// correlates the capture's server-side flows ONCE so the matching step can
// show "~N pairs match" and the fields whose values varied — the group-by
// suggestions. It writes nothing and reads no config beyond what the
// wizard already chose; the run itself stays a separate leg.
package app

import (
	"context"
	"fmt"
	"os"
	"sort"
	"strconv"
	"strings"

	"github.com/moov-io/iso8583"

	"jiso/internal/analyzer"
)

// AnalyzeScanOptions are the inputs of one scan: the wizard's capture,
// header and spec choices. An empty SpecPath means the engine default.
type AnalyzeScanOptions struct {
	PcapPath   string
	HeaderType string
	SpecPath   string
	Unsecure   bool
}

// FieldVariance is one field whose captured values were not constant,
// per side: the group-by pane's suggestion list. Sample carries up to two
// observed values, card fields anonymized unless unsecure.
type FieldVariance struct {
	Field    string   `json:"field"`
	Side     string   `json:"side"`
	Distinct int      `json:"distinct"`
	Sample   []string `json:"sample,omitempty"`
}

// AnalyzeScan is the scan result: the correlated pairs (shared with the
// run when nothing changed) and the variance report folded from them.
type AnalyzeScan struct {
	Pairs     []analyzer.MatchPair
	Variances []FieldVariance
	Unsecure  bool
}

// ScanForMatch pairs every dst flow of the capture (the same default flow
// set the run starts with) and reports the varying fields. An empty or
// unparseable capture fails like the other legs: a typed error naming the
// path, never an empty scan dressed up as success.
func (a *App) ScanForMatch(ctx context.Context, opts AnalyzeScanOptions) (*AnalyzeScan, error) {
	if err := checkCtx(ctx); err != nil {
		return nil, err
	}
	if _, err := os.Stat(opts.PcapPath); err != nil {
		return nil, &ConfigError{Path: opts.PcapPath, Err: fmt.Errorf("capture file not readable: %w", err)}
	}
	spec, err := resolveAnalyzeSpec(opts.SpecPath)
	if err != nil {
		return nil, err
	}
	flows, err := EnumeratePCAPFlows(opts.PcapPath)
	if err != nil {
		return nil, err
	}

	stream := analyzer.NewStreamAnalyzer(spec)
	// Correlate against a single server port, exactly as the scenario
	// scaffold does: ExtractAnnotatedMessagesFromFile annotates the whole
	// capture for one server port (the port labels request vs response, it
	// does not filter), so scanning each dst flow would count every pair
	// once per flow. The primary dst flow is the wizard's server port.
	serverPort, ok := primaryDstFlow(flows)
	if !ok {
		return nil, &ConfigError{Path: opts.PcapPath, Err: fmt.Errorf("no server-side flow found in %s", opts.PcapPath)}
	}
	annotated, err := stream.ExtractAnnotatedMessagesFromFile(opts.PcapPath, opts.HeaderType, serverPort)
	if err != nil {
		return nil, &ConfigError{Path: opts.PcapPath, Err: fmt.Errorf("scan extraction failed: %w", err)}
	}
	pairs, err := analyzer.NewCorrelator(opts.Unsecure).Correlate(annotated)
	if err != nil {
		return nil, &ConfigError{Path: opts.PcapPath, Err: fmt.Errorf("scan correlation failed: %w", err)}
	}
	scan := &AnalyzeScan{Unsecure: opts.Unsecure}
	for _, p := range pairs {
		scan.Pairs = append(scan.Pairs, analyzer.MatchPair{
			Request:  annotatedMessage(p.Request),
			Response: annotatedMessage(p.Response),
		})
	}
	if len(scan.Pairs) == 0 {
		return nil, &ConfigError{Path: opts.PcapPath, Err: fmt.Errorf("no request/response pairs found in %s", opts.PcapPath)}
	}

	scan.Variances = scanVariances(scan.Pairs, opts.Unsecure)
	return scan, nil
}

// primaryDstFlow picks the server port of the busiest dst flow (most
// packets); ok=false when the capture has no dst flow at all.
func primaryDstFlow(flows []analyzer.TrafficDirection) (uint16, bool) {
	var best *analyzer.TrafficDirection
	for i := range flows {
		f := &flows[i]
		if f.Mode != analyzer.DirectionDst {
			continue
		}
		if best == nil || f.PacketCount > best.PacketCount {
			best = f
		}
	}
	if best == nil {
		return 0, false
	}
	return best.TargetPort, true
}

// annotatedMessage unwraps one annotated message (nil-safe: a pair may
// carry only its request side).
func annotatedMessage(m *analyzer.AnnotatedMessage) *iso8583.Message {
	if m == nil {
		return nil
	}
	return m.Message
}

// scanVariances collects the non-constant fields per side. The walk order
// (pairs in capture order, fields by id) fixes the sample order, so the
// group-by list is stable across scans of the same capture.
func scanVariances(pairs []analyzer.MatchPair, unsecure bool) []FieldVariance {
	type observed struct {
		vals    map[string]bool
		samples []string
	}
	seen := map[string]*observed{}
	for side, pick := range map[string]func(analyzer.MatchPair) *iso8583.Message{
		analyzer.CondSideReq:  func(p analyzer.MatchPair) *iso8583.Message { return p.Request },
		analyzer.CondSideResp: func(p analyzer.MatchPair) *iso8583.Message { return p.Response },
	} {
		for _, p := range pairs {
			msg := pick(p)
			if msg == nil {
				continue
			}
			ids := make([]int, 0, 32)
			for id, f := range msg.GetFields() {
				if f != nil {
					ids = append(ids, id)
				}
			}
			sort.Ints(ids) // map order would make the sample list random
			for _, id := range ids {
				f := msg.GetField(id)
				val, err := f.String()
				if err != nil {
					continue
				}
				key := side + "|" + strconv.Itoa(id)
				o := seen[key]
				if o == nil {
					o = &observed{vals: map[string]bool{}}
					seen[key] = o
				}
				if !o.vals[val] {
					o.vals[val] = true
					if len(o.samples) < 2 {
						o.samples = append(o.samples, displayScanValue(id, val, unsecure))
					}
				}
			}
		}
	}

	var out []FieldVariance
	for key, o := range seen {
		if len(o.vals) < 2 {
			continue // a constant field groups nothing
		}
		side, field, ok := strings.Cut(key, "|")
		if !ok {
			continue
		}
		out = append(out, FieldVariance{
			Field: field, Side: side, Distinct: len(o.vals), Sample: o.samples,
		})
	}
	sort.Slice(out, func(i, j int) bool {
		if out[i].Side != out[j].Side {
			return out[i].Side == analyzer.CondSideReq
		}
		a, _ := strconv.Atoi(out[i].Field)
		b, _ := strconv.Atoi(out[j].Field)
		return a < b
	})
	return out
}

// displayScanValue anonymizes the PAN/track samples of a secured scan: the
// group-by list is a display surface and shows no card data.
func displayScanValue(id int, val string, unsecure bool) string {
	if unsecure || !isScanCardField(id) {
		return val
	}
	return analyzer.NewAnonymizer(false).AnonymizePAN(val)
}

func isScanCardField(id int) bool {
	return id == 2 || id == 35 || id == 45 || id == 55
}

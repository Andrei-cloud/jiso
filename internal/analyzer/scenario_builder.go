package analyzer

import (
	"fmt"

	json "github.com/goccy/go-json"
	"github.com/moov-io/iso8583"

	"jiso/internal/config"
	"jiso/internal/transactions"
	"jiso/internal/utils"
)

// ScenarioScaffoldOptions specifies parameters for scenario scaffolding
type ScenarioScaffoldOptions struct {
	ScenarioName       string
	IncludeReversals   map[int]bool // Pair index -> whether to include reversal step
	GenerateMockRoutes bool
	Unsecure           bool
}

// ScenarioScaffoldResult contains generated config items
type ScenarioScaffoldResult struct {
	Transactions []config.Item
	Datasets     []config.Item
	Scenario     config.Item
	MockRoutes   []config.Item
	// Warnings names honest facts about the scaffold the operator should
	// see — e.g. that several routes now replay different responses to
	// one shared request match, answered most-seen-first (F12.3:
	// scaffold routes never match cards, so a card's specific answer may
	// be shadowed).
	Warnings []string
}

// ScenarioBuilder constructs test scenario scaffolds from correlated request-response pairs
type ScenarioBuilder struct {
	spec           *iso8583.MessageSpec
	varianceEngine *VarianceEngine
	anonymizer     *Anonymizer
	unsecure       bool
}

// NewScenarioBuilder creates a new ScenarioBuilder instance
func NewScenarioBuilder(spec *iso8583.MessageSpec, unsecure ...bool) *ScenarioBuilder {
	unsec := false
	if len(unsecure) > 0 {
		unsec = unsecure[0]
	}
	return &ScenarioBuilder{
		spec:           spec,
		varianceEngine: NewVarianceEngine(spec, unsec),
		anonymizer:     NewAnonymizer(unsec),
		unsecure:       unsec,
	}
}

// Build generates transaction templates, datasets, scenario steps, and optional mock routes from correlated pairs
func (sb *ScenarioBuilder) Build(pairs []*CorrelatedPair, opts ScenarioScaffoldOptions) (*ScenarioScaffoldResult, error) {
	if len(pairs) == 0 {
		return nil, fmt.Errorf("no correlated pairs selected for scenario building")
	}

	scenName := scenarioNameOrDefault(opts.ScenarioName)

	anon := sb.anonymizerFor(opts.Unsecure)

	result := &ScenarioScaffoldResult{}
	var scenarioSteps []transactions.ScenarioStep

	// Route behaviours, grouped by request shape: primary and reversal are separate
	// because a reversal is a different route an operator recognises separately.
	primaryGroupMap := make(map[string]*routeGroup)
	var primaryGroupOrder []string

	reversalGroupMap := make(map[string]*routeGroup)
	var reversalGroupOrder []string

	for idx, pair := range pairs {
		if pair == nil || pair.Request == nil || pair.Request.Message == nil || pair.Response == nil || pair.Response.Message == nil {
			continue
		}

		reqMsg := pair.Request.Message
		reqMTI, _ := reqMsg.GetMTI()
		reqDE3 := procCodeOf(reqMsg, "000000")

		// 1. Build base transaction template and request step
		txFields := buildMessageTemplateFields(reqMsg, opts.Unsecure, anon)
		noteDroppedFields(result, dropUnpackableFields(sb.spec, txFields))
		txName := fmt.Sprintf("Tx %s DE3=%s #%d", reqMTI, reqDE3, idx+1)
		stepName := fmt.Sprintf("%s DE3=%s (Step #%d)", reqMTI, reqDE3, idx+1)
		includeRev := opts.IncludeReversals[idx]
		// A standalone reversal pair (its original lives outside the
		// capture; the correlator emits the 04xx as its own pair) already
		// IS the reversal: the request step sends it and its primary
		// route replays the captured 0410/0430. Attaching a second,
		// "Reversal of" step would send the same message twice and
		// double-suffix the step name.
		if pair.Reversal == pair.Request {
			includeRev = false
		}

		txItem, reqStep, err := requestScaffold(reqMTI, reqDE3, txName, stepName, txFields, pair, includeRev)
		if err != nil {
			return nil, err
		}
		result.Transactions = append(result.Transactions, txItem)
		scenarioSteps = append(scenarioSteps, reqStep)

		// 3. Build Reversal Template and Step if requested
		if includeRev {
			revTxItem, revStep, droppedRev := sb.buildReversal(pair, reqMTI, reqDE3, stepName, idx, txFields, anon)
			noteDroppedFields(result, droppedRev)
			result.Transactions = append(result.Transactions, revTxItem)
			scenarioSteps = append(scenarioSteps, revStep)
		}

		// 4. Collect Primary Mock Routes if requested
		if opts.GenerateMockRoutes && pair.Response != nil && pair.Response.Message != nil {
			primaryGroupOrder = sb.accumulatePrimaryRoute(reqMsg, pair.Response.Message, anon, opts.Unsecure, reqMTI, reqDE3, primaryGroupMap, primaryGroupOrder)
		}

		// 5. Collect Reversal Mock Routes if requested
		if opts.GenerateMockRoutes && includeRev {
			reversalGroupOrder = sb.accumulateReversalRoute(pair, anon, opts.Unsecure, reversalGroupMap, reversalGroupOrder)
		}
	}

	appendPrimaryRoutes(result, primaryGroupOrder, primaryGroupMap)
	appendReversalRoutes(result, reversalGroupOrder, reversalGroupMap)

	if err := finalizeScenario(result, scenName, scenarioSteps); err != nil {
		return nil, err
	}

	return result, nil
}

// buildReversal constructs one pair's reversal transaction template and its
// scenario step: the template copies the captured reversal message (mapping the
// responder-generated fields to composer keywords) when one was captured, and
// falls back to the request fields otherwise; the step asserts the reversal
// response code. Values the spec cannot encode are dropped (returned for the
// scaffold's warnings) rather than shipped as a template that cannot pack.
func (sb *ScenarioBuilder) buildReversal(pair *CorrelatedPair, reqMTI, reqDE3, stepName string, idx int, txFields map[string]any, anon *Anonymizer) (config.Item, transactions.ScenarioStep, []string) {
	revTxName := fmt.Sprintf("Reversal for %s DE3=%s #%d", reqMTI, reqDE3, idx+1)
	revFields := make(map[string]any)

	if pair.Reversal != nil && pair.Reversal.Message != nil {
		// Copy fields from captured reversal message
		revMsg := pair.Reversal.Message
		for i, f := range revMsg.GetFields() {
			if f == nil || i == 1 {
				continue
			}
			extracted, ok := extractFieldValueForTemplate(f)
			if !ok {
				continue
			}
			extracted = anon.AnonymizeFieldValue(i, extracted)
			fieldKey := fmt.Sprintf("%d", i)

			// The reversal template replaces the responder-generated fields with the
			// keywords the composer expands; field 90 is rebuilt from the original
			// message's identity, field 3 keeps the request's processing code, and
			// anything else is carried through as captured.
			switch i {
			case 7, 11, 37:
				revFields[fieldKey] = utils.KeywordAuto
			case 38:
				revFields[fieldKey] = "{{context.AuthId}}"
			case 90:
				revFields[fieldKey] = "{{context.OrigMTI}}{{context.OrigSTAN}}{{context.OrigDateTime}}0000000000000000000000"
			case 3:
				revFields[fieldKey] = FormatProcCode(fmt.Sprintf("%v", extracted))
			default:
				revFields[fieldKey] = extracted
			}
		}
	} else {
		// Construct base 0400 reversal template from request fields
		for k, v := range txFields {
			revFields[k] = v
		}
		revFields["0"] = "0400"
		revFields["7"] = utils.KeywordAuto
		revFields["11"] = utils.KeywordAuto
		revFields["37"] = utils.KeywordAuto
		revFields["38"] = "{{context.AuthId}}"
		revFields["90"] = "{{context.OrigMTI}}{{context.OrigSTAN}}{{context.OrigDateTime}}0000000000000000000000"
	}

	dropped := dropUnpackableFields(sb.spec, revFields)
	revFieldsBytes, _ := json.Marshal(revFields)
	revTxItem := config.Item{
		Type:        config.TypeTransaction,
		Name:        revTxName,
		Description: fmt.Sprintf("Scaffolded reversal transaction template for MTI %s", reqMTI),
		Fields:      revFieldsBytes,
	}

	revRespCode := "00"
	if pair.ReversalResp != nil && pair.ReversalResp.Message != nil {
		if rc := getFieldString(pair.ReversalResp.Message, 39); rc != "" {
			revRespCode = rc
		}
	}

	revStep := transactions.ScenarioStep{
		Name:             fmt.Sprintf("Reversal of %s (Step #%d)", stepName, idx+1),
		UseTransactionID: revTxName,
		Validate: []transactions.Assertion{
			{
				Field:  "39",
				Expect: revRespCode,
			},
		},
	}

	return revTxItem, revStep, dropped
}

// anonymizerFor returns the builder's anonymizer, rebuilding one when the
// requested unsecure mode differs from the cached instance's.
func (sb *ScenarioBuilder) anonymizerFor(unsecure bool) *Anonymizer {
	if sb.anonymizer == nil || unsecure != sb.unsecure {
		return NewAnonymizer(unsecure)
	}

	return sb.anonymizer
}

// responseCodeOf returns field 39 of the message, or def when the message is
// absent or carries no response code.
func responseCodeOf(m *iso8583.Message, def string) string {
	if m != nil {
		if rc := getFieldString(m, 39); rc != "" {
			return rc
		}
	}

	return def
}

// requestScaffold builds one pair's request transaction template and its
// scenario step. The step's Extract map -- the values a following reversal step
// reads back out of the response -- is added only when a reversal is included.
func requestScaffold(reqMTI, reqDE3, txName, stepName string, txFields map[string]any, pair *CorrelatedPair, includeRev bool) (config.Item, transactions.ScenarioStep, error) {
	txFieldsBytes, err := json.Marshal(txFields)
	if err != nil {
		return config.Item{}, transactions.ScenarioStep{}, fmt.Errorf("failed to marshal fields for '%s': %w", txName, err)
	}
	txItem := config.Item{
		Type:        config.TypeTransaction,
		Name:        txName,
		Description: fmt.Sprintf("Scaffolded transaction template for MTI %s DE3 %s", reqMTI, reqDE3),
		Fields:      txFieldsBytes,
	}
	reqStep := transactions.ScenarioStep{
		Name:             stepName,
		UseTransactionID: txName,
		Validate: []transactions.Assertion{
			{Field: "39", Expect: responseCodeOf(pair.Response.Message, "00")},
		},
	}
	if includeRev {
		reqStep.Extract = map[string]string{
			"AuthId":        "38",
			"OrigMTI":       "0",
			"OrigSTAN":      "11",
			"OrigDateTime":  "7",
			"OrigAcquirer":  "32",
			"OrigForwarder": "33",
		}
	}

	return txItem, reqStep, nil
}

// finalizeScenario marshals the collected steps into the result's scenario item.
func finalizeScenario(result *ScenarioScaffoldResult, scenName string, steps []transactions.ScenarioStep) error {
	stepsBytes, err := json.Marshal(steps)
	if err != nil {
		return fmt.Errorf("failed to marshal scenario steps: %w", err)
	}
	result.Scenario = config.Item{
		Type:        config.TypeScenario,
		Name:        scenName,
		Description: fmt.Sprintf("Scaffolded test scenario containing %d step(s) extracted from PCAP", len(steps)),
		Steps:       stepsBytes,
	}

	return nil
}

// scenarioNameOrDefault falls back to the default scaffold name when the caller
// supplied none.
func scenarioNameOrDefault(name string) string {
	if name == "" {
		return "Scaffolded PCAP Test Scenario"
	}

	return name
}

// procCodeOf returns the message's formatted DE3 processing code, or def when
// the message carries none.
func procCodeOf(m *iso8583.Message, def string) string {
	if pc := FormatProcCode(getFieldString(m, 3)); pc != "" {
		return pc
	}

	return def
}

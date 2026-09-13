package app

import "testing"

// Compile-time references asserting the APP-203 result vocabulary is
// exported with exactly these go doc names. Renaming or unexporting any
// of them breaks this file's compilation.
var (
	_ ScenarioReport
	_ ScenarioStepView
	_ ScenarioValidationView
	_ StressSummary
	_ ServerStats
	_ DbStatsView
	_ DbSessionView
	_ DbSessionStats
	_ DbStressRunView
	_ DbTransactionView
	_ DbTransactionRetrospective
	_ DbMessageReconstruction
	_ AnalyzeOutput
	_ AnalyzeFlowView
	_ AnalyzePairView
	_ InfoView

	_ = NewScenarioReport

	_ = NewServerStatsFromServerStats

	_ = NewDbStatsViewFromTransaction
	_ = NewDbSessionViewFromRecord
	_ = NewDbSessionStatsFromStatsMap

	_ = NewDbTransactionViewFromRecord
	_ = NewDbMessageReconstruction
	_ = NewAnalyzeOutputFromAnalysis
	_ = NewAnalyzeOutputFromScenarioScaffold
	_ = NewInfoView
	_ = NewInfoViewFromComposedMessage
)

// TestResultVocabularyIsGoDocVisible re-checks the constructors are non-nil
// function values; the var block above already proves name visibility.
func TestResultVocabularyIsGoDocVisible(t *testing.T) {
	t.Parallel()

	constructors := map[string]any{
		"NewScenarioReport":                    NewScenarioReport,
		"NewServerStatsFromServerStats":        NewServerStatsFromServerStats,
		"NewDbStatsViewFromTransaction":        NewDbStatsViewFromTransaction,
		"NewDbSessionViewFromRecord":           NewDbSessionViewFromRecord,
		"NewDbSessionStatsFromStatsMap":        NewDbSessionStatsFromStatsMap,
		"NewDbTransactionViewFromRecord":       NewDbTransactionViewFromRecord,
		"NewDbMessageReconstruction":           NewDbMessageReconstruction,
		"NewAnalyzeOutputFromAnalysis":         NewAnalyzeOutputFromAnalysis,
		"NewAnalyzeOutputFromScenarioScaffold": NewAnalyzeOutputFromScenarioScaffold,
		"NewInfoView":                          NewInfoView,
		"NewInfoViewFromComposedMessage":       NewInfoViewFromComposedMessage,
	}

	for name, ctor := range constructors {
		if ctor == nil {
			t.Errorf("%s is nil", name)
		}
	}
}

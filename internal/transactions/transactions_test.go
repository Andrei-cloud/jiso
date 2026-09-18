package transactions

import (
	"os"
	"path/filepath"
	"testing"

	json "github.com/goccy/go-json"
	"github.com/moov-io/iso8583"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"github.com/stretchr/testify/suite"
)

type TransactionCollectionSuite struct {
	suite.Suite
	tc *TransactionCollection
}

func (suite *TransactionCollectionSuite) SetupTest() {
	// Create a temporary file with the test data
	data := []map[string]any{
		{
			"name":        "test1",
			"description": "Test transaction 1",
			"fields": map[string]any{
				"2":  "1234567890123456",
				"3":  123456,
				"4":  "10000",
				"7":  "auto",
				"11": "auto",
				"37": "auto",
			},
		},
		{
			"name":        "test2",
			"description": "Test transaction 2",
			"fields": map[string]any{
				"2":  "9876543210987654",
				"3":  654321,
				"4":  "20000",
				"7":  "auto",
				"11": "auto",
				"37": "auto",
			},
		},
	}
	dataBytes, err := json.Marshal(data)
	suite.Require().NoError(err)

	tmpDir := suite.T().TempDir()
	tmpFile := filepath.Join(tmpDir, "test_transactions.json")
	err = os.WriteFile(tmpFile, dataBytes, 0o644)
	suite.Require().NoError(err)

	spec := iso8583.Spec87
	tc, err := NewTransactionCollection(tmpFile, spec)
	suite.Require().NoError(err)
	suite.tc = tc
}

func (suite *TransactionCollectionSuite) TestListNames() {
	names := suite.tc.ListNames()
	suite.Len(names, 2)
	suite.Contains(names, "test1")
	suite.Contains(names, "test2")
}

func (suite *TransactionCollectionSuite) TestInfo() {
	info, err := suite.tc.Info("test1")
	suite.NoError(err)
	suite.Equal("test1", info.Name)
	suite.Equal("Test transaction 1", info.Description)
	suite.JSONEq(`{
        "2": "1234567890123456",
        "3": 123456,
        "4": "10000",
        "7": "auto",
        "11": "auto",
        "37": "auto"
    }`, info.FieldsJSON)
}

func TestLoadRealTransactionJSON(t *testing.T) {
	t.Parallel()

	tmpDir := t.TempDir()
	txFile := filepath.Join(tmpDir, "transaction.json")
	sampleJSON := `[
		{
			"type": "transaction",
			"name": "Echo Test",
			"description": "Network Management: Echo",
			"fields": {
				"0": "0800",
				"7": "auto",
				"11": "auto",
				"70": "301"
			}
		},
		{
			"type": "mock_route",
			"name": "Echo Route",
			"match_fields": {
				"0": "0800",
				"70": "301"
			},
			"response_mti": "0810",
			"response_fields": {
				"39": "00"
			}
		}
	]`
	require.NoError(t, os.WriteFile(txFile, []byte(sampleJSON), 0o644))

	spec := iso8583.Spec87
	tc, err := NewTransactionCollection(txFile, spec)
	require.NoError(t, err)
	require.NotNil(t, tc)
	assert.Equal(t, 1, len(tc.ListNames()))
	assert.Equal(t, 1, len(tc.GetMockRoutes()))
}

// TestInfoSpecAndDataset pins the per-transaction spec/dataset exposure the
// §B table renders: declared spec paths verbatim (the fallback spec is never
// reported as declared), inline rows as "inline", a referenced dataset with
// its row count, and a missing reference as the name with rows -1 — the
// table shows the name without a count instead of panicking or lying.
func TestInfoSpecAndDataset(t *testing.T) {
	t.Parallel()

	tmpDir := t.TempDir()
	txFile := filepath.Join(tmpDir, "transactions.json")
	sampleJSON := `[
		{"type":"transaction","name":"declared","description":"spec key","spec":"a/flex.json","fields":{"0":"0800"}},
		{"type":"transaction","name":"legacy","description":"spec_file key","spec_file":"b/mastercard.json","fields":{"0":"0800"}},
		{"type":"transaction","name":"inline","description":"inline rows","dataset":[{"2":"1111222233334444"},{"2":"1111222233335555"}],"fields":{"0":"0200"}},
		{"type":"transaction","name":"hit","description":"named dataset","dataset_name":"pool","fields":{"0":"0200"}},
		{"type":"transaction","name":"miss","description":"missing dataset","dataset_name":"gone","fields":{"0":"0200"}},
		{"type":"transaction","name":"both","description":"inline wins","dataset":[{"2":"1111222233336666"}],"dataset_name":"pool","fields":{"0":"0200"}},
		{"type":"transaction","name":"bare","description":"neither","fields":{"0":"0200"}},
		{"type":"dataset","name":"pool","data":[{"2":"4000000000000002"},{"2":"4000000000000003"},{"2":"4000000000000004"}]}
	]`
	require.NoError(t, os.WriteFile(txFile, []byte(sampleJSON), 0o644))

	tc, err := NewTransactionCollection(txFile, iso8583.Spec87)
	require.NoError(t, err)

	cases := []struct {
		name, spec, dataset string
		rows                int
	}{
		{"declared", "a/flex.json", "", 0},
		{"legacy", "b/mastercard.json", "", 0},
		{"inline", "", "inline", 2},
		{"hit", "", "pool", 3},
		{"miss", "", "gone", -1},
		{"both", "", "inline", 1},
		{"bare", "", "", 0},
	}
	for _, c := range cases {
		info, err := tc.Info(c.name)
		require.NoError(t, err)
		assert.Equal(t, c.spec, info.Spec, "%s: declared spec", c.name)
		assert.Equal(t, c.dataset, info.Dataset, "%s: dataset", c.name)
		assert.Equal(t, c.rows, info.DatasetRows, "%s: dataset rows", c.name)
	}
}

func TestTransactionCollectionSuite(t *testing.T) {
	t.Parallel()

	suite.Run(t, new(TransactionCollectionSuite))
}

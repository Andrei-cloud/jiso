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
	data := []map[string]interface{}{
		{
			"name":        "test1",
			"description": "Test transaction 1",
			"fields": map[string]interface{}{
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
			"fields": map[string]interface{}{
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
	name, desc, fields, err := suite.tc.Info("test1")
	suite.NoError(err)
	suite.Equal("test1", name)
	suite.Equal("Test transaction 1", desc)
	suite.JSONEq(`{
        "2": "1234567890123456",
        "3": 123456,
        "4": "10000",
        "7": "auto",
        "11": "auto",
        "37": "auto"
    }`, fields)
}

func TestLoadRealTransactionJSON(t *testing.T) {
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

func TestTransactionCollectionSuite(t *testing.T) {
	suite.Run(t, new(TransactionCollectionSuite))
}

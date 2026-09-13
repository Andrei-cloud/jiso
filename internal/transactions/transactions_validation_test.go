package transactions

import (
	"os"

	json "github.com/goccy/go-json"
	"github.com/moov-io/iso8583"
	"github.com/moov-io/iso8583/specs"
)

func (suite *TransactionCollectionSuite) TestValidate() {
	// Test valid transaction collection (should pass)
	err := suite.tc.Validate()
	suite.NoError(err)
}

func (suite *TransactionCollectionSuite) TestValidateEmptyCollection() {
	// Create a collection with no transactions
	data := []map[string]any{}
	dataBytes, err := json.Marshal(data)
	suite.Require().NoError(err)
	file, err := os.CreateTemp(suite.T().TempDir(), "empty_transactions.json")
	suite.Require().NoError(err)
	defer func() { _ = os.Remove(file.Name()) }()
	_, err = file.Write(dataBytes)
	suite.Require().NoError(err)

	spec := iso8583.Spec87
	tc, err := NewTransactionCollection(file.Name(), spec)
	suite.Error(err) // Should fail because no transactions found
	suite.Nil(tc)
}

func (suite *TransactionCollectionSuite) TestValidateDuplicateNames() {
	// Create transactions with duplicate names
	data := []map[string]any{
		{
			"name":        "duplicate",
			"description": "First transaction",
			"fields": map[string]any{
				"2": "1234567890123456",
			},
		},
		{
			"name":        "duplicate",
			"description": "Second transaction with same name",
			"fields": map[string]any{
				"2": "9876543210987654",
			},
		},
	}
	dataBytes, err := json.Marshal(data)
	suite.Require().NoError(err)
	file, err := os.CreateTemp(suite.T().TempDir(), "duplicate_transactions.json")
	suite.Require().NoError(err)
	defer func() { _ = os.Remove(file.Name()) }()
	_, err = file.Write(dataBytes)
	suite.Require().NoError(err)

	spec := iso8583.Spec87
	tc, err := NewTransactionCollection(file.Name(), spec)
	suite.Error(err) // Should fail validation
	suite.Nil(tc)
}

func (suite *TransactionCollectionSuite) TestValidateInvalidFieldId() {
	// Create transaction with invalid field ID
	data := []map[string]any{
		{
			"name":        "invalid_field",
			"description": "Transaction with invalid field ID",
			"fields": map[string]any{
				"1": "invalid field ID (should be 2-128)",
			},
		},
	}
	dataBytes, err := json.Marshal(data)
	suite.Require().NoError(err)
	file, err := os.CreateTemp(suite.T().TempDir(), "invalid_field_transactions.json")
	suite.Require().NoError(err)
	defer func() { _ = os.Remove(file.Name()) }()
	_, err = file.Write(dataBytes)
	suite.Require().NoError(err)

	spec := iso8583.Spec87
	tc, err := NewTransactionCollection(file.Name(), spec)
	suite.Error(err) // Should fail validation
	suite.Nil(tc)
}

func (suite *TransactionCollectionSuite) TestValidateInvalidDataset() {
	// Create transaction with invalid dataset
	data := []map[string]any{
		{
			"name":        "invalid_dataset",
			"description": "Transaction with invalid dataset",
			"fields": map[string]any{
				"2": "1234567890123456",
			},
			"dataset": []map[int]string{
				{
					1: "", // Invalid field ID and empty value
				},
			},
		},
	}
	dataBytes, err := json.Marshal(data)
	suite.Require().NoError(err)
	file, err := os.CreateTemp(suite.T().TempDir(), "invalid_dataset_transactions.json")
	suite.Require().NoError(err)
	defer func() { _ = os.Remove(file.Name()) }()
	_, err = file.Write(dataBytes)
	suite.Require().NoError(err)

	spec := iso8583.Spec87
	tc, err := NewTransactionCollection(file.Name(), spec)
	suite.Error(err) // Should fail validation
	suite.Nil(tc)
}

func (suite *TransactionCollectionSuite) TestValidatePerTransactionSpec() {
	data := []map[string]any{
		{
			"type":        "transaction",
			"name":        "Echo Mastercard Test",
			"description": "Network Management: Echo Mastercard",
			"spec":        "specs/mastercard.json",
			"fields": map[string]any{
				"0":  "0800",
				"70": "301",
			},
		},
	}
	dataBytes, err := json.Marshal(data)
	suite.Require().NoError(err)
	file, err := os.CreateTemp(suite.T().TempDir(), "per_spec_transactions.json")
	suite.Require().NoError(err)
	defer func() { _ = os.Remove(file.Name()) }()
	_, err = file.Write(dataBytes)
	suite.Require().NoError(err)

	spec := iso8583.Spec87
	tc, err := NewTransactionCollection(file.Name(), spec)
	suite.NoError(err)
	suite.NotNil(tc)
}

func (suite *TransactionCollectionSuite) TestValidateBinaryFieldHexLengthUsesBytes() {
	data := []map[string]any{
		{
			"type":        "transaction",
			"name":        "binary_hex_len_ok",
			"description": "Binary field hex length validation",
			"fields": map[string]any{
				"0":  "0400",
				"61": "000000000000000000000000000000001300",
			},
		},
	}
	dataBytes, err := json.Marshal(data)
	suite.Require().NoError(err)
	file, err := os.CreateTemp(suite.T().TempDir(), "binary_hex_len_transactions.json")
	suite.Require().NoError(err)
	defer func() { _ = os.Remove(file.Name()) }()
	_, err = file.Write(dataBytes)
	suite.Require().NoError(err)

	specJSON := []byte(`{
		"fields": {
			"0": {
				"type": "String",
				"length": 4,
				"description": "Message Type Indicator",
				"enc": "ASCII",
				"prefix": "ASCII.Fixed"
			},
			"1": {
				"type": "Bitmap",
				"length": 8,
				"description": "Bitmap",
				"enc": "Binary",
				"prefix": "Hex.Fixed"
			},
			"61": {
				"type": "Binary",
				"length": 19,
				"description": "Point of Service Data",
				"enc": "Binary",
				"prefix": "Hex.Fixed"
			}
		}
	}`)
	spec, err := specs.ImportJSON(specJSON)
	suite.Require().NoError(err)

	tc, err := NewTransactionCollection(file.Name(), spec)
	suite.NoError(err)
	suite.NotNil(tc)
}

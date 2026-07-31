package transactions

import (
	json "github.com/goccy/go-json"
	"os"

	"github.com/moov-io/iso8583"
)



func (suite *TransactionCollectionSuite) TestValidate() {
	// Test valid transaction collection (should pass)
	err := suite.tc.Validate()
	suite.NoError(err)
}

func (suite *TransactionCollectionSuite) TestValidateEmptyCollection() {
	// Create a collection with no transactions
	data := []map[string]interface{}{}
	dataBytes, err := json.Marshal(data)
	suite.Require().NoError(err)
	file, err := os.CreateTemp("", "empty_transactions.json")
	suite.Require().NoError(err)
	defer os.Remove(file.Name())
	_, err = file.Write(dataBytes)
	suite.Require().NoError(err)

	spec := iso8583.Spec87
	tc, err := NewTransactionCollection(file.Name(), spec)
	suite.Error(err) // Should fail because no transactions found
	suite.Nil(tc)
}

func (suite *TransactionCollectionSuite) TestValidateDuplicateNames() {
	// Create transactions with duplicate names
	data := []map[string]interface{}{
		{
			"name":        "duplicate",
			"description": "First transaction",
			"fields": map[string]interface{}{
				"2": "1234567890123456",
			},
		},
		{
			"name":        "duplicate",
			"description": "Second transaction with same name",
			"fields": map[string]interface{}{
				"2": "9876543210987654",
			},
		},
	}
	dataBytes, err := json.Marshal(data)
	suite.Require().NoError(err)
	file, err := os.CreateTemp("", "duplicate_transactions.json")
	suite.Require().NoError(err)
	defer os.Remove(file.Name())
	_, err = file.Write(dataBytes)
	suite.Require().NoError(err)

	spec := iso8583.Spec87
	tc, err := NewTransactionCollection(file.Name(), spec)
	suite.Error(err) // Should fail validation
	suite.Nil(tc)
}

func (suite *TransactionCollectionSuite) TestValidateInvalidFieldId() {
	// Create transaction with invalid field ID
	data := []map[string]interface{}{
		{
			"name":        "invalid_field",
			"description": "Transaction with invalid field ID",
			"fields": map[string]interface{}{
				"1": "invalid field ID (should be 2-128)",
			},
		},
	}
	dataBytes, err := json.Marshal(data)
	suite.Require().NoError(err)
	file, err := os.CreateTemp("", "invalid_field_transactions.json")
	suite.Require().NoError(err)
	defer os.Remove(file.Name())
	_, err = file.Write(dataBytes)
	suite.Require().NoError(err)

	spec := iso8583.Spec87
	tc, err := NewTransactionCollection(file.Name(), spec)
	suite.Error(err) // Should fail validation
	suite.Nil(tc)
}

func (suite *TransactionCollectionSuite) TestValidateInvalidDataset() {
	// Create transaction with invalid dataset
	data := []map[string]interface{}{
		{
			"name":        "invalid_dataset",
			"description": "Transaction with invalid dataset",
			"fields": map[string]interface{}{
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
	file, err := os.CreateTemp("", "invalid_dataset_transactions.json")
	suite.Require().NoError(err)
	defer os.Remove(file.Name())
	_, err = file.Write(dataBytes)
	suite.Require().NoError(err)

	spec := iso8583.Spec87
	tc, err := NewTransactionCollection(file.Name(), spec)
	suite.Error(err) // Should fail validation
	suite.Nil(tc)
}


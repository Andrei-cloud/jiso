package transactions

import (
	"encoding/json"
	"os"
	"testing"

	"github.com/moov-io/iso8583"
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
	file, err := os.CreateTemp("", "transactions.json")
	suite.Require().NoError(err)
	defer os.Remove(file.Name())
	_, err = file.Write(dataBytes)
	suite.Require().NoError(err)

	// Create a new TransactionCollection instance with the temporary file
	spec := iso8583.Spec87
	tc, err := NewTransactionCollection(file.Name(), spec)
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

func (suite *TransactionCollectionSuite) TestCompose() {
	msg, err := suite.tc.Compose("test1")
	suite.NotNil(msg)
	suite.NoError(err)
	suite.Equal(iso8583.Spec87, msg.GetSpec())

	// Assert that the message fields are correct
	value, err := msg.GetField(2).String()
	suite.NoError(err)
	suite.Equal("1234567890123456", value)

	value, err = msg.GetField(3).String()
	suite.NoError(err)
	suite.Equal("123456", value)

	value, err = msg.GetField(4).String()
	suite.NoError(err)
	suite.Equal("10000", value)

	// Assert that the auto-generated fields are correct
	value, err = msg.GetField(7).String()
	suite.NoError(err)
	suite.NotEmpty(value)

	value, err = msg.GetField(11).String()
	suite.NoError(err)
	suite.NotEmpty(value)

	value, err = msg.GetField(37).String()
	suite.NoError(err)
	suite.NotEmpty(value)

}

func (suite *TransactionCollectionSuite) TestListFormatted() {
	formatted := suite.tc.ListFormatted()
	suite.Len(formatted, 2)
	suite.Contains(formatted, "test1 - Test transaction 1")
	suite.Contains(formatted, "test2 - Test transaction 2")
}

func TestTransactionCollectionSuite(t *testing.T) {
	suite.Run(t, new(TransactionCollectionSuite))
}

package transactions

import (
	"encoding/json"
	"os"
	"testing"

	"github.com/moov-io/iso8583"
	"github.com/moov-io/iso8583/encoding"
	"github.com/moov-io/iso8583/field"
	"github.com/moov-io/iso8583/prefix"
	"github.com/moov-io/iso8583/specs"
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

func (suite *TransactionCollectionSuite) TestCompositeField() {
	compositeTestSpecWithSizedBitmap := &field.Spec{
		Length:      30,
		Description: "Test Spec",
		Pref:        prefix.ASCII.LL,
		Bitmap: field.NewBitmap(&field.Spec{
			Length:            8,
			Description:       "Bitmap",
			Enc:               encoding.BytesToASCIIHex,
			Pref:              prefix.Hex.Fixed,
			DisableAutoExpand: true,
		}),
		Subfields: map[string]field.Field{
			"1": field.NewString(&field.Spec{
				Length:      2,
				Description: "String Field",
				Enc:         encoding.ASCII,
				Pref:        prefix.ASCII.LL,
			}),
			"2": field.NewString(&field.Spec{
				Length:      2,
				Description: "String Field",
				Enc:         encoding.ASCII,
				Pref:        prefix.ASCII.LL,
			}),
			"3": field.NewString(&field.Spec{
				Length:      6,
				Description: "Numeric Field",
				Enc:         encoding.ASCII,
				Pref:        prefix.ASCII.Fixed,
			}),
			"4": field.NewString(&field.Spec{
				Length:      6,
				Description: "Numeric Field",
				Enc:         encoding.ASCII,
				Pref:        prefix.ASCII.Fixed,
			}),
			"5": field.NewString(&field.Spec{
				Length:      6,
				Description: "Numeric Field",
				Enc:         encoding.ASCII,
				Pref:        prefix.ASCII.Fixed,
			}),
			"6": field.NewString(&field.Spec{
				Length:      6,
				Description: "Numeric Field",
				Enc:         encoding.ASCII,
				Pref:        prefix.ASCII.Fixed,
			}),
			"7": field.NewString(&field.Spec{
				Length:      6,
				Description: "Numeric Field",
				Enc:         encoding.ASCII,
				Pref:        prefix.ASCII.Fixed,
			}),
			"8": field.NewString(&field.Spec{
				Length:      6,
				Description: "Numeric Field",
				Enc:         encoding.ASCII,
				Pref:        prefix.ASCII.Fixed,
			}),
			"9": field.NewString(&field.Spec{
				Length:      6,
				Description: "Numeric Field",
				Enc:         encoding.ASCII,
				Pref:        prefix.ASCII.Fixed,
			}),
			"10": field.NewString(&field.Spec{
				Length:      6,
				Description: "Numeric Field",
				Enc:         encoding.ASCII,
				Pref:        prefix.ASCII.Fixed,
			}),
		},
	}

	data := struct {
		F1  *field.String
		F2  *field.String
		F3  *field.String
		F4  *field.String
		F5  *field.String
		F6  *field.String
		F7  *field.String
		F8  *field.String
		F9  *field.String
		F10 *field.String
	}{
		F10: field.NewStringValue("11 456"),
	}

	composite := field.NewComposite(compositeTestSpecWithSizedBitmap)
	err := composite.Marshal(&data)
	suite.NoError(err)

	packed, err := composite.Pack()
	suite.NoError(err)
	suite.Equal("22004000000000000011 456", string(packed))
}

func (suite *TransactionCollectionSuite) TestSpecWithCompositeField() {
	specJSON := []byte(`{
		"name": "ISO8583_DHI",
		"fields": {
			"1": {
				"type": "Composite",
				"length": 255,
				"description": "Private use field",
				"prefix": "ASCII.LL",
				"bitmap": {
						"type": "Bitmap",
						"length": 8,
						"description": "Bitmap",
						"enc": "HexToASCII",
						"prefix": "Hex.Fixed",
						"disableautoexpand": true
				},
				"subfields": {
					"1": {
						"type": "String",
						"length": 2,
						"description": "Cardholder certificate Serial Number",
						"enc": "ASCII",
						"prefix": "ASCII.Fixed"
					},
					"2": {
						"type": "String",
						"length": 2,
						"description": "Merchant certificate Serial Number",
						"enc": "ASCII",
						"prefix": "ASCII.Fixed"
					},
					"3": {
						"type": "String",
						"length": 2,
						"description": "Transaction ID",
						"enc": "ASCII",
						"prefix": "ASCII.Fixed"
					},
					"4": {
						"type": "String",
						"length": 20,
						"description": "CAVV",
						"enc": "ASCII",
						"prefix": "ASCII.Fixed"
					},
					"5": {
						"type": "String",
						"length": 20,
						"description": "CAVV",
						"enc": "ASCII",
						"prefix": "ASCII.Fixed"
					},
					"6": {
						"type": "String",
						"length": 2,
						"description": "Cardholder certificate Serial Number",
						"enc": "ASCII",
						"prefix": "ASCII.Fixed"
					},
					"7": {
						"type": "String",
						"length": 2,
						"description": "Merchant certificate Serial Number",
						"enc": "ASCII",
						"prefix": "ASCII.Fixed"
					},
					"8": {
						"type": "String",
						"length": 2,
						"description": "Transaction ID",
						"enc": "ASCII",
						"prefix": "ASCII.Fixed"
					},
					"9": {
						"type": "String",
						"length": 20,
						"description": "CAVV",
						"enc": "ASCII",
						"prefix": "ASCII.Fixed"
					},
					"10": {
						"type": "String",
						"length": 6,
						"description": "CVV2",
						"enc": "ASCII",
						"prefix": "ASCII.Fixed"
					}
				}
			}
		}
	}`)

	spec, err := specs.Builder.ImportJSON(specJSON)
	suite.NoError(err)

	data := struct {
		F1  *field.String
		F2  *field.String
		F3  *field.String
		F4  *field.String
		F5  *field.String
		F6  *field.String
		F7  *field.String
		F8  *field.String
		F9  *field.String
		F10 *field.String
	}{
		F10: field.NewStringValue("11 456"),
	}

	compositeRestored := field.NewComposite(spec.Fields[1].Spec())
	err = compositeRestored.Marshal(&data)
	suite.NoError(err)

	packed, err := compositeRestored.Pack()
	suite.NoError(err)
	suite.Equal("22004000000000000011 456", string(packed))
}

func TestTransactionCollectionSuite(t *testing.T) {
	suite.Run(t, new(TransactionCollectionSuite))
}

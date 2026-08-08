package transactions

import (
	"os"
	"path/filepath"

	json "github.com/goccy/go-json"
	"github.com/moov-io/iso8583"
	"github.com/moov-io/iso8583/encoding"
	"github.com/moov-io/iso8583/field"
	"github.com/moov-io/iso8583/prefix"
	"github.com/moov-io/iso8583/specs"
)

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

func (suite *TransactionCollectionSuite) TestComposeEchoMastercard() {
	wd, err := os.Getwd()
	suite.Require().NoError(err)
	projectRoot := filepath.Dir(filepath.Dir(wd))
	txPath := filepath.Join(projectRoot, "transactions", "transaction.json")

	tc, err := NewTransactionCollection(txPath, iso8583.Spec87)
	suite.Require().NoError(err)
	suite.Require().NotNil(tc)

	msg, err := tc.Compose("Echo Mastercard")
	suite.NoError(err)
	suite.NotNil(msg)

	// Check fields
	f2, err := msg.GetField(2).String()
	suite.NoError(err)
	suite.Equal("41275", f2)

	f7, err := msg.GetField(7).String()
	suite.NoError(err)
	suite.NotEmpty(f7)

	f11, err := msg.GetField(11).String()
	suite.NoError(err)
	suite.NotEmpty(f11)

	f70, err := msg.GetField(70).String()
	suite.NoError(err)
	suite.Equal("270", f70)

	// Pack message to verify ISO8583 encoding succeeds
	packed, err := msg.Pack()
	suite.NoError(err)
	suite.NotEmpty(packed)
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

func (suite *TransactionCollectionSuite) TestComposeBitmapCompositeFromDatasetSubfields() {
	spec := &iso8583.MessageSpec{
		Name: "bitmap-composite-compose-test",
		Fields: map[int]field.Field{
			0: field.NewString(&field.Spec{Length: 4, Description: "MTI", Enc: encoding.ASCII, Pref: prefix.ASCII.Fixed}),
			1: field.NewBitmap(&field.Spec{Length: 8, Description: "Bitmap", Enc: encoding.Binary, Pref: prefix.Binary.Fixed}),
			3: field.NewString(&field.Spec{Length: 6, Description: "Processing Code", Enc: encoding.ASCII, Pref: prefix.ASCII.Fixed}),
			62: field.NewComposite(&field.Spec{
				Length:      20,
				Description: "Bitmap Composite",
				Pref:        prefix.ASCII.LL,
				Bitmap:      field.NewBitmap(&field.Spec{Length: 1, Description: "Field 62.0 Bitmap", Enc: encoding.Binary, Pref: prefix.Binary.Fixed, DisableAutoExpand: true}),
				Subfields: map[string]field.Field{
					"1": field.NewString(&field.Spec{Length: 2, Description: "Field 62.1", Enc: encoding.ASCII, Pref: prefix.ASCII.Fixed}),
					"2": field.NewString(&field.Spec{Length: 2, Description: "Field 62.2", Enc: encoding.ASCII, Pref: prefix.ASCII.Fixed}),
				},
			}),
		},
	}

	file, err := os.CreateTemp("", "bitmap_composite_dataset_*.json")
	suite.Require().NoError(err)
	defer os.Remove(file.Name())

	content := []byte(`[
		{
			"type": "transaction",
			"name": "bitmap-compose",
			"dataset_name": "bitmap_dataset",
			"fields": {
				"0": "0100",
				"3": "000000",
				"62": {
					"1": "{{data.DE_62_1}}",
					"2": "{{data.DE_62_2}}"
				}
			}
		},
		{
			"type": "dataset",
			"name": "bitmap_dataset",
			"data": [
				{
					"DE_62_2": "BB"
				}
			]
		}
	]`)
	_, err = file.Write(content)
	suite.Require().NoError(err)

	tc, err := NewTransactionCollection(file.Name(), spec)
	suite.Require().NoError(err)

	msg, err := tc.Compose("bitmap-compose")
	suite.Require().NoError(err)

	composite, ok := msg.GetField(62).(*field.Composite)
	suite.Require().True(ok)
	subfields := composite.GetSubfields()
	_, hasField1 := subfields["1"]
	field2, hasField2 := subfields["2"]
	suite.False(hasField1)
	suite.True(hasField2)

	value2, err := field2.String()
	suite.Require().NoError(err)
	suite.Equal("BB", value2)

	packed, err := msg.Pack()
	suite.Require().NoError(err)
	suite.NotEmpty(packed)
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

	spec, err := specs.ImportJSON(specJSON)
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

func (suite *TransactionCollectionSuite) TestReservedAutoKeywords() {
	suite.True(isReservedAutoKeywordString("auto"))
	suite.True(isReservedAutoKeywordString("$auto"))
	suite.True(isReservedAutoKeywordString("STAN"))
	suite.True(isReservedAutoKeywordString("$STAN"))
	suite.True(isReservedAutoKeywordString("GEN_STAN"))
	suite.True(isReservedAutoKeywordString("RRN"))
	suite.True(isReservedAutoKeywordString("$RRN"))
	suite.True(isReservedAutoKeywordString("GEN_RRN"))
	suite.True(isReservedAutoKeywordString("auth_code"))
	suite.True(isReservedAutoKeywordString("$auth_code"))
	suite.True(isReservedAutoKeywordString("gen_auth_code"))
	suite.True(isReservedAutoKeywordString("datetime"))
	suite.True(isReservedAutoKeywordString("random"))

	msg := iso8583.NewMessage(iso8583.Spec87)
	suite.tc.handleAutoFieldsWithKeyword(11, msg, "STAN")
	f11 := msg.GetField(11)
	suite.NotNil(f11)
	val11, err := f11.String()
	suite.NoError(err)
	suite.NotEmpty(val11)

	suite.tc.handleAutoFieldsWithKeyword(37, msg, "RRN")
	f37 := msg.GetField(37)
	suite.NotNil(f37)
	val37, err := f37.String()
	suite.NoError(err)
	suite.NotEmpty(val37)

	suite.tc.handleAutoFieldsWithKeyword(38, msg, "AUTH_CODE")
	f38 := msg.GetField(38)
	suite.NotNil(f38)
	val38, err := f38.String()
	suite.NoError(err)
	suite.Equal(6, len(val38))

	msgAuto := iso8583.NewMessage(iso8583.Spec87)
	suite.tc.handleAutoFields(38, msgAuto)
	f38Auto := msgAuto.GetField(38)
	suite.NotNil(f38Auto)
	val38Auto, err := f38Auto.String()
	suite.NoError(err)
	suite.Equal(6, len(val38Auto))

	msgField90 := iso8583.NewMessage(iso8583.Spec87)
	suite.tc.handleAutoFields(90, msgField90)
	f90 := msgField90.GetField(90)
	suite.NotNil(f90)
	val90, err := f90.String()
	suite.NoError(err)
	suite.Equal(42, len(val90))
}

func (suite *TransactionCollectionSuite) TestMockRouteLatencyJitterParsing() {
	data := []map[string]interface{}{
		{
			"type":        "transaction",
			"name":        "test_tx",
			"description": "Test transaction",
			"fields": map[string]interface{}{
				"0": "0800",
			},
		},
		{
			"type": "mock_route",
			"name": "Sign On Route",
			"match_fields": map[string]interface{}{
				"0": "0800",
			},
			"latency_ms": 100,
			"jitter_ms":  25,
		},
	}
	dataBytes, err := json.Marshal(data)
	suite.Require().NoError(err)
	file, err := os.CreateTemp("", "transactions_mock.json")
	suite.Require().NoError(err)
	defer os.Remove(file.Name())
	_, err = file.Write(dataBytes)
	suite.Require().NoError(err)

	tc, err := NewTransactionCollection(file.Name(), iso8583.Spec87)
	suite.Require().NoError(err)
	routes := tc.GetMockRoutes()
	suite.Require().Len(routes, 1)
	suite.Equal("Sign On Route", routes[0].Name)
	suite.Equal(100, routes[0].LatencyMs)
	suite.Equal(25, routes[0].JitterMs)
}

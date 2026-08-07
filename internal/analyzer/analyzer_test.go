package analyzer

import (
	"bytes"
	"encoding/binary"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"jiso/internal/config"
	"jiso/internal/utils"

	json "github.com/goccy/go-json"

	"github.com/moov-io/iso8583"
	"github.com/moov-io/iso8583/encoding"
	"github.com/moov-io/iso8583/field"
	"github.com/moov-io/iso8583/prefix"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestStreamAnalyzerAndVarianceEngine(t *testing.T) {
	spec, err := utils.CreateSpecFromFile("../../specs/spec_bcp.json")
	require.NoError(t, err)

	msg1 := iso8583.NewMessage(spec)
	msg1.MTI("0200")
	msg1.Field(3, "000000")
	msg1.Field(7, "0412232900")
	msg1.Field(11, "000001")
	msg1.Field(2, "4111111111111111")

	p1, err := msg1.Pack()
	require.NoError(t, err)

	msg2 := iso8583.NewMessage(spec)
	msg2.MTI("0200")
	msg2.Field(3, "000000")
	msg2.Field(7, "0412233000")
	msg2.Field(11, "000002")
	msg2.Field(2, "4222222222222222")

	p2, err := msg2.Pack()
	require.NoError(t, err)

	// Build stream bytes
	stream := make([]byte, 2+len(p1)+2+len(p2))
	binary.BigEndian.PutUint16(stream[0:2], uint16(len(p1)))
	copy(stream[2:], p1)

	offset := 2 + len(p1)
	binary.BigEndian.PutUint16(stream[offset:offset+2], uint16(len(p2)))
	copy(stream[offset+2:], p2)

	analyzer := NewStreamAnalyzer(spec)
	extracted, err := analyzer.ExtractMessagesFromStream(stream)
	require.NoError(t, err)
	assert.Len(t, extracted, 2)

	flows := analyzer.AggregateFlows(extracted)
	require.Len(t, flows, 1)

	flowKey := "0200_000000"
	flow, exists := flows[flowKey]
	require.True(t, exists)
	assert.Equal(t, 2, flow.Count)

	varianceEng := NewVarianceEngine(spec)
	results, err := varianceEng.AnalyzeFlow(flow)
	require.NoError(t, err)
	require.Len(t, results, 1)

	res := results[0]
	assert.Equal(t, config.TypeTransaction, res.Transaction.GetType())
	assert.Equal(t, config.TypeDataset, res.Dataset.GetType())
	assert.Len(t, res.Dataset.Data, 2)
	assert.True(t, strings.HasPrefix(res.Dataset.Data[0]["DE_2"], "41111111"))
	assert.True(t, strings.HasSuffix(res.Dataset.Data[0]["DE_2"], "000"))
	assert.True(t, strings.HasPrefix(res.Dataset.Data[1]["DE_2"], "42222222"))
	assert.True(t, strings.HasSuffix(res.Dataset.Data[1]["DE_2"], "000"))

	// Test Unsecure mode (unsecure = true) preserves clear PANs
	unsecEng := NewVarianceEngine(spec, true)
	unsecResults, err := unsecEng.AnalyzeFlow(flow)
	require.NoError(t, err)
	require.Len(t, unsecResults, 1)
	assert.Equal(t, "4111111111111111", unsecResults[0].Dataset.Data[0]["DE_2"])
	assert.Equal(t, "4222222222222222", unsecResults[0].Dataset.Data[1]["DE_2"])
}

func TestNetworkManagement08XX(t *testing.T) {
	spec, err := utils.CreateSpecFromFile("../../specs/spec_bcp.json")
	require.NoError(t, err)

	msg1 := iso8583.NewMessage(spec)
	msg1.MTI("0800")
	msg1.Field(3, "990000")
	msg1.Field(7, "0412232900")
	msg1.Field(11, "000001")
	msg1.Field(70, "301")

	msg2 := iso8583.NewMessage(spec)
	msg2.MTI("0800")
	msg2.Field(3, "990000")
	msg2.Field(7, "0412233000")
	msg2.Field(11, "000002")
	msg2.Field(70, "301")

	flow := &CapturedFlow{
		MTI:      "0800",
		DE3:      "990000",
		Messages: []*iso8583.Message{msg1, msg2},
		Count:    2,
	}

	varianceEng := NewVarianceEngine(spec)
	results, err := varianceEng.AnalyzeFlow(flow)
	require.NoError(t, err)

	// Since MTI is 0800 and 70=301 is same for both (with 7,11 mapping to auto), they deduplicate to 1 network transaction
	require.Len(t, results, 1)
	tx := results[0].Transaction
	assert.Equal(t, "Captured Network 0800 #1", tx.Name)
	assert.Empty(t, tx.DatasetName)
	assert.Empty(t, results[0].Dataset.Name)

	var fields map[string]interface{}
	err = json.Unmarshal(tx.Fields, &fields)
	require.NoError(t, err)

	assert.Equal(t, "auto", fields["7"])
	assert.Equal(t, "auto", fields["11"])
	assert.EqualValues(t, 301, fields["70"])
	assert.Nil(t, fields["1"]) // Field 1 (Bitmap) MUST NOT be present
}

func TestAnalyzeFlow_VaryingBitmapCompositeUsesSubfieldDataset(t *testing.T) {
	spec := &iso8583.MessageSpec{
		Name: "bitmap-composite-test",
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

	buildMsg := func(subfieldID, value string) *iso8583.Message {
		msg := iso8583.NewMessage(spec)
		msg.MTI("0100")
		require.NoError(t, msg.Field(3, "000000"))

		composite := field.NewComposite(spec.Fields[62].Spec())
		require.NoError(t, composite.MarshalPath(subfieldID, value))
		packed, err := composite.Bytes()
		require.NoError(t, err)
		require.NoError(t, msg.BinaryField(62, packed))
		return msg
	}

	flow := &CapturedFlow{
		MTI:      "0100",
		DE3:      "000000",
		Messages: []*iso8583.Message{buildMsg("1", "AA"), buildMsg("2", "BB")},
		Count:    2,
	}

	results, err := NewVarianceEngine(spec).AnalyzeFlow(flow)
	require.NoError(t, err)
	require.Len(t, results, 1)

	var fields map[string]interface{}
	require.NoError(t, json.Unmarshal(results[0].Transaction.Fields, &fields))
	field62, ok := fields["62"].(map[string]interface{})
	require.True(t, ok)
	assert.Equal(t, "{{data.DE_62_1}}", field62["1"])
	assert.Equal(t, "{{data.DE_62_2}}", field62["2"])

	require.Len(t, results[0].Dataset.Data, 2)
	assert.Equal(t, "AA", results[0].Dataset.Data[0]["DE_62_1"])
	assert.Empty(t, results[0].Dataset.Data[0]["DE_62"])
	_, hasBitmapBlob := results[0].Dataset.Data[0]["DE_62"]
	assert.False(t, hasBitmapBlob)
	assert.Equal(t, "BB", results[0].Dataset.Data[1]["DE_62_2"])
	_, hasRawField := results[0].Dataset.Data[1]["DE_62"]
	assert.False(t, hasRawField)
}

func TestExtractFromPCAPFile(t *testing.T) {
	spec, err := utils.CreateSpecFromFile("../../specs/spec.json")
	if err != nil {
		spec, err = utils.CreateSpecFromFile("./specs/spec.json")
	}
	require.NoError(t, err)

	analyzer := NewStreamAnalyzer(spec)
	extracted, err := analyzer.ExtractMessagesFromFile("../../output.pcap", "binary2")
	if err != nil {
		extracted, err = analyzer.ExtractMessagesFromFile("output.pcap", "binary2")
	}
	if err == nil && len(extracted) > 0 {
		flows := analyzer.AggregateFlows(extracted)
		varianceEng := NewVarianceEngine(spec)
		var items []config.ConfigItem

		for _, flow := range flows {
			results, err := varianceEng.AnalyzeFlow(flow)
			require.NoError(t, err)
			for _, res := range results {
				items = append(items, res.Transaction)
				if res.Dataset.Name != "" && len(res.Dataset.Data) > 0 {
					items = append(items, res.Dataset)
				}
			}
		}

		require.NotEmpty(t, items)
		data, err := json.MarshalIndent(items, "", "  ")
		require.NoError(t, err)

		_ = os.WriteFile(filepath.Join(t.TempDir(), "pcaped.json"), data, 0o644)
	}
}

func TestInspectAndFilterPCAPDirections(t *testing.T) {
	dirs, err := InspectPCAPDirections("../../output.pcap")
	if err != nil {
		dirs, err = InspectPCAPDirections("output.pcap")
	}
	if err == nil {
		require.NotEmpty(t, dirs)
		// Should discover Port 9999 directions and All directions
		has9999 := false
		for _, d := range dirs {
			if d.TargetPort == 9999 || d.Mode == "all" {
				has9999 = true
				break
			}
		}
		assert.True(t, has9999)

		spec, err := utils.CreateSpecFromFile("../../specs/spec.json")
		if err != nil {
			spec, err = utils.CreateSpecFromFile("./specs/spec.json")
		}
		require.NoError(t, err)

		streamAnalyzer := NewStreamAnalyzer(spec)
		// Test filtering by Dst Port 9999 (Requests)
		dstDir := TrafficDirection{TargetPort: 9999, Mode: "dst", Label: "Dst 9999"}
		extractedDst, err := streamAnalyzer.ExtractMessagesFromFileWithDirection("../../output.pcap", "binary2", dstDir)
		if err != nil {
			extractedDst, err = streamAnalyzer.ExtractMessagesFromFileWithDirection("output.pcap", "binary2", dstDir)
		}
		if err == nil {
			assert.NotEmpty(t, extractedDst)
			flows := streamAnalyzer.AggregateFlows(extractedDst)
			varianceEng := NewVarianceEngine(spec)
			var items []config.ConfigItem

			for _, flow := range flows {
				results, err := varianceEng.AnalyzeFlow(flow)
				require.NoError(t, err)
				for _, res := range results {
					items = append(items, res.Transaction)
					if res.Dataset.Name != "" && len(res.Dataset.Data) > 0 {
						items = append(items, res.Dataset)
					}
				}
			}

			// Verify that no response MTI (0210, 0410, 0810) exists in extractedDst or generated items
			for _, item := range items {
				var fields map[string]interface{}
				if err := json.Unmarshal(item.Fields, &fields); err == nil {
					mti, _ := fields["0"].(string)
					assert.NotContains(t, []string{"0210", "0410", "0810"}, mti, "Response MTI should not be present in request-only directional extraction")
				}
			}

			data, err := json.MarshalIndent(items, "", "  ")
			require.NoError(t, err)
			_ = os.WriteFile("../../transactions/pcaped.json", data, 0o644)

			// Test filtering by Src Port 9999 (Outgoing Responses -> Mock Routes)
			srcDir := TrafficDirection{TargetPort: 9999, Mode: "src", Label: "Src 9999"}
			extractedSrc, err := streamAnalyzer.ExtractMessagesFromFileWithDirection("../../output.pcap", "binary2", srcDir)
			if err != nil {
				extractedSrc, err = streamAnalyzer.ExtractMessagesFromFileWithDirection("output.pcap", "binary2", srcDir)
			}
			if err == nil && len(extractedSrc) > 0 {
				srcFlows := streamAnalyzer.AggregateFlows(extractedSrc)
				var routeItems []config.ConfigItem
				for _, flow := range srcFlows {
					results, err := varianceEng.AnalyzeFlowToMockRoutes(flow)
					require.NoError(t, err)
					for _, res := range results {
						routeItems = append(routeItems, res.Transaction)
					}
				}
				if len(routeItems) > 0 {
					routeData, err := json.MarshalIndent(routeItems, "", "  ")
					require.NoError(t, err)
					_ = os.WriteFile("../../transactions/mock_routes.json", routeData, 0o644)
				}
			}
		}
	}
}

func TestStreamAnalyzerMultiHeader(t *testing.T) {
	spec, err := utils.CreateSpecFromFile("../../specs/spec_bcp.json")
	require.NoError(t, err)

	msg := iso8583.NewMessage(spec)
	msg.MTI("0800")
	msg.Field(3, "990000")
	msg.Field(11, "123456")
	packed, err := msg.Pack()
	require.NoError(t, err)

	headerTypes := []string{"ascii4", "binary2", "bcd2", "NAPS"}

	for _, hType := range headerTypes {
		t.Run("Header_"+hType, func(t *testing.T) {
			hdr, err := utils.SelectLength(hType)
			require.NoError(t, err)

			var buf bytes.Buffer
			writeLenFunc := utils.WriteMessageLengthWrapper(hdr)
			if hType == "NAPS" {
				writeLenFunc = utils.NapsWriteLengthWrapper(writeLenFunc)
			}

			_, err = writeLenFunc(&buf, len(packed))
			require.NoError(t, err)
			buf.Write(packed)

			analyzer := NewStreamAnalyzer(spec)
			extracted, err := analyzer.ExtractMessagesFromStream(buf.Bytes(), hType)
			require.NoError(t, err)
			require.Len(t, extracted, 1)

			mti, _ := extracted[0].GetMTI()
			assert.Equal(t, "0800", mti)
		})
	}
}

func TestAnalyzeFlowToMockRoutes(t *testing.T) {
	spec, err := utils.CreateSpecFromFile("../../specs/spec.json")
	require.NoError(t, err)

	respMsg1 := iso8583.NewMessage(spec)
	respMsg1.MTI("0210")
	respMsg1.Field(3, "000000")
	respMsg1.Field(7, "0412232900")
	respMsg1.Field(11, "000001")
	respMsg1.Field(22, "021")
	respMsg1.Field(38, "824664")
	respMsg1.Field(39, "00")

	flow := &CapturedFlow{
		MTI:      "0210",
		DE3:      "000000",
		DE22:     "021",
		Messages: []*iso8583.Message{respMsg1},
		Count:    1,
	}

	ve := NewVarianceEngine(spec)
	results, err := ve.AnalyzeFlowToMockRoutes(flow)
	require.NoError(t, err)
	require.Len(t, results, 1)

	route := results[0].Transaction
	assert.Equal(t, config.TypeMockRoute, route.Type)
	assert.Equal(t, "Mock Route 0210_000000_021", route.Name)
	assert.Equal(t, "0200", route.MatchFields["0"])
	assert.Equal(t, "000000", route.MatchFields["3"])
	assert.Equal(t, "021", route.MatchFields["22"])
	assert.Equal(t, "0210", route.ResponseMTI)
	assert.Equal(t, []int{3, 7, 11, 22}, route.EchoFields)
	assert.Equal(t, "00", route.ResponseFields["39"])
	assert.Equal(t, "auth_code", route.ResponseFields["38"])
}

func TestFindAvailablePCAPFiles(t *testing.T) {
	files := utils.FindAvailablePCAPFiles()
	require.NotEmpty(t, files)
	assert.Contains(t, files[len(files)-1], "Custom Path...")
}

func TestAnalyzeFlowToMockRoutesWithCompositeFields(t *testing.T) {
	spec, err := utils.CreateSpecFromFile("../../specs/example_composed_emv.json")
	require.NoError(t, err)

	respMsg := iso8583.NewMessage(spec)
	respMsg.MTI("0210")
	respMsg.Field(3, "000000")
	respMsg.Field(39, "00")

	compField55 := spec.Fields[55]
	require.NotNil(t, compField55)

	instance := field.NewInstanceOf(compField55)
	comp, ok := instance.(*field.Composite)
	require.True(t, ok)

	err = comp.MarshalPath("9F26", "11223344")
	require.NoError(t, err)
	err = comp.MarshalPath("9F27", "8")
	require.NoError(t, err)

	packed55, err := comp.Bytes()
	require.NoError(t, err)
	respMsg.BinaryField(55, packed55)

	flow := &CapturedFlow{
		MTI:      "0210",
		DE3:      "000000",
		Messages: []*iso8583.Message{respMsg},
		Count:    1,
	}

	ve := NewVarianceEngine(spec)
	results, err := ve.AnalyzeFlowToMockRoutes(flow)
	require.NoError(t, err)
	require.Len(t, results, 1)

	route := results[0].Transaction
	f55, exists := route.ResponseFields["55"]
	require.True(t, exists)

	f55Map, isMap := f55.(map[string]interface{})
	require.True(t, isMap, "Composite response field 55 should be a map of subfields, not raw string")
	assert.Equal(t, "11223344", f55Map["9F26"])
	assert.Equal(t, "8", f55Map["9F27"])
}


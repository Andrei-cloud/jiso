package base2

import (
	"encoding/hex"
	"fmt"
	"io"
	"net"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	json "github.com/goccy/go-json"
	"github.com/moov-io/iso8583"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"jiso/internal/config"
	"jiso/internal/db"
	"jiso/internal/server"
	"jiso/internal/utils"
)

func TestEndToEndVisaMockServerAndCTFExport(t *testing.T) {
	// 1. Initialize SQLite Database
	tmpDir := t.TempDir()
	dbPath := filepath.Join(tmpDir, "e2e_visa_ctf.db")
	outputCTF := filepath.Join(tmpDir, "e2e_clearing.ctf")

	config.GetConfig().SetDbPath(dbPath)
	require.NoError(t, db.InitDB(dbPath))
	defer func() {
		_ = db.Close()
		config.GetConfig().SetDbPath("")
	}()

	// 2. Load Visa Spec
	visaSpecPath := filepath.Join("..", "..", "..", "specs", "visa.json")
	spec, err := utils.CreateSpecFromFile(visaSpecPath)
	require.NoError(t, err)
	require.NotNil(t, spec)

	// 3. Configure and Start Mock Server with Visa Routes
	routes := []config.MockRouteConfig{
		{
			Name: "Visa Auth Approval",
			MatchFields: map[string]interface{}{
				"0": "0100",
			},
			ResponseMTI: "0110",
			EchoFields:  []int{2, 3, 4, 11, 37, 41, 42, 43, 49},
			ResponseFields: map[string]interface{}{
				"38": "123456",
				"39": "00",
				"62": "123456789012345",
			},
			LatencyMs: 0,
		},
	}

	srvPort := "19899"
	srv := server.NewServer(spec, routes, "binary2")
	err = srv.Start(srvPort)
	require.NoError(t, err)
	defer func() {
		_ = srv.Stop()
	}()
	require.True(t, srv.IsRunning())

	sessionID := "e2e-session-visa-ctf-01"
	require.NoError(t, db.UpsertSession(sessionID, visaSpecPath, "visa.json", "tx.json", "tx.json", "127.0.0.1", srvPort, "CLIENT", "binary2", "active", false))

	// 4. Connect Client to Mock Server
	conn, err := net.Dial("tcp", "127.0.0.1:"+srvPort)
	require.NoError(t, err)
	defer conn.Close()

	testTxData := []struct {
		name     string
		pan      string
		procCode string
		amount   string
	}{
		{"Visa Purchase 1", "4000000000000002", "000000", "000000010000"},
		{"Visa Cash Advance", "4000000000000002", "010000", "000000025000"},
		{"Visa Purchase 2", "4111110000000001", "000000", "000000005000"},
	}

	for i, td := range testTxData {
		req := iso8583.NewMessage(spec)
		req.MTI("0100")
		require.NoError(t, req.Field(2, td.pan))
		require.NoError(t, req.Field(3, td.procCode))
		require.NoError(t, req.Field(4, td.amount))
		require.NoError(t, req.Field(11, fmt.Sprintf("%06d", i+1)))
		require.NoError(t, req.Field(37, fmt.Sprintf("2630600000%02d", i+1)))
		require.NoError(t, req.Field(41, "TERM0001"))
		require.NoError(t, req.Field(42, "MERCH0000000001"))
		require.NoError(t, req.Field(43, "TEST MERCHANT 1          TEST CITY    US"))
		require.NoError(t, req.Field(49, "840"))

		reqPacked, err := req.Pack()
		require.NoError(t, err)

		// Send with 2-byte header
		header := []byte{byte(len(reqPacked) >> 8), byte(len(reqPacked) & 0xFF)}
		_, err = conn.Write(append(header, reqPacked...))
		require.NoError(t, err)

		// Read response
		respLenBuf := make([]byte, 2)
		_, err = io.ReadFull(conn, respLenBuf)
		require.NoError(t, err)
		respLen := (int(respLenBuf[0]) << 8) | int(respLenBuf[1])

		respData := make([]byte, respLen)
		_, err = io.ReadFull(conn, respData)
		require.NoError(t, err)

		resp := iso8583.NewMessage(spec)
		require.NoError(t, resp.Unpack(respData))

		respCode, err := resp.GetString(39)
		require.NoError(t, err)
		assert.Equal(t, "00", respCode)

		// Build JSON representations for SQLite recording
		reqMap := map[string]interface{}{
			"mti": "0100",
			"fields": map[string]interface{}{
				"2":  td.pan,
				"3":  td.procCode,
				"4":  td.amount,
				"11": fmt.Sprintf("%06d", i+1),
				"37": fmt.Sprintf("2630600000%02d", i+1),
				"41": "TERM0001",
				"42": "MERCH0000000001",
				"43": "TEST MERCHANT 1          TEST CITY    840",
				"49": "840",
			},
		}
		respMap := map[string]interface{}{
			"mti": "0110",
			"fields": map[string]interface{}{
				"38": "123456",
				"39": "00",
				"62": "123456789012345",
			},
		}

		reqJSON, _ := json.Marshal(reqMap)
		respJSON, _ := json.Marshal(respMap)
		respJSONStr := string(respJSON)
		reqHex := hex.EncodeToString(reqPacked)
		respHex := hex.EncodeToString(respData)

		require.NoError(t, db.InsertTransactionEnriched(&db.EnrichedTransactionRecord{
			SessionID:        sessionID,
			TxName:           td.name,
			Timestamp:        time.Now().UTC(),
			Success:          true,
			ResponseCode:     respCode,
			RequestJSON:      string(reqJSON),
			ResponseJSON:     &respJSONStr,
			RequestRawHEX:    reqHex,
			ResponseRawHEX:   &respHex,
			ProcessingTimeMs: 15,
		}))
	}

	// 5. Generate Base II CTF File for Session (All Transactions)
	sessionRec, err := db.GetSessionByID(sessionID)
	require.NoError(t, err)

	approvedTxs, err := db.GetApprovedVisaTransactions(sessionID)
	require.NoError(t, err)
	require.Equal(t, 3, len(approvedTxs))

	now := time.Date(2026, 11, 2, 14, 23, 38, 0, time.UTC)
	optsAll := GeneratorOptions{
		CIB:            "400129",
		BatchNumber:    1,
		GenerationTime: now,
	}

	resultAll, err := GenerateCTF(sessionRec, approvedTxs, optsAll)
	require.NoError(t, err)
	require.NotNil(t, resultAll)

	// 3 transactions: each has TCR 0, TCR 1, TCR 5 (= 9 data TCRs) + TC 91 + TC 92 = 11 lines
	assert.Equal(t, 3, resultAll.MonetaryTxCount)
	assert.Equal(t, int64(40000), resultAll.DestinationAmountSum)
	assert.Equal(t, int64(40000), resultAll.SourceAmountSum)
	assert.Equal(t, 11, len(resultAll.Records))

	require.NoError(t, os.WriteFile(outputCTF, resultAll.RawContent, 0644))

	fileBytes, err := os.ReadFile(outputCTF)
	require.NoError(t, err)
	lines := strings.Split(strings.TrimRight(string(fileBytes), "\n"), "\n")
	require.Equal(t, 11, len(lines))

	for idx, line := range lines {
		assert.Equal(t, 168, len(line), "Line %d is not 168 characters: %s", idx+1, line)
	}

	// Verify TC 91 Trailer: Batch total TCRs = 9 data TCRs + 1 trailer = 10 TCRs
	assert.Equal(t, "9100", lines[9][:4])
	assert.Equal(t, "000000000010", lines[9][48:60])
	assert.Equal(t, "000000000003", lines[9][30:42])
	assert.Equal(t, "000000000040000", lines[9][15:30])

	// Verify TC 92 File Trailer
	assert.Equal(t, "9200", lines[10][:4])

	// 6. Test with BIN Filter "411111"
	optsBIN := GeneratorOptions{
		BINFilter:      "411111",
		CIB:            "400129",
		BatchNumber:    1,
		GenerationTime: now,
	}
	resultBIN, err := GenerateCTF(sessionRec, approvedTxs, optsBIN)
	require.NoError(t, err)
	assert.Equal(t, 1, resultBIN.MonetaryTxCount)
	assert.Equal(t, int64(5000), resultBIN.DestinationAmountSum)
	assert.Equal(t, 2, resultBIN.SkippedCount)
	assert.Equal(t, 5, len(resultBIN.Records))
}

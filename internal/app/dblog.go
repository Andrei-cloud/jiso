package app

import (
	"path/filepath"

	"github.com/moov-io/iso8583"

	"jiso/internal/config"
	"jiso/internal/db"
	"jiso/internal/utils"
)

// LogTransactionToDB stores an executed transaction in the session database
// when a database path is configured. Moved from the legacy REPL send path
// so App.Send and the remaining command-layer senders share one
// implementation.
func LogTransactionToDB(sessionID, trxnName string, req, resp *iso8583.Message, processingTimeMs int, success bool) {
	if config.GetConfig().GetDbPath() == "" {
		return
	}
	specPath := config.GetConfig().GetSpec()
	specName := filepath.Base(specPath)
	if specPath == "" {
		specName = ""
	}
	txFilePath := config.GetConfig().GetFile()
	txFileName := filepath.Base(txFilePath)
	if txFilePath == "" {
		txFileName = ""
	}

	spec := utils.ResolveSpec(specPath, utils.GetDefaultSpec())

	var reqJSON string
	var reqRawHex string
	if req != nil {
		jsonStr, err := db.MessageToJSONWithSpec(req, spec)
		if err == nil && jsonStr != "" {
			reqJSON = jsonStr
		} else {
			if packed, pErr := req.Pack(); pErr == nil {
				reqRawHex = utils.HexDump(packed)
			}
		}
	}

	respJSON, respRawHex, responseCode := responseLogFields(resp, spec)

	db.LogTransactionEnriched(&db.TransactionRecord{
		SessionID:        sessionID,
		TxName:           trxnName,
		TxFilePath:       txFilePath,
		TxFileName:       txFileName,
		SpecPath:         specPath,
		SpecName:         specName,
		RequestJSON:      reqJSON,
		ResponseJSON:     respJSON,
		RequestRawHEX:    reqRawHex,
		ResponseRawHEX:   respRawHex,
		ResponseCode:     responseCode,
		ProcessingTimeMs: processingTimeMs,
		Success:          success,
	})
}

// responseLogFields extracts a response message's response code and its JSON
// (or, when that is unavailable, raw-hex) form for the transaction log; both
// representations are nil when there is no response.
func responseLogFields(resp *iso8583.Message, spec *iso8583.MessageSpec) (respJSON, respRawHex *string, responseCode string) {
	if resp == nil {
		return nil, nil, ""
	}
	if f := resp.GetField(39); f != nil {
		if str, err := f.String(); err == nil {
			responseCode = str
		}
	}
	jsonStr, err := db.MessageToJSONWithSpec(resp, spec)
	if err == nil && jsonStr != "" {
		return &jsonStr, nil, responseCode
	}
	if packed, pErr := resp.Pack(); pErr == nil {
		rHex := utils.HexDump(packed)

		return nil, &rHex, responseCode
	}

	return nil, nil, responseCode
}

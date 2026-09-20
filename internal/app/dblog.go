package app

import (
	"encoding/hex"
	"path/filepath"
	"strings"

	"github.com/moov-io/iso8583"

	"jiso/internal/config"
	"jiso/internal/db"
	"jiso/internal/utils"
)

// LogTransactionToDB stores an executed transaction in the session database
// when a database path is configured. Moved from the legacy REPL send path
// so App.Send and the remaining command-layer senders share one
// implementation.
//
// Fields are extracted and recorded against the specification each message
// actually spoke (a stamped transaction composes against its own spec even
// while the session config names another), so the stored JSON, the recorded
// wire hex and the spec_name the CTF eligibility filter reads all describe
// the dialect the link carried rather than whatever was configured last.
func LogTransactionToDB(sessionID, trxnName string, req, resp *iso8583.Message, processingTimeMs int, success bool) {
	cfg := config.GetConfig()
	if cfg.GetDbPath() == "" {
		return
	}
	cfgSpecPath := cfg.GetSpec()
	specPath := cfgSpecPath
	specName := filepath.Base(cfgSpecPath)
	if cfgSpecPath == "" {
		specName = ""
	}
	if own := messageSpecName(req); own != "" {
		specName = own
	} else if own := messageSpecName(resp); own != "" {
		specName = own
	}
	txFilePath := cfg.GetFile()
	txFileName := filepath.Base(txFilePath)
	if txFilePath == "" {
		txFileName = ""
	}

	var reqJSON, reqRawHex string
	if req != nil {
		if jsonStr, err := db.MessageToJSONWithSpec(req, messageSpecOrDefault(req, cfgSpecPath)); err == nil && jsonStr != "" {
			reqJSON = jsonStr
		}
		reqRawHex = packedHex(req)
	}

	respJSON, respRawHex, responseCode := responseLogFields(resp, cfgSpecPath)

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

// messageSpecOrDefault returns the spec the message was composed or unpacked
// against, falling back to the session config's resolved spec.
func messageSpecOrDefault(m *iso8583.Message, cfgSpecPath string) *iso8583.MessageSpec {
	if m != nil {
		if s := m.GetSpec(); s != nil {
			return s
		}
	}
	return utils.ResolveSpec(cfgSpecPath, utils.GetDefaultSpec())
}

// messageSpecName returns the name the message's own spec carries (e.g.
// "ISO8583_VISA"), "" when the message has no spec of its own.
func messageSpecName(m *iso8583.Message) string {
	if m == nil {
		return ""
	}
	if s := m.GetSpec(); s != nil {
		return s.Name
	}

	return ""
}

// packedHex renders the message's wire bytes as plain hex, "" when the
// message cannot be packed. Plain hex is what reconstructFromHEX consumes;
// legacy rows may still hold a formatted HexDump blob.
func packedHex(m *iso8583.Message) string {
	if m == nil {
		return ""
	}
	if packed, err := m.Pack(); err == nil {
		return strings.ToUpper(hex.EncodeToString(packed))
	}

	return ""
}

// responseLogFields extracts a response message's response code and its JSON
// (or, when that is unavailable, raw-hex) form for the transaction log; the
// response code is "" when there is no response.
func responseLogFields(resp *iso8583.Message, cfgSpecPath string) (respJSON, respRawHex *string, responseCode string) {
	if resp == nil {
		return nil, nil, ""
	}
	if f := resp.GetField(39); f != nil {
		if str, err := f.String(); err == nil {
			responseCode = str
		}
	}
	jsonStr, err := db.MessageToJSONWithSpec(resp, messageSpecOrDefault(resp, cfgSpecPath))
	if err == nil && jsonStr != "" {
		rHex := packedHex(resp)
		return &jsonStr, &rHex, responseCode
	}
	if rHex := packedHex(resp); rHex != "" {
		return nil, &rHex, responseCode
	}

	return nil, nil, responseCode
}

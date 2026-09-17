package cmd

import (
	"errors"
	"fmt"
	"time"

	"jiso/internal/app"
	cfg "jiso/internal/config"
	"jiso/internal/transactions"
)

// stressSender adapts the App façade to the app.WorkerSender contract the
// worker manager drives. It mirrors the legacy SendCommand.ExecuteBackground
// (same pseudo response codes on failure paths, same repository/DB logging
// semantics) but goes through Service.SendAsync, whose pending-request
// correlation by STAN the connection manager already performs — the legacy
// interactive path re-checked the STAN after a synchronous Send, which is
// redundant here.
type stressSender struct {
	a  *app.App
	tc transactions.Repository
}

func newStressSender(a *app.App, tc transactions.Repository) *stressSender {
	return &stressSender{a: a, tc: tc}
}

// StartClock is a no-op headless-side: the stress worker computes its own
// TPS/latency stats; the legacy TransactionStats clock only fed the
// interactive worker-status view.
func (s *stressSender) StartClock() {}

// ExecuteBackground runs one async send and reports the response code,
// execution time, and failure. An offline connection keeps the legacy
// semantics: "OFFLINE" with a nil error (a skipped send, not a failure).
func (s *stressSender) ExecuteBackground(trxnName string, skipValidation bool, sessionID string) (string, time.Duration, error) {
	svc := s.a.Service()
	if svc == nil || !svc.IsConnected() {
		return "OFFLINE", 0, nil
	}

	msg, err := s.tc.Compose(trxnName)
	if err != nil {
		s.tc.LogTransaction(trxnName, false)

		return "COMPOSE_ERR", 0, err
	}

	if !skipValidation {
		if err := app.ValidateMessage(msg); err != nil {
			s.tc.LogTransaction(trxnName, false)

			return "VALIDATION_ERR", 0, fmt.Errorf("message validation failed: %w", err)
		}
	}

	logSessionID := sessionID
	if logSessionID == "" {
		logSessionID = cfg.GetConfig().GetSessionID()
	}

	executionStart := time.Now()

	responseChan, err := svc.SendAsync(msg, trxnName)
	if err != nil {
		s.tc.LogTransaction(trxnName, false)
		app.LogTransactionToDB(logSessionID, trxnName, msg, nil, 0, false)

		if stats := s.a.NetworkingStats(); stats != nil {
			stats.RecordError(app.IsRetriableError(err))
		}

		return "SEND_ERR", time.Since(executionStart), err
	}

	// The connection manager delivers the STAN-correlated response, or
	// nil when the response timeout fired.
	resp := <-responseChan
	execTime := time.Since(executionStart)

	if resp == nil {
		s.tc.LogTransaction(trxnName, false)
		app.LogTransactionToDB(logSessionID, trxnName, msg, nil, int(execTime.Milliseconds()), false)

		return "TIMEOUT", execTime, fmt.Errorf("response timeout for transaction %s", trxnName)
	}

	rc := resp.GetField(39)
	if rc == nil {
		return "MISSING_RC", execTime, errors.New("response code field 39 missing")
	}

	rcStr, err := rc.String()
	if err != nil {
		s.tc.LogTransaction(trxnName, false)
		app.LogTransactionToDB(logSessionID, trxnName, msg, nil, 0, false)

		return "RC_PARSE_ERR", execTime, err
	}

	s.tc.LogTransaction(trxnName, true)
	app.LogTransactionToDB(logSessionID, trxnName, msg, resp, int(execTime.Milliseconds()), true)

	return rcStr, execTime, nil
}

// WireWorkerSender wires an App that was not built by the cobra CLI or
// the service path (the TUI) with the same headless worker sender the
// `jiso stress` command uses, so background-send and stress workers can
// start. Without it every TUI worker start fails with "send command not
// found or has wrong type".
func WireWorkerSender(a *app.App) {
	// Resolve per start so a tx-file reload (§L apply) is picked up,
	// mirroring the CLI's "current send command" resolver semantics.
	a.SetWorkerSenderResolver(func() app.WorkerSender {
		return newStressSender(a, a.Transactions())
	})
}

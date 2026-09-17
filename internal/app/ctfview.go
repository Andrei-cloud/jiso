// ctfview.go is the §K CTF-export façade. It reuses exactly
// the headless path — db.OpenExisting reads, the db.GetVisaSessions
// eligibility filter `ctf list` uses, db.GetApprovedVisaTransactions, and
// base2.GenerateCTF — no record generation is re-implemented here. The
// TUI drives these methods off the UI thread (tea.Cmd); the CLI keeps
// its own cmd/ctf.go entry. PreviewExport is the dry path (dryRun=true,
// nothing touched); WriteExport is the -o write (os.WriteFile of
// the cleaned path — a missing directory surfaces as the write error,
// never a silently created tree). §N3 overwrite confirmation is a
// frontend concern; the summary reports Overwrote when the target
// already existed. Unknown session and no-eligible-tx are typed
// ConfigErrors naming the id — records are never fabricated. Missing or
// unset DB reuses the §I typed errors (ErrDBNotConfigured / a
// ConfigError wrapping db.ErrDBNotFound); no path ever creates a file.
package app

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	"jiso/internal/clearing/base2"
	"jiso/internal/db"
)

// DefaultCtfCIB is the CIB default (the `ctf export --cib` default).
const DefaultCtfCIB = "400129"

// CtfSessionView is one CTF-eligible session row: the same rows
// db.GetVisaSessions returns (Visa spec/path/header match), with the
// approved count (success + RC 00/000/empty) `ctf list` prints.
type CtfSessionView struct {
	SessionID      string    `json:"session_id"`
	StartTime      time.Time `json:"start_time"`
	LastActiveTime time.Time `json:"last_active_time"`
	ApprovedCount  int       `json:"approved_count"`
}

// CtfExportSummary is the machine-readable §K result (the ctfExportSummary
// shape emits, plus the first/last record preview strings the
// overlay shows). DryRun=true + Written=false carry the dry contract.
type CtfExportSummary struct {
	SessionID            string `json:"session_id"`
	OutputPath           string `json:"output_path"`
	DryRun               bool   `json:"dry_run,omitempty"`
	Written              bool   `json:"written"`
	Overwrote            bool   `json:"overwrote,omitempty"`
	FileSizeBytes        int    `json:"file_size_bytes"`
	Records              int    `json:"records"`
	MonetaryTransactions int    `json:"monetary_transactions"`
	TotalTCRs            int    `json:"total_tcrs"`
	DestinationAmountSum int64  `json:"destination_amount_sum_raw"`
	SourceAmountSum      int64  `json:"source_amount_sum_raw"`
	CIB                  string `json:"cib"`
	ProcessingDate       string `json:"processing_date"`
	SkippedTransactions  int    `json:"skipped_transactions,omitempty"`
	ApprovedTransactions int    `json:"approved_transactions"`
	FirstRecord          string `json:"first_record,omitempty"`
	LastRecord           string `json:"last_record,omitempty"`

	// RecordLines carries every record string the write emits — the §K
	// record viewer shows all of them. It is
	// deliberately NOT part of the JSON: the CLI ctf export --json
	// wire shape stays byte-stable with the first/last pair only.
	// (Records already names the COUNT in that wire shape.)
	RecordLines []string `json:"-"`
}

// ListCtfSessions returns the CTF-eligible sessions newest-activity-
// first (the db.GetVisaSessions order `ctf list` shows). A missing or
// unset DB surfaces the same typed errors as the §I read path.
func (a *App) ListCtfSessions(ctx context.Context) ([]CtfSessionView, error) {
	closeRead, err := a.openRead(ctx)
	if err != nil {
		return nil, err
	}
	defer closeRead()

	records, err := db.GetVisaSessions()
	if err != nil {
		return nil, fmt.Errorf("failed to fetch Visa sessions: %w", err)
	}

	views := make([]CtfSessionView, 0, len(records))
	for _, rec := range records {
		if rec == nil {
			continue
		}
		views = append(views, CtfSessionView{
			SessionID:      rec.SessionID,
			StartTime:      rec.StartTime,
			LastActiveTime: rec.LastActiveTime,
			ApprovedCount:  rec.SuccessCount,
		})
	}

	return views, nil
}

// generateCTF is the shared leg behind PreviewExport and WriteExport:
// open read-only, resolve the session (unknown → ConfigError naming the
// id), load approved txs (none → ConfigError naming the id), apply the
// defaults, and run base2.GenerateCTF. The caller supplies now so
// the record's GenerationTime (and the derived processing date) is stamped
// from a single deterministic clock read rather than a time.Now buried in
// the façade. Nothing is written here.
func (a *App) generateCTF(ctx context.Context, sessionID, cib, binFilter string, batch int, now time.Time) (*base2.CTFResult, int, error) {
	sessionID = strings.TrimSpace(sessionID)
	if sessionID == "" {
		return nil, 0, &ConfigError{Path: sessionID, Err: errors.New("session id is required")}
	}

	closeRead, err := a.openRead(ctx)
	if err != nil {
		return nil, 0, err
	}
	defer closeRead()

	session, err := db.GetSessionByID(sessionID)
	if err != nil {
		if errors.Is(err, db.ErrSessionNotFound) {
			return nil, 0, &ConfigError{Path: sessionID, Err: errors.New("unknown session")}
		}

		return nil, 0, fmt.Errorf("failed to load session %s: %w", sessionID, err)
	}

	txs, err := db.GetApprovedVisaTransactions(sessionID)
	if err != nil {
		return nil, 0, fmt.Errorf("failed to query approved transactions: %w", err)
	}
	if len(txs) == 0 {
		return nil, 0, &ConfigError{Path: sessionID, Err: fmt.Errorf("no approved transactions found in session %s", sessionID)}
	}

	if strings.TrimSpace(cib) == "" {
		cib = DefaultCtfCIB
	}
	if batch <= 0 {
		batch = 1
	}

	result, err := base2.GenerateCTF(session, txs, base2.GeneratorOptions{
		BINFilter:      strings.TrimSpace(binFilter),
		CIB:            cib,
		BatchNumber:    batch,
		GenerationTime: now.UTC(),
	})
	if err != nil {
		// base2 refuses to emit an empty batch (all txs BIN-filtered):
		// surface it as the config class naming the id, never a file.
		if result == nil || result.MonetaryTxCount == 0 {
			return nil, 0, &ConfigError{Path: sessionID, Err: err}
		}

		return nil, 0, fmt.Errorf("CTF generation failed: %w", err)
	}

	return result, len(txs), nil
}

// summaryFromResult builds the view over one generated result (the
// summaryFromCTF shape) with the first/last record previews.
func summaryFromResult(sessionID, outputPath string, result *base2.CTFResult, approved int, dryRun, written bool) *CtfExportSummary {
	s := &CtfExportSummary{
		SessionID:            sessionID,
		OutputPath:           outputPath,
		DryRun:               dryRun,
		Written:              written,
		FileSizeBytes:        len(result.RawContent),
		Records:              len(result.Records),
		MonetaryTransactions: result.MonetaryTxCount,
		TotalTCRs:            result.TotalTCRCount,
		DestinationAmountSum: result.DestinationAmountSum,
		SourceAmountSum:      result.SourceAmountSum,
		CIB:                  result.CIB,
		ProcessingDate:       result.ProcessingDate,
		SkippedTransactions:  result.SkippedCount,
		ApprovedTransactions: approved,
	}
	if n := len(result.Records); n > 0 {
		s.FirstRecord = result.Records[0].String()
		s.LastRecord = result.Records[n-1].String()
		s.RecordLines = make([]string, 0, n)
		for _, r := range result.Records {
			s.RecordLines = append(s.RecordLines, r.String())
		}
	}

	return s
}

// PreviewExport is the dry path (--dry-run): the same summary a
// write would produce — tx count, totals, first/last record strings —
// with nothing written. A missing session or an empty eligible set is a
// typed ConfigError naming the id.
func (a *App) PreviewExport(ctx context.Context, sessionID, cib, binFilter string, batch int) (*CtfExportSummary, error) {
	now := time.Now().UTC()
	result, approved, err := a.generateCTF(ctx, sessionID, cib, binFilter, batch, now)
	if err != nil {
		return nil, err
	}

	return summaryFromResult(sessionID, ctfOutputPath("", now), result, approved, true, false), nil
}

// WriteExport is the -o path: generate, then os.WriteFile the
// cleaned output path (0644). A blank outPath gets the default
// name; a write into a missing directory fails with the OS error naming
// the path — no directory tree is created. Overwrote reports the target
// existed before (the frontend asks §N3 confirm before calling).
func (a *App) WriteExport(ctx context.Context, sessionID, cib, binFilter string, batch int, outPath string) (*CtfExportSummary, error) {
	now := time.Now().UTC()
	result, approved, err := a.generateCTF(ctx, sessionID, cib, binFilter, batch, now)
	if err != nil {
		return nil, err
	}

	cleanPath := filepath.Clean(ctfOutputPath(outPath, now))
	_, statErr := os.Stat(cleanPath)
	overwrote := statErr == nil

	if err := os.WriteFile(cleanPath, result.RawContent, 0o644); err != nil {
		return nil, fmt.Errorf("failed to write CTF file to %s: %w", cleanPath, err)
	}

	summary := summaryFromResult(sessionID, cleanPath, result, approved, false, true)
	summary.Overwrote = overwrote

	return summary, nil
}

// ctfOutputPath resolves the write target: an explicit path wins; a
// blank one becomes the default name, stamped from the caller's now
// (the same clock read that produced the records) rather than a fresh
// time.Now inside the path builder.
func ctfOutputPath(outPath string, now time.Time) string {
	if strings.TrimSpace(outPath) != "" {
		return outPath
	}
	now = now.UTC()

	return fmt.Sprintf("R06_CL%s_AE_VISACTFFile_001O_%s_%s.ctf", DefaultCtfCIB, now.Format("20060102"), now.Format("150405"))
}

package app

import (
	"time"

	"jiso/internal/db"
)

// DbStatsView is the single JSON-serializable shape behind `db stats`:
// the sessions overview, one session's stats (plus its stress runs and
// transaction list), and one transaction's retrospective review. Two
// printers exist today (internal/command/db_stats.go for the REPL and
// internal/cli/cmd/db.go for cobra) with drifted views of the same data;
// both are meant to render this shape once shimmed (E1 review #23). Mode
// tells a frontend which of the three sections is populated:
// "list", "overview", or "transaction".
type DbStatsView struct {
	Mode         string                      `json:"mode"`
	Sessions     []DbSessionView             `json:"sessions,omitempty"`
	Session      *DbSessionView              `json:"session,omitempty"`
	Stats        *DbSessionStats             `json:"stats,omitempty"`
	StressRuns   []DbStressRunView           `json:"stress_runs,omitempty"`
	Transactions []DbTransactionView         `json:"transactions,omitempty"`
	Transaction  *DbTransactionRetrospective `json:"transaction,omitempty"`
}

// DbSessionView is one recorded session, as stored by db.SessionRecord.
type DbSessionView struct {
	SessionID        string    `json:"session_id"`
	StartTime        time.Time `json:"start_time"`
	LastActiveTime   time.Time `json:"last_active_time"`
	SpecPath         string    `json:"spec_path,omitempty"`
	SpecName         string    `json:"spec_name,omitempty"`
	TxFilePath       string    `json:"tx_file_path,omitempty"`
	TxFileName       string    `json:"tx_file_name,omitempty"`
	Host             string    `json:"host,omitempty"`
	Port             string    `json:"port,omitempty"`
	ConnectionType   string    `json:"connection_type,omitempty"`
	HeaderType       string    `json:"header_type,omitempty"`
	TLSEnabled       bool      `json:"tls_enabled"`
	Status           string    `json:"status,omitempty"`
	TransactionCount int       `json:"transaction_count"`
	SuccessCount     int       `json:"success_count"`
	FailedCount      int       `json:"failed_count"`
	StressTestCount  int       `json:"stress_test_count"`
}

// DbSessionStats holds the per-session counters returned by
// db.GetTransactionStats.
type DbSessionStats struct {
	TotalTransactions        int            `json:"total_transactions"`
	SuccessfulTransactions   int            `json:"successful_transactions"`
	FailedTransactions       int            `json:"failed_transactions"`
	AverageProcessingTimeMs  float64        `json:"average_processing_time_ms"`
	ResponseCodeDistribution map[string]int `json:"response_code_distribution,omitempty"`
}

// DbStressRunView is one persisted stress-test run of a session.
type DbStressRunView struct {
	WorkerID               string         `json:"worker_id"`
	StartTime              time.Time      `json:"start_time"`
	EndTime                time.Time      `json:"end_time"`
	TargetTPS              int            `json:"target_tps"`
	Concurrency            int            `json:"concurrency"`
	TotalDuration          time.Duration  `json:"total_duration"`
	TotalTransactions      int            `json:"total_transactions"`
	SuccessfulTransactions int            `json:"successful_transactions"`
	FailedTransactions     int            `json:"failed_transactions"`
	AverageTPS             float64        `json:"average_tps"`
	PeakTPS                float64        `json:"peak_tps"`
	MinLatencyMs           float64        `json:"min_latency_ms"`
	MeanLatencyMs          float64        `json:"mean_latency_ms"`
	MaxLatencyMs           float64        `json:"max_latency_ms"`
	P50LatencyMs           float64        `json:"p50_latency_ms"`
	P90LatencyMs           float64        `json:"p90_latency_ms"`
	P95LatencyMs           float64        `json:"p95_latency_ms"`
	P99LatencyMs           float64        `json:"p99_latency_ms"`
	TransactionNames       []string       `json:"transaction_names,omitempty"`
	ResponseCodes          map[string]int `json:"response_codes,omitempty"`
}

// DbTransactionView is one transaction row of a session overview. MTI is
// populated only by the §I read façade (parsed from the stored request
// JSON); the shared record builder leaves it empty so existing views and
// goldens are untouched.
type DbTransactionView struct {
	ID             int64         `json:"id"`
	SessionID      string        `json:"session_id"`
	Timestamp      time.Time     `json:"timestamp"`
	TxName         string        `json:"transaction_name"`
	TxFileName     string        `json:"tx_file_name,omitempty"`
	SpecName       string        `json:"spec_name,omitempty"`
	MTI            string        `json:"mti,omitempty"`
	ProcessingTime time.Duration `json:"processing_time"`
	Success        bool          `json:"success"`
	ResponseCode   string        `json:"response_code"`
}

// DbTransactionRetrospective is the `db stats tx <id>` review: the stored
// row plus the reconstructed request/response messages.
type DbTransactionRetrospective struct {
	ID             int64                    `json:"id"`
	SessionID      string                   `json:"session_id"`
	Timestamp      time.Time                `json:"timestamp"`
	TxName         string                   `json:"transaction_name"`
	TxFileName     string                   `json:"tx_file_name,omitempty"`
	TxFilePath     string                   `json:"tx_file_path,omitempty"`
	SpecName       string                   `json:"spec_name,omitempty"`
	SpecPath       string                   `json:"spec_path,omitempty"`
	ProcessingTime time.Duration            `json:"processing_time"`
	Success        bool                     `json:"success"`
	ResponseCode   string                   `json:"response_code"`
	HasResponse    bool                     `json:"has_response"`
	Request        *DbMessageReconstruction `json:"request,omitempty"`
	Response       *DbMessageReconstruction `json:"response,omitempty"`
}

// DbMessageReconstruction is the plain rendering output of
// db.Reconstruct for one stored message.
type DbMessageReconstruction struct {
	HEX          string `json:"hex,omitempty"`
	DescribeText string `json:"describe_text,omitempty"`
	RawFallback  bool   `json:"raw_fallback"`
	ParseError   string `json:"parse_error,omitempty"`
}

// NewDbStatsViewFromTransaction builds the transaction retrospective view.
// request and response are the db.Reconstruct outputs for the stored
// columns (nil when reconstruction failed or nothing was recorded).
func NewDbStatsViewFromTransaction(
	record *db.EnrichedTransactionRecord,
	request *db.ReconstructedMessage,
	response *db.ReconstructedMessage,
) *DbStatsView {
	view := &DbStatsView{Mode: "transaction"}

	if record == nil {
		return view
	}

	retrospective := DbTransactionRetrospective{
		ID:             record.ID,
		SessionID:      record.SessionID,
		Timestamp:      record.Timestamp,
		TxName:         record.TxName,
		TxFileName:     record.TxFileName,
		TxFilePath:     record.TxFilePath,
		SpecName:       record.SpecName,
		SpecPath:       record.SpecPath,
		ProcessingTime: time.Duration(record.ProcessingTimeMs) * time.Millisecond,
		Success:        record.Success,
		ResponseCode:   record.ResponseCode,
		HasResponse:    record.ResponseJSON != nil || record.ResponseRawHEX != nil,
	}

	if req := NewDbMessageReconstruction(request); req != nil {
		retrospective.Request = req
	}
	if resp := NewDbMessageReconstruction(response); resp != nil {
		retrospective.Response = resp
	}

	view.Transaction = &retrospective

	return view
}

// NewDbSessionViewFromRecord copies a db.SessionRecord into plain fields.
func NewDbSessionViewFromRecord(record *db.SessionRecord) DbSessionView {
	return DbSessionView{
		SessionID:        record.SessionID,
		StartTime:        record.StartTime,
		LastActiveTime:   record.LastActiveTime,
		SpecPath:         record.SpecPath,
		SpecName:         record.SpecName,
		TxFilePath:       record.TxFilePath,
		TxFileName:       record.TxFileName,
		Host:             record.Host,
		Port:             record.Port,
		ConnectionType:   record.ConnectionType,
		HeaderType:       record.HeaderType,
		TLSEnabled:       record.TLSEnabled,
		Status:           record.Status,
		TransactionCount: record.TransactionCount,
		SuccessCount:     record.SuccessCount,
		FailedCount:      record.FailedCount,
		StressTestCount:  record.StressTestCount,
	}
}

// NewDbSessionStatsFromStatsMap extracts the counters db.GetTransactionStats
// stores under total_transactions, successful_transactions,
// failed_transactions, average_processing_time_ms, and
// response_code_distribution. Missing or unexpected keys stay zero.
func NewDbSessionStatsFromStatsMap(stats map[string]any) DbSessionStats {
	view := DbSessionStats{}
	if stats == nil {
		return view
	}

	view.TotalTransactions = statsInt(stats, "total_transactions")
	view.SuccessfulTransactions = statsInt(stats, "successful_transactions")
	view.FailedTransactions = statsInt(stats, "failed_transactions")
	view.AverageProcessingTimeMs = statsFloat(stats, "average_processing_time_ms")

	switch codes := stats["response_code_distribution"].(type) {
	case map[string]int:
		if len(codes) > 0 {
			view.ResponseCodeDistribution = make(map[string]int, len(codes))
			for code, count := range codes {
				view.ResponseCodeDistribution[code] = count
			}
		}
	case map[string]any:
		if len(codes) > 0 {
			view.ResponseCodeDistribution = make(map[string]int, len(codes))
			for code, raw := range codes {
				view.ResponseCodeDistribution[code] = int(statsValueFloat(raw))
			}
		}
	}

	return view
}

// NewDbTransactionViewFromRecord copies one session transaction row.
func NewDbTransactionViewFromRecord(record *db.EnrichedTransactionRecord) DbTransactionView {
	return DbTransactionView{
		ID:             record.ID,
		SessionID:      record.SessionID,
		Timestamp:      record.Timestamp,
		TxName:         record.TxName,
		TxFileName:     record.TxFileName,
		SpecName:       record.SpecName,
		ProcessingTime: time.Duration(record.ProcessingTimeMs) * time.Millisecond,
		Success:        record.Success,
		ResponseCode:   record.ResponseCode,
	}
}

// NewDbMessageReconstruction copies a db.ReconstructedMessage output into
// plain rendering fields; nil in, nil out.
func NewDbMessageReconstruction(message *db.ReconstructedMessage) *DbMessageReconstruction {
	if message == nil {
		return nil
	}

	return &DbMessageReconstruction{
		HEX:          message.HEX,
		DescribeText: message.DescribeText,
		RawFallback:  message.IsRawFallback,
		ParseError:   message.ParseError,
	}
}

func statsInt(stats map[string]any, key string) int {
	return int(statsValueFloat(stats[key]))
}

func statsFloat(stats map[string]any, key string) float64 {
	return statsValueFloat(stats[key])
}

func statsValueFloat(value any) float64 {
	switch v := value.(type) {
	case int:
		return float64(v)
	case int64:
		return float64(v)
	case float64:
		return v
	default:
		return 0
	}
}

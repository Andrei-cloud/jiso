package transactions

import (
	"encoding/json"
	"sync"
	"time"

	cfg "jiso/internal/config"

	"github.com/moov-io/iso8583"
)

const (
	transactionCacheFile = "transaction_cache.json"
)

type transactionParsedCache struct {
	once         sync.Once
	fieldMap     map[int]interface{}
	staticFields map[int]interface{}
	autoFields   map[int]string
}

type Transaction struct {
	Name        string                  `json:"name"`
	Description string                  `json:"description"`
	Spec        string                  `json:"spec,omitempty"`
	Fields      json.RawMessage         `json:"fields"`
	Dataset     []map[int]string        `json:"dataset"`
	DatasetName string                  `json:"dataset_name"`
	parsedCache *transactionParsedCache `json:"-"`
}

type Dataset struct {
	Name string              `json:"name"`
	Data []map[string]string `json:"data"`
}

type Scenario struct {
	Name        string         `json:"name"`
	Description string         `json:"description"`
	DatasetName string         `json:"dataset_name"`
	Steps       []ScenarioStep `json:"steps"`
}

type ScenarioStep struct {
	Name             string                 `json:"name"`
	UseTransactionId string                 `json:"use_transaction_id"`
	Fields           map[string]interface{} `json:"fields"`
	Extract          map[string]string      `json:"extract"`
	Validate         []Assertion            `json:"validate"`
}

type Assertion struct {
	Field  string `json:"field"`
	Expect string `json:"expect,omitempty"`
	Regex  string `json:"regex,omitempty"`
	Exists *bool  `json:"exists,omitempty"`
}

type ConfigItem struct {
	Type           string                 `json:"type"`
	Name           string                 `json:"name"`
	Description    string                 `json:"description"`
	Spec           string                 `json:"spec,omitempty"`
	SpecFile       string                 `json:"spec_file,omitempty"`
	Fields         json.RawMessage        `json:"fields,omitempty"`
	Dataset        []map[int]string       `json:"dataset,omitempty"`
	Data           []map[string]string    `json:"data,omitempty"`
	DatasetName    string                 `json:"dataset_name,omitempty"`
	Steps          []ScenarioStep         `json:"steps,omitempty"`
	MatchFields    map[string]interface{} `json:"match_fields,omitempty"`
	RequiredFields []string               `json:"required_fields,omitempty"`
	EchoFields     []int                  `json:"echo_fields,omitempty"`
	ResponseMTI    string                 `json:"response_mti,omitempty"`
	ResponseFields map[string]string      `json:"response_fields,omitempty"`
	DelayMs        int                    `json:"delay_ms,omitempty"`
	LatencyMs      int                    `json:"latency_ms,omitempty"`
	JitterMs       int                    `json:"jitter_ms,omitempty"`
	DropConnection bool                   `json:"drop_connection,omitempty"`
}

// TransactionState stores information about transaction state
type TransactionState struct {
	LastUsedDataset map[string]int   `json:"last_used_dataset"` // Maps transaction names to last used dataset index
	TransactionLogs []TransactionLog `json:"transaction_logs"`  // Store recent transaction logs
}

// TransactionLog tracks usage of transactions
type TransactionLog struct {
	Name      string    `json:"name"`
	Timestamp time.Time `json:"timestamp"`
	Success   bool      `json:"success"`
}

// Add a cache for quick transaction lookups
type TransactionCollection struct {
	spec         *iso8583.MessageSpec
	transactions []Transaction
	cache        map[string]*Transaction // Add transaction cache
	datasets     map[string]*Dataset
	scenarios    map[string]*Scenario

	mockRoutes []cfg.MockRouteConfig

	// State management
	state         TransactionState
	stateLock     sync.RWMutex
	saveLock      sync.Mutex // Protects against concurrent saves
	persistDir    string
	lastSavedUnix int64
}

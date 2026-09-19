package transactions

import (
	"encoding/json"
	"sync"
	"time"

	"github.com/moov-io/iso8583"

	cfg "jiso/internal/config"
)

const (
	transactionCacheFile = "transaction_cache.json"
)

type transactionParsedCache struct {
	mu           sync.Mutex
	done         bool
	err          error
	fieldMap     map[int]any
	staticFields map[int]any
	autoFields   map[int]string
}

// Transaction is one named message an operator can send: the field defaults as
// authored in the transaction file, plus the dataset its dynamic values draw
// from. Fields stay raw JSON until a send needs them, so a large file costs no
// parse time for the transactions nobody uses.
type Transaction struct {
	Name        string                  `json:"name"`
	Description string                  `json:"description"`
	Spec        string                  `json:"spec,omitempty"`
	Fields      json.RawMessage         `json:"fields"`
	Dataset     []map[int]string        `json:"dataset"`
	DatasetName string                  `json:"dataset_name"`
	parsedCache *transactionParsedCache // derived at first compose, never in the file
}

// Dataset is a named set of rows a Transaction or Scenario draws from, one row
// per message composed while rotating through it.
type Dataset struct {
	Name string              `json:"name"`
	Data []map[string]string `json:"data"`
}

// Scenario is an ordered set of steps run over one connection, sharing session
// state so a value extracted by one step can be sent by the next.
type Scenario struct {
	Name        string `json:"name"`
	Description string `json:"description"`
	DatasetName string `json:"dataset_name"`
	// Spec is the specification this scenario was captured/declared
	// with (the same provenance its step transactions carry); "" when
	// the file declared none. The §F spec gate seats its browse on it.
	Spec  string         `json:"spec,omitempty"`
	Steps []ScenarioStep `json:"steps"`
}

// ScenarioStep is one step: which transaction to compose, the overrides that
// make this message differ from the base transaction, which values to extract
// from the reply, and which assertions must hold.
type ScenarioStep struct {
	Name             string            `json:"name"`
	UseTransactionID string            `json:"use_transaction_id"`
	Fields           map[string]any    `json:"fields"`
	Extract          map[string]string `json:"extract"`
	Validate         []Assertion       `json:"validate"`
}

// Assertion is one check on a step reply: Expect compares a field to a literal,
// Regex matches it, Exists only requires it to be present. A failure is reported
// by field name, not just as "the step failed".
type Assertion struct {
	Field  string `json:"field"`
	Expect string `json:"expect,omitempty"`
	Regex  string `json:"regex,omitempty"`
	Exists *bool  `json:"exists,omitempty"`
}

// ConfigItem is the config package's polymorphic entry, aliased so the
// transactions, datasets and scenarios in one file can all be read with a single
// type. The name stays here because transactions.ConfigItem does not stutter.
type ConfigItem = cfg.Item

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

// TransactionCollection holds the transactions, datasets and scenarios from one
// transaction file and composes messages from them. Lookups are cached and the
// parse is lazy because the TUI opens the file at startup and renders the list
// before anyone sends: paying to parse every transaction up front would charge
// startup time for work that may never happen.
type TransactionCollection struct {
	spec         *iso8583.MessageSpec
	transactions []Transaction
	cache        map[string]*Transaction // name -> transaction, filled on first lookup
	cacheMu      sync.RWMutex            // guards cache; held only for a map read
	parseMu      sync.Mutex              // one goroutine parses a transaction at a time
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

package config

import (
	"bytes"
	"math/rand"
	"sort"
	"strconv"
	"time"

	json "github.com/goccy/go-json"
)

// Discriminator defines the valid configuration item types
type Discriminator string

const (
	// TypeTransaction is a composed message: an MTI plus its field defaults.
	// These four strings are written into saved config files, so changing one is a
	// file-format change that breaks files already on disk, not a rename.
	TypeTransaction Discriminator = "transaction"
	// TypeDataset is a named set of rows a transaction or scenario draws values from.
	TypeDataset Discriminator = "dataset"
	// TypeScenario is an ordered run of steps that share extracted values.
	TypeScenario Discriminator = "scenario"
	// TypeMockRoute is a canned reply the embedded server answers with.
	TypeMockRoute Discriminator = "mock_route"
)

// MockRouteConfig defines configuration for embedded mock server response routes
type MockRouteConfig struct {
	Name           string         `json:"name"`
	Description    string         `json:"description,omitempty"`
	MatchFields    map[string]any `json:"match_fields,omitempty"`
	RequiredFields []string       `json:"required_fields,omitempty"`
	EchoFields     []int          `json:"echo_fields,omitempty"`
	ResponseMTI    string         `json:"response_mti,omitempty"`
	ResponseFields map[string]any `json:"response_fields,omitempty"`
	DelayMs        int            `json:"delay_ms,omitempty"`
	LatencyMs      int            `json:"latency_ms,omitempty"`
	JitterMs       int            `json:"jitter_ms,omitempty"`
	DropConnection bool           `json:"drop_connection,omitempty"`
}

// Item represents a polymorphic configuration entry in the flat configuration array
type Item struct {
	Type           Discriminator       `json:"type,omitempty"`
	Name           string              `json:"name"`
	Description    string              `json:"description,omitempty"`
	Spec           string              `json:"spec,omitempty"`
	SpecFile       string              `json:"spec_file,omitempty"`
	Fields         json.RawMessage     `json:"fields,omitempty"`
	Dataset        []map[int]string    `json:"dataset,omitempty"`
	Data           []map[string]string `json:"data,omitempty"`
	DatasetName    string              `json:"dataset_name,omitempty"`
	Steps          json.RawMessage     `json:"steps,omitempty"`
	MatchFields    map[string]any      `json:"match_fields,omitempty"`
	RequiredFields []string            `json:"required_fields,omitempty"`
	EchoFields     []int               `json:"echo_fields,omitempty"`
	ResponseMTI    string              `json:"response_mti,omitempty"`
	ResponseFields map[string]any      `json:"response_fields,omitempty"`
	DelayMs        int                 `json:"delay_ms,omitempty"`
	LatencyMs      int                 `json:"latency_ms,omitempty"`
	JitterMs       int                 `json:"jitter_ms,omitempty"`
	DropConnection bool                `json:"drop_connection,omitempty"`
}

// OrderedMap represents a map with preserved, sorted key insertion order for JSON marshaling
type OrderedMap struct {
	keys   []string
	values map[string]any
}

// NewOrderedMap returns an empty map that remembers the order keys were added.
func NewOrderedMap() *OrderedMap {
	return &OrderedMap{
		keys:   make([]string, 0),
		values: make(map[string]any),
	}
}

// Set stores value under key, recording the key in the order only the first
// time, so re-setting a value does not move it in the output.
func (om *OrderedMap) Set(key string, value any) {
	if _, exists := om.values[key]; !exists {
		om.keys = append(om.keys, key)
	}
	om.values[key] = value
}

// MarshalJSON emits the object in insertion order instead of the alphabetical
// order encoding/json picks, which is what keeps a saved config file stable
// enough to diff between two edits.
func (om *OrderedMap) MarshalJSON() ([]byte, error) {
	var buf bytes.Buffer
	buf.WriteByte('{')
	for i, k := range om.keys {
		if i > 0 {
			buf.WriteByte(',')
		}
		keyBytes, err := json.Marshal(k)
		if err != nil {
			return nil, err
		}
		buf.Write(keyBytes)
		buf.WriteByte(':')
		valBytes, err := json.Marshal(om.values[k])
		if err != nil {
			return nil, err
		}
		buf.Write(valBytes)
	}
	buf.WriteByte('}')
	return buf.Bytes(), nil
}

// SortMapKeysRecursively takes a map or slice or primitive any and ensures any map[string]any
// with numeric string keys (or subfields/subelements) is sorted in numerical ascending order.
func SortMapKeysRecursively(v any) any {
	switch val := v.(type) {
	case map[string]any:
		type keyVal struct {
			key string
			num int
			val any
		}
		kvs := make([]keyVal, 0, len(val)) // len(val) is known: the map being copied
		for k, elem := range val {
			n, err := strconv.Atoi(k)
			if err != nil {
				n = 999999
			}
			kvs = append(kvs, keyVal{key: k, num: n, val: SortMapKeysRecursively(elem)})
		}
		sort.Slice(kvs, func(i, j int) bool {
			if kvs[i].num != kvs[j].num {
				return kvs[i].num < kvs[j].num
			}
			return kvs[i].key < kvs[j].key
		})

		ordered := NewOrderedMap()
		for _, kv := range kvs {
			ordered.Set(kv.key, kv.val)
		}
		return ordered
	case []any:
		for i, elem := range val {
			val[i] = SortMapKeysRecursively(elem)
		}
		return val
	default:
		return v
	}
}

// SortStringMapKeys sorts a map[string]any into an OrderedMap for numeric ascending JSON output
func SortStringMapKeys(m map[string]any) any {
	if m == nil {
		return nil
	}
	return SortMapKeysRecursively(m)
}

// SortInterfaceMapKeys sorts a map[string]any into an OrderedMap for numeric ascending JSON output
func SortInterfaceMapKeys(m map[string]any) any {
	if m == nil {
		return nil
	}
	return SortMapKeysRecursively(m)
}

// GetTotalDelay calculates base latency + random jitter range in milliseconds
func (m *MockRouteConfig) GetTotalDelay() time.Duration {
	baseDelay := m.DelayMs
	if baseDelay == 0 && m.LatencyMs > 0 {
		baseDelay = m.LatencyMs
	}
	if m.JitterMs <= 0 {
		if baseDelay < 0 {
			return 0
		}
		return time.Duration(baseDelay) * time.Millisecond
	}

	// Random jitter between -JitterMs and +JitterMs
	jitter := rand.Intn(2*m.JitterMs+1) - m.JitterMs
	total := baseDelay + jitter
	if total < 0 {
		return 0
	}
	return time.Duration(total) * time.Millisecond
}

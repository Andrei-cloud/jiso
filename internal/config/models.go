package config

import (
	"bytes"
	"fmt"
	"math/rand"
	"sort"
	"strconv"
	"time"

	json "github.com/goccy/go-json"
)

// ConfigDiscriminator defines the valid configuration item types
type ConfigDiscriminator string

const (
	TypeTransaction ConfigDiscriminator = "transaction"
	TypeDataset     ConfigDiscriminator = "dataset"
	TypeScenario    ConfigDiscriminator = "scenario"
	TypeMockRoute   ConfigDiscriminator = "mock_route"
)

// MockRouteConfig defines configuration for embedded mock server response routes
type MockRouteConfig struct {
	Name           string                 `json:"name"`
	Description    string                 `json:"description,omitempty"`
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

// ConfigItem represents a polymorphic configuration entry in the flat configuration array
type ConfigItem struct {
	Type           ConfigDiscriminator    `json:"type,omitempty"`
	Name           string                 `json:"name"`
	Description    string                 `json:"description,omitempty"`
	Fields         json.RawMessage        `json:"fields,omitempty"`
	Dataset        []map[int]string       `json:"dataset,omitempty"`
	Data           []map[string]string    `json:"data,omitempty"`
	DatasetName    string                 `json:"dataset_name,omitempty"`
	Steps          json.RawMessage        `json:"steps,omitempty"`
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

// GetType returns the item discriminator, defaulting to "transaction" if unassigned
func (c *ConfigItem) GetType() ConfigDiscriminator {
	if c.Type == "" {
		return TypeTransaction
	}
	return c.Type
}

// ParseConfigItems parses a JSON byte slice into a slice of ConfigItem objects
func ParseConfigItems(data []byte) ([]ConfigItem, error) {
	var items []ConfigItem
	if err := json.Unmarshal(data, &items); err != nil {
		return nil, fmt.Errorf("failed to parse config items: %w", err)
	}

	for i := range items {
		if items[i].Type == "" {
			items[i].Type = TypeTransaction
		}
	}
	return items, nil
}

// OrderedMap represents a map with preserved, sorted key insertion order for JSON marshaling
type OrderedMap struct {
	keys   []string
	values map[string]interface{}
}

func NewOrderedMap() *OrderedMap {
	return &OrderedMap{
		keys:   make([]string, 0),
		values: make(map[string]interface{}),
	}
}

func (om *OrderedMap) Set(key string, value interface{}) {
	if _, exists := om.values[key]; !exists {
		om.keys = append(om.keys, key)
	}
	om.values[key] = value
}

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

// SortMapKeysRecursively takes a map or slice or primitive interface{} and ensures any map[string]interface{}
// with numeric string keys (or subfields/subelements) is sorted in numerical ascending order.
func SortMapKeysRecursively(v interface{}) interface{} {
	switch val := v.(type) {
	case map[string]interface{}:
		type keyVal struct {
			key string
			num int
			val interface{}
		}
		var kvs []keyVal
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
	case []interface{}:
		for i, elem := range val {
			val[i] = SortMapKeysRecursively(elem)
		}
		return val
	default:
		return v
	}
}

// SortFieldsJSON normalizes a raw JSON message by sorting all key-value objects in ascending numeric key order
func SortFieldsJSON(raw json.RawMessage) json.RawMessage {
	if len(raw) == 0 {
		return raw
	}
	var parsed interface{}
	if err := json.Unmarshal(raw, &parsed); err != nil {
		return raw
	}
	sorted := SortMapKeysRecursively(parsed)
	data, err := json.Marshal(sorted)
	if err != nil {
		return raw
	}
	return json.RawMessage(data)
}

// SortStringMapKeys sorts a map[string]string into an OrderedMap for numeric ascending JSON output
func SortStringMapKeys(m map[string]string) interface{} {
	if m == nil {
		return nil
	}
	type keyVal struct {
		key string
		num int
		val string
	}
	var kvs []keyVal
	for k, v := range m {
		n, err := strconv.Atoi(k)
		if err != nil {
			n = 999999
		}
		kvs = append(kvs, keyVal{key: k, num: n, val: v})
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
}

// SortInterfaceMapKeys sorts a map[string]interface{} into an OrderedMap for numeric ascending JSON output
func SortInterfaceMapKeys(m map[string]interface{}) interface{} {
	if m == nil {
		return nil
	}
	type keyVal struct {
		key string
		num int
		val interface{}
	}
	var kvs []keyVal
	for k, v := range m {
		n, err := strconv.Atoi(k)
		if err != nil {
			n = 999999
		}
		kvs = append(kvs, keyVal{key: k, num: n, val: SortMapKeysRecursively(v)})
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

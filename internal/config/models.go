package config

import (
	json "github.com/goccy/go-json"
	"fmt"
	"math/rand"
	"time"
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

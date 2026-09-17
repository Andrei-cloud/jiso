package config

import (
	"fmt"
	"os"
	"path/filepath"
	"sort"

	json "github.com/goccy/go-json"
)

// SaveItems merges newItems into filename with duplicate-name filtering
// and deterministic ordering, then writes the merged set atomically per
// the original analyze persistence contract: this is the ONE generated-
// items writer shared by the interactive wizard, the headless analyze
// (SaveConfigItems delegates here), and the §J TUI wizard write leg
// Moved verbatim from internal/command so internal/app can
// write generated items without importing the survey-bound package.
func SaveItems(filename string, newItems []Item) error {
	// Create directory if needed
	dir := filepath.Dir(filename)
	if dir != "" && dir != "." {
		if err := os.MkdirAll(dir, 0o755); err != nil {
			return err
		}
	}

	var existingItems []Item
	if data, err := os.ReadFile(filename); err == nil && len(data) > 0 {
		_ = json.Unmarshal(data, &existingItems)
	}

	// Filter out duplicate names
	itemMap := make(map[string]Item)
	for _, item := range existingItems {
		itemMap[item.Name] = item
	}
	for _, item := range newItems {
		itemMap[item.Name] = item
	}

	mergedItems := make([]serializableConfigItem, 0, len(itemMap))
	for _, item := range itemMap {
		mergedItems = append(mergedItems, toSerializableItem(item))
	}

	// Sort items by Name for deterministic order
	sort.Slice(mergedItems, func(i, j int) bool {
		return mergedItems[i].Name < mergedItems[j].Name
	})

	outputBytes, err := json.MarshalIndent(mergedItems, "", "  ")
	if err != nil {
		return fmt.Errorf("failed to marshal merged config items: %w", err)
	}

	return os.WriteFile(filename, outputBytes, 0o644)
}

// serializableConfigItem is a custom serializable struct used to preserve sorted
// map order during JSON marshaling.
type serializableConfigItem struct {
	Type           Discriminator   `json:"type,omitempty"`
	Name           string          `json:"name"`
	Description    string          `json:"description,omitempty"`
	Spec           string          `json:"spec,omitempty"`
	SpecFile       string          `json:"spec_file,omitempty"`
	Fields         any             `json:"fields,omitempty"`
	Dataset        any             `json:"dataset,omitempty"`
	Data           any             `json:"data,omitempty"`
	DatasetName    string          `json:"dataset_name,omitempty"`
	Steps          json.RawMessage `json:"steps,omitempty"`
	MatchFields    any             `json:"match_fields,omitempty"`
	RequiredFields []string        `json:"required_fields,omitempty"`
	EchoFields     []int           `json:"echo_fields,omitempty"`
	ResponseMTI    string          `json:"response_mti,omitempty"`
	ResponseFields any             `json:"response_fields,omitempty"`
	DelayMs        int             `json:"delay_ms,omitempty"`
	LatencyMs      int             `json:"latency_ms,omitempty"`
	JitterMs       int             `json:"jitter_ms,omitempty"`
	DropConnection bool            `json:"drop_connection,omitempty"`
}

// toSerializableItem converts an Item into its sorted serializable form.
func toSerializableItem(item Item) serializableConfigItem {
	sItem := serializableConfigItem{
		Type:           item.Type,
		Name:           item.Name,
		Description:    item.Description,
		Spec:           item.Spec,
		SpecFile:       item.SpecFile,
		DatasetName:    item.DatasetName,
		Steps:          item.Steps,
		RequiredFields: item.RequiredFields,
		EchoFields:     item.EchoFields,
		ResponseMTI:    item.ResponseMTI,
		DelayMs:        item.DelayMs,
		LatencyMs:      item.LatencyMs,
		JitterMs:       item.JitterMs,
		DropConnection: item.DropConnection,
	}

	if len(item.Fields) > 0 {
		sItem.Fields = sortedFields(item.Fields)
	}
	if item.Dataset != nil {
		sItem.Dataset = item.Dataset
	}
	if item.Data != nil {
		sItem.Data = item.Data
	}
	if item.MatchFields != nil {
		sItem.MatchFields = sortedMap(item.MatchFields, SortInterfaceMapKeys)
	}
	if item.ResponseFields != nil {
		sItem.ResponseFields = sortedMap(item.ResponseFields, SortStringMapKeys)
	}

	return sItem
}

// MarshalItemPreview renders one config item in exactly the form SaveItems
// writes to disk: ISO8583 field keys — and nested composite subfields — are
// recursively sorted in numeric ascending order (0,2,3,4,7,11,…), not Go's
// default map key order (0,11,14,18,19,2,…). The §J generated-item picker
// previews this so the operator sees precisely what `w` will persist; the
// preview and the file can no longer disagree on field order.
func MarshalItemPreview(item Item, prefix, indent string) ([]byte, error) {
	return json.MarshalIndent(toSerializableItem(item), prefix, indent)
}

// sortedFields re-marshals a raw fields blob with recursively sorted keys,
// falling back to the original bytes when it cannot be parsed or re-marshalled.
func sortedFields(raw json.RawMessage) any {
	var parsed any
	if err := json.Unmarshal(raw, &parsed); err != nil {
		return raw
	}

	sorted := SortMapKeysRecursively(parsed)
	if sortedBytes, err := json.Marshal(sorted); err == nil {
		return json.RawMessage(sortedBytes)
	}

	return raw
}

// sortedMap re-marshals a map with keys sorted by sortFn, falling back to the
// original value on marshal failure.
func sortedMap(m map[string]any, sortFn func(map[string]any) any) any {
	sorted := sortFn(m)
	if sortedBytes, err := json.Marshal(sorted); err == nil {
		return json.RawMessage(sortedBytes)
	}

	return m
}

package transactions

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"math/rand"
	"os"
	"path/filepath"
	"strings"

	"jiso/internal/utils"

	"github.com/moov-io/iso8583"
)

type Transaction struct {
	Name        string           `json:"name"`
	Description string           `json:"description"`
	Fields      json.RawMessage  `json:"fields"`
	Dataset     []map[int]string `json:"dataset"`
}

type TransactionCollection struct {
	spec         *iso8583.MessageSpec
	transactions []Transaction
}

func NewTransactionCollection(
	filename string,
	specs *iso8583.MessageSpec,
) (*TransactionCollection, error) {
	if isInvalidFilename(filename) {
		return nil, errors.New("invalid filename")
	}

	data, err := os.ReadFile(filename)
	if err != nil {
		return nil, fmt.Errorf("failed to read file: %w", err)
	}

	var transactions []Transaction
	if err := json.Unmarshal(data, &transactions); err != nil {
		return nil, fmt.Errorf("failed to unmarshal data: %w", err)
	}

	if len(transactions) == 0 {
		return nil, errors.New("no transactions found in the file")
	}

	fmt.Printf("Transactions loaded successfully. Count: %d\n", len(transactions))
	return &TransactionCollection{transactions: transactions, spec: specs}, nil
}

func isInvalidFilename(filename string) bool {
	return strings.Contains(filepath.Clean(filename), "..")
}

func (tc *TransactionCollection) ListNames() []string {
	names := make([]string, len(tc.transactions))
	for i, t := range tc.transactions {
		names[i] = t.Name
	}
	return names
}

func (tc *TransactionCollection) Info(name string) (string, string, string, error) {
	t, err := tc.findTransaction(name)
	if err != nil {
		return "", "", "", err
	}

	fieldsJSON, err := json.MarshalIndent(t.Fields, "", "  ")
	if err != nil {
		return "", "", "", err
	}
	return t.Name, t.Description, string(fieldsJSON), nil
}

func (tc *TransactionCollection) Compose(name string) (*iso8583.Message, error) {
	t, err := tc.findTransaction(name)
	if err != nil {
		return nil, err
	}

	msg := iso8583.NewMessage(tc.spec)
	err = tc.populateFields(msg, t)
	if err != nil {
		return nil, err
	}

	return msg, nil
}

func (tc *TransactionCollection) findTransaction(name string) (*Transaction, error) {
	for _, t := range tc.transactions {
		if t.Name == name {
			return &t, nil
		}
	}
	return nil, fmt.Errorf("transaction not found: %s", name)
}

func (tc *TransactionCollection) populateFields(msg *iso8583.Message, t *Transaction) error {
	fieldMap := make(map[int]interface{})
	if err := json.Unmarshal(t.Fields, &fieldMap); err != nil {
		return fmt.Errorf("json unmarshal error: %w", err)
	}

	dummyMsg := iso8583.NewMessage(tc.spec)
	if err := json.Unmarshal(t.Fields, &dummyMsg); err != nil {
		return fmt.Errorf("json unmarshal error: %w", err)
	}

	tc.setAutoFields(msg, fieldMap, t)
	tc.setStaticFields(msg, dummyMsg)
	tc.setRandomFields(msg, t.Dataset)

	return nil
}

func (tc *TransactionCollection) setAutoFields(
	msg *iso8583.Message,
	fieldMap map[int]interface{},
	t *Transaction,
) {
	for i, v := range fieldMap {
		if i < 2 {
			continue
		}

		switch v := v.(type) {
		case string:
			switch v {
			case "auto":
				tc.handleAutoFields(i, msg)
			case "random":
				tc.handleRandomFields(msg, t)
			}
		}
	}
}

func (tc *TransactionCollection) setStaticFields(msg *iso8583.Message, dummyMsg *iso8583.Message) {
	for i, f := range dummyMsg.GetFields() {
		if v, err := f.Bytes(); err == nil {
			if !bytes.Equal(v, []byte("random")) {
				msg.BinaryField(i, v)
			}
		}
	}
}

func (tc *TransactionCollection) handleAutoFields(i int, msg *iso8583.Message) {
	switch i {
	case 7:
		msg.Field(i, utils.GetTrxnDateTime())
	case 11:
		msg.Field(i, utils.GetCounter().GetStan())
	case 37:
		msg.Field(i, utils.GetRRNInstance().GetRRN())
	}
}

func (tc *TransactionCollection) setRandomFields(msg *iso8583.Message, dataSet []map[int]string) {
	if len(dataSet) > 0 {
		// Pick a random entry from the dataset
		randIndex := rand.Intn(len(dataSet))
		randomValues := dataSet[randIndex]

		for i, v := range randomValues {
			if v != "" {
				msg.Field(i, v)
			}
		}
	}
}

func (tc *TransactionCollection) handleRandomFields(msg *iso8583.Message, t *Transaction) {
	if len(t.Dataset) > 0 {
		// Pick a random value from the preloaded dataset
		randIndex := rand.Intn(len(t.Dataset))
		randomValues := t.Dataset[randIndex]

		for i, v := range randomValues {
			if v != "" {
				msg.Field(i, v)
			}
		}
	}
}

func (tc *TransactionCollection) ListFormatted() []string {
	maxNameLen := 0
	for _, t := range tc.transactions {
		if len(t.Name) > maxNameLen {
			maxNameLen = len(t.Name)
		}
	}

	formatted := make([]string, len(tc.transactions))
	for i, t := range tc.transactions {
		formatted[i] = fmt.Sprintf("%-*s - %s", maxNameLen, t.Name, t.Description)
	}
	return formatted
}

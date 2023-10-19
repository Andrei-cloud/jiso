package transactions

import (
	"bytes"
	"encoding/json"
	"fmt"
	"jiso/internal/utils"
	"math/rand"
	"os"

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
	data, err := os.ReadFile(filename)
	if err != nil {
		return nil, err
	}

	var transactions []Transaction
	err = json.Unmarshal(data, &transactions)
	if err != nil {
		return nil, err
	}

	fmt.Printf("Transactions loaded successfully. Count: %d\n", len(transactions))

	return &TransactionCollection{transactions: transactions, spec: specs}, nil
}

func (tc *TransactionCollection) ListNames() []string {
	var names []string
	for _, t := range tc.transactions {
		names = append(names, t.Name)
	}
	return names
}

func (tc *TransactionCollection) Info(name string) (string, string, string, error) {
	for _, t := range tc.transactions {
		if t.Name == name {
			fieldsJSON, err := json.MarshalIndent(t.Fields, "", "  ")
			if err != nil {
				return "", "", "", err
			}
			return t.Name, t.Description, string(fieldsJSON), nil
		}
	}
	return "", "", "", fmt.Errorf("transaction not found: %s", name)
}

func (tc *TransactionCollection) Compose(name string) (*iso8583.Message, error) {
	for _, t := range tc.transactions {
		if t.Name == name {
			// Create new ISO8583 message
			dummymsg := iso8583.NewMessage(tc.spec)

			// Parse JSON
			err := json.Unmarshal(t.Fields, &dummymsg)
			if err != nil {
				fmt.Println("JSON unmarshal error", err)
				return nil, err
			}

			fieldMap := make(map[int]interface{})
			err = json.Unmarshal(t.Fields, &fieldMap)
			if err != nil {
				fmt.Println("JSON unmarshal error", err)
				return nil, err
			}

			msg := iso8583.NewMessage(tc.spec)

			for i, f := range dummymsg.GetFields() {
				if v, err := f.Bytes(); err == nil {
					if !bytes.Equal(v, []byte("random")) {
						msg.BinaryField(i, v)
					}
				}
			}

			for i, v := range fieldMap {
				if i < 2 {
					continue
				}

				switch v := v.(type) {
				case string:
					if v == "auto" {
						switch i {
						case 7:
							msg.Field(i, utils.GetTrxnDateTime())
						case 11:
							msg.Field(i, utils.GetCounter().GetStan())
						case 37:
							msg.Field(i, utils.GetRRNInstance().GetRRN())
						}
					} else if v == "random" {
						if len(t.Dataset) > 0 {
							// Pick a random value from the preloaded dataset
							randIndex := rand.Intn(len(t.Dataset))
							randomValues := t.Dataset[randIndex]

							for i, v := range randomValues {
								if len(v) == 0 {
									continue
								}
								msg.Field(i, v)
							}
						}
					}
				}
			}

			return msg, nil
		}
	}
	return nil, fmt.Errorf("transaction not found: %s", name)
}

func (tc *TransactionCollection) ListFormatted() []string {
	var formatted []string
	maxNameLen := 0
	for _, t := range tc.transactions {
		if len(t.Name) > maxNameLen {
			maxNameLen = len(t.Name)
		}
	}
	for _, t := range tc.transactions {
		formatted = append(formatted, fmt.Sprintf("%-*s - %s", maxNameLen, t.Name, t.Description))
	}
	return formatted
}

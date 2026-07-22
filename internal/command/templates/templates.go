package templates

import (
	_ "embed"
)

// DefaultSpecJSON embeds the default ISO8583 specification JSON
//
//go:embed default_spec.json
var DefaultSpecJSON []byte

// DefaultTransactionJSON embeds the default sample transaction JSON configuration
//
//go:embed default_transactions.json
var DefaultTransactionJSON []byte

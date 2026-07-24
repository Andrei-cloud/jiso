package core

import (
	"strings"

	shellquote "github.com/kballard/go-shellquote"
)

// Lexer tokenizes command lines handling quotes and escaped characters
type Lexer struct{}

// NewLexer creates a new Lexer instance
func NewLexer() *Lexer {
	return &Lexer{}
}

// Tokenize splits a command input string into arguments respecting quotes
func (l *Lexer) Tokenize(input string) ([]string, error) {
	trimmed := strings.TrimSpace(input)
	if trimmed == "" {
		return []string{}, nil
	}
	return shellquote.Split(trimmed)
}

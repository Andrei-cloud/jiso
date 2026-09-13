package lexer

import (
	"reflect"
	"testing"
)

func TestTokenize(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name  string
		input string
		want  []string
	}{
		{"simple", "send 0100", []string{"send", "0100"}},
		{"quoted", `send "0100 value"`, []string{"send", "0100 value"}},
		{"empty", "   ", []string{}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := NewLexer().Tokenize(tt.input)
			if err != nil {
				t.Fatalf("Tokenize(%q) unexpected error: %v", tt.input, err)
			}
			if !reflect.DeepEqual(got, tt.want) {
				t.Fatalf("Tokenize(%q) = %v, want %v", tt.input, got, tt.want)
			}
		})
	}
}

func TestTokenizeUnbalancedQuote(t *testing.T) {
	t.Parallel()

	if _, err := NewLexer().Tokenize(`send "unterminated`); err == nil {
		t.Fatal("expected error for unbalanced quote, got nil")
	}
}

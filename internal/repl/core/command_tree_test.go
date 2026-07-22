package core

import (
	"context"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestCommandTreeAndLexer(t *testing.T) {
	registry := NewCommandRegistry()

	targetExec := false
	var passedArgs []string

	targetNode := NewCommandNode("target", "Set target endpoint")
	targetNode.Handler = func(ctx context.Context, args []string) error {
		targetExec = true
		passedArgs = args
		return nil
	}

	registry.Register(targetNode)

	err := registry.ExecuteLine(context.Background(), `target "127.0.0.1:8080" --flag value`)
	require.NoError(t, err)
	assert.True(t, targetExec)
	assert.Equal(t, []string{"127.0.0.1:8080", "--flag", "value"}, passedArgs)
}

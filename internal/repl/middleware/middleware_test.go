package middleware

import (
	"context"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestWithRecovery(t *testing.T) {
	panicHandler := func(ctx context.Context, args []string) error {
		panic("something went wrong")
	}

	wrapped := WithRecovery()(panicHandler)
	err := wrapped(context.Background(), nil)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "recovered from panic")
}

func TestWithConnectionGuard(t *testing.T) {
	dummyHandler := func(ctx context.Context, args []string) error {
		return nil
	}

	offlineCheck := func() bool { return false }
	onlineCheck := func() bool { return true }

	guardOffline := WithConnectionGuard(offlineCheck)(dummyHandler)
	errOffline := guardOffline(context.Background(), nil)
	require.Error(t, errOffline)
	assert.Equal(t, "connection is offline. Use 'connect' or 'target <addr>' to establish a connection", errOffline.Error())

	guardOnline := WithConnectionGuard(onlineCheck)(dummyHandler)
	errOnline := guardOnline(context.Background(), nil)
	require.NoError(t, errOnline)
}

func TestWithMetrics(t *testing.T) {
	logged := false
	logFn := func(d time.Duration) {
		logged = true
	}

	slowHandler := func(ctx context.Context, args []string) error {
		time.Sleep(10 * time.Millisecond)
		return nil
	}

	wrapped := WithMetrics(5*time.Millisecond, logFn)(slowHandler)
	err := wrapped(context.Background(), nil)
	require.NoError(t, err)
	assert.True(t, logged)
}

package middleware

import (
	"context"
	"errors"
	"fmt"
	"time"

	"jiso/internal/repl/core"
)

// WithRecovery returns a middleware that catches panics during command execution
func WithRecovery() core.Middleware {
	return func(next core.CommandHandler) core.CommandHandler {
		return func(ctx context.Context, args []string) (err error) {
			defer func() {
				if r := recover(); r != nil {
					err = fmt.Errorf("command recovered from panic: %v", r)
				}
			}()
			return next(ctx, args)
		}
	}
}

// WithMetrics returns a middleware that logs execution time if it exceeds a threshold
func WithMetrics(threshold time.Duration, logFn func(duration time.Duration)) core.Middleware {
	return func(next core.CommandHandler) core.CommandHandler {
		return func(ctx context.Context, args []string) error {
			start := time.Now()
			err := next(ctx, args)
			duration := time.Since(start)
			if duration >= threshold && logFn != nil {
				logFn(duration)
			}
			return err
		}
	}
}

// WithConnectionGuard returns a middleware that verifies TCP connection status before command execution
func WithConnectionGuard(checkConnected func() bool) core.Middleware {
	return func(next core.CommandHandler) core.CommandHandler {
		return func(ctx context.Context, args []string) error {
			if checkConnected != nil && !checkConnected() {
				return errors.New("connection is offline. Use 'connect' or 'target <addr>' to establish a connection")
			}
			return next(ctx, args)
		}
	}
}

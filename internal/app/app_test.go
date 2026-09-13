package app

import (
	"encoding/json"
	"errors"
	"net"
	"os"
	"strconv"
	"strings"
	"testing"
	"time"

	"jiso/internal/config"
)

// testConfig resets the shared config singleton for one test and restores
// it afterwards. Tests in this file must not run in parallel because they
// mutate that singleton.
func testConfig(t *testing.T) *config.Config {
	t.Helper()

	cfg := config.GetConfig()
	cfg.Reset()
	t.Cleanup(cfg.Reset)

	return cfg
}

func TestNewAndClose(t *testing.T) {
	cfg := testConfig(t)
	cfg.SetHost("127.0.0.1")
	cfg.SetPort("65535")

	a, err := New(cfg)
	if err != nil {
		t.Fatalf("New failed: %v", err)
	}

	if a.Service() == nil {
		t.Error("Service returned nil")
	}
	if a.Transactions() == nil {
		t.Error("Transactions returned nil")
	}
	if a.NetworkingStats() == nil {
		t.Error("NetworkingStats returned nil")
	}
	if a.Config() != cfg {
		t.Error("Config accessor returned a different instance")
	}
	if a.IsConnected() {
		t.Error("fresh App must not report connected")
	}

	if err := a.Close(); err != nil {
		t.Fatalf("Close failed: %v", err)
	}
	if err := a.Close(); err != nil {
		t.Fatalf("second Close must be idempotent, got: %v", err)
	}

	if err := a.Connect(); !errors.Is(err, ErrClosed) {
		t.Errorf("Connect after Close = %v, want ErrClosed", err)
	}
	if _, err := a.Send("anything"); !errors.Is(err, ErrClosed) {
		t.Errorf("Send after Close = %v, want ErrClosed", err)
	}
}

func TestNewRejectsInvalidConfig(t *testing.T) {
	cfg := testConfig(t)
	cfg.SetReconnectAttempts(-1)

	a, err := New(cfg)
	if err == nil {
		t.Fatal("New with invalid config returned no error")
	}
	if a != nil {
		t.Error("New with invalid config returned a non-nil App")
	}

	var cfgErr *ConfigError
	if !errors.As(err, &cfgErr) {
		t.Fatalf("error type = %T (%v), want *ConfigError", err, err)
	}
	if !strings.Contains(err.Error(), "configuration validation failed") {
		t.Errorf("error text = %q, want legacy validation prefix", err.Error())
	}
}

func TestNewRejectsTxFileWithoutSpec(t *testing.T) {
	cfg := testConfig(t)

	txFile := t.TempDir() + "/transactions.json"
	if err := os.WriteFile(txFile, []byte("[]"), 0o600); err != nil {
		t.Fatalf("failed to write temp tx file: %v", err)
	}
	cfg.SetFile(txFile)

	_, err := New(cfg)
	if err == nil {
		t.Fatal("New with tx file but no spec returned no error")
	}
	if !strings.Contains(err.Error(), "specification must be defined before loading transaction file") {
		t.Errorf("error text = %q, want spec-before-tx-file message", err.Error())
	}
}

func TestConnectFailureAgainstClosedPort(t *testing.T) {
	cfg := testConfig(t)

	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("failed to open probe listener: %v", err)
	}
	addr, ok := listener.Addr().(*net.TCPAddr)
	if !ok {
		t.Fatalf("listener.Addr() = %T, want *net.TCPAddr", listener.Addr())
	}
	port := strconv.Itoa(addr.Port)
	if err := listener.Close(); err != nil {
		t.Fatalf("failed to close probe listener: %v", err)
	}

	cfg.SetHost("127.0.0.1")
	cfg.SetPort(port)
	cfg.SetReconnectAttempts(0)
	cfg.SetConnectTimeout(time.Second)
	cfg.SetTotalConnectTimeout(2 * time.Second)

	a, err := New(cfg)
	if err != nil {
		t.Fatalf("New failed: %v", err)
	}
	defer func() {
		_ = a.Close()
	}()

	start := time.Now()
	err = a.Connect()
	elapsed := time.Since(start)

	if err == nil {
		t.Fatal("Connect to closed port returned no error")
	}
	if elapsed > 5*time.Second {
		t.Errorf("Connect took %v, want fast failure with 0 reconnect attempts", elapsed)
	}
	if a.IsConnected() {
		t.Error("App reports connected after failed Connect")
	}
}

func TestSendWithoutConnect(t *testing.T) {
	cfg := testConfig(t)
	cfg.SetHost("127.0.0.1")
	cfg.SetPort("65535")

	a, err := New(cfg)
	if err != nil {
		t.Fatalf("New failed: %v", err)
	}
	defer func() {
		_ = a.Close()
	}()

	result, err := a.Send("nonexistent")
	if err == nil {
		t.Fatal("Send without Connect returned no error")
	}
	if result != nil {
		t.Errorf("Send without Connect returned result %+v, want nil", result)
	}

	var notConn *NotConnectedError
	if !errors.As(err, &notConn) {
		t.Fatalf("error type = %T (%v), want *NotConnectedError", err, err)
	}
	if !strings.Contains(err.Error(), "not connected to target host 127.0.0.1:65535") {
		t.Errorf("error text = %q, want clear not-connected message", err.Error())
	}
}

func TestSendWithoutTarget(t *testing.T) {
	cfg := testConfig(t)

	a, err := New(cfg)
	if err != nil {
		t.Fatalf("New failed: %v", err)
	}
	defer func() {
		_ = a.Close()
	}()

	_, err = a.Send("nonexistent")
	if err == nil {
		t.Fatal("Send without target returned no error")
	}
	if !strings.Contains(err.Error(), "target host and port are not configured") {
		t.Errorf("error text = %q, want target-not-configured message", err.Error())
	}
}

func TestSendResultJSONSerializable(t *testing.T) {
	original := SendResult{
		Description: "--- REQUEST ---\n",
		Hex:         "00000000  30 39 30 30 |\n",
		Fields:      []FieldView{{ID: "11", Name: "STAN", Value: "000001"}},
		Elapsed:     1500 * time.Millisecond,
		Error:       "boom",
		Warnings:    []string{"careful"},
		ResponseHex: "00000000  30 39 31 30 |\n",
	}

	data, err := json.Marshal(original)
	if err != nil {
		t.Fatalf("Marshal failed: %v", err)
	}

	var decoded SendResult
	if err := json.Unmarshal(data, &decoded); err != nil {
		t.Fatalf("Unmarshal failed: %v", err)
	}

	if decoded.Description != original.Description ||
		decoded.Hex != original.Hex ||
		decoded.Elapsed != original.Elapsed ||
		decoded.Error != original.Error ||
		decoded.ResponseHex != original.ResponseHex {
		t.Errorf("round-trip mismatch: %+v != %+v", decoded, original)
	}
	if len(decoded.Fields) != 1 || decoded.Fields[0] != original.Fields[0] {
		t.Errorf("fields round-trip mismatch: %+v", decoded.Fields)
	}
	if len(decoded.Warnings) != 1 || decoded.Warnings[0] != "careful" {
		t.Errorf("warnings round-trip mismatch: %+v", decoded.Warnings)
	}
}

package config

import (
	"testing"
	"time"
)

func TestGetConfig(t *testing.T) {
	config1 := GetConfig()
	if config1 == nil {
		t.Fatal("GetConfig returned nil")
	}

	config2 := GetConfig()
	if config1 != config2 {
		t.Error("GetConfig is not returning singleton instance")
	}
}

func TestConfigSettersAndGetters(t *testing.T) {
	config := GetConfig()

	// Test SetHost and GetHost
	config.SetHost("testhost")
	if config.GetHost() != "testhost" {
		t.Errorf("Expected host 'testhost', got '%s'", config.GetHost())
	}

	// Test SetPort and GetPort
	config.SetPort("9999")
	if config.GetPort() != "9999" {
		t.Errorf("Expected port '9999', got '%s'", config.GetPort())
	}

	// Test SetSpec and GetSpec
	config.SetSpec("testspec.json")
	if config.GetSpec() != "testspec.json" {
		t.Errorf("Expected spec 'testspec.json', got '%s'", config.GetSpec())
	}

	// Test SetFile and GetFile
	config.SetFile("testfile.json")
	if config.GetFile() != "testfile.json" {
		t.Errorf("Expected file 'testfile.json', got '%s'", config.GetFile())
	}

	// Test SetReconnectAttempts and GetReconnectAttempts
	config.SetReconnectAttempts(5)
	if config.GetReconnectAttempts() != 5 {
		t.Errorf("Expected reconnect attempts 5, got %d", config.GetReconnectAttempts())
	}

	// Test SetConnectTimeout and GetConnectTimeout
	timeout := 10 * time.Second
	config.SetConnectTimeout(timeout)
	if config.GetConnectTimeout() != timeout {
		t.Errorf("Expected connect timeout %v, got %v", timeout, config.GetConnectTimeout())
	}

	// Test SetTotalConnectTimeout and GetTotalConnectTimeout
	totalTimeout := 20 * time.Second
	config.SetTotalConnectTimeout(totalTimeout)
	if config.GetTotalConnectTimeout() != totalTimeout {
		t.Errorf(
			"Expected total connect timeout %v, got %v",
			totalTimeout,
			config.GetTotalConnectTimeout(),
		)
	}
}

func TestConfigSetHostEmpty(t *testing.T) {
	config := GetConfig()
	config.SetHost("initial")
	config.SetHost("") // Should not change
	if config.GetHost() != "initial" {
		t.Errorf("Expected host 'initial', got '%s'", config.GetHost())
	}
}

func TestConfigSetPortEmpty(t *testing.T) {
	config := GetConfig()
	config.SetPort("initial")
	config.SetPort("") // Should not change
	if config.GetPort() != "initial" {
		t.Errorf("Expected port 'initial', got '%s'", config.GetPort())
	}
}

func TestConfigSetSpecEmpty(t *testing.T) {
	config := GetConfig()
	config.SetSpec("initial")
	config.SetSpec("") // Should not change
	if config.GetSpec() != "initial" {
		t.Errorf("Expected spec 'initial', got '%s'", config.GetSpec())
	}
}

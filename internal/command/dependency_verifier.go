package command

import (
	"fmt"
	"strings"

	cfg "jiso/internal/config"
	"jiso/internal/service"
	"jiso/internal/transactions"
)

// VerifyTarget checks if target endpoint (host and port) is configured
func VerifyTarget() error {
	host := cfg.GetConfig().GetHost()
	port := cfg.GetConfig().GetPort()
	if strings.TrimSpace(host) == "" || strings.TrimSpace(port) == "" {
		return fmt.Errorf("target host and port are not configured. Please set target using 'target <host:port>' first")
	}
	return nil
}

// VerifySpec checks if a specification is loaded
func VerifySpec(svc *service.Service) error {
	if svc != nil && svc.GetSpec() != nil {
		return nil
	}
	specFile := cfg.GetConfig().GetSpec()
	if strings.TrimSpace(specFile) == "" {
		return fmt.Errorf("specification is not selected. Please select a specification using 'spec <path>' first")
	}
	return nil
}

// VerifyTx checks if transaction repository is loaded and non-empty
func VerifyTx(tx transactions.Repository) error {
	if tx != nil && len(tx.ListNames()) > 0 {
		return nil
	}
	txFile := cfg.GetConfig().GetFile()
	if strings.TrimSpace(txFile) == "" {
		return fmt.Errorf("transaction file is not selected. Please select a transaction file using 'tx <path>' first")
	}
	if tx != nil && len(tx.ListNames()) == 0 {
		return fmt.Errorf("transaction file '%s' contains no transactions. Please select a valid transaction file using 'tx <path>'", txFile)
	}
	return nil
}

// VerifyConnection checks if service is connected to target host
func VerifyConnection(svc *service.Service) error {
	if err := VerifyTarget(); err != nil {
		return err
	}
	if svc == nil || !svc.IsConnected() {
		host, port := cfg.GetConfig().GetHost(), cfg.GetConfig().GetPort()
		return fmt.Errorf("not connected to target host %s:%s. Please connect first using 'connect'", host, port)
	}
	return nil
}

package connection

import (
	"fmt"
	"sync"
	"time"

	"jiso/internal/utils"

	"github.com/moov-io/iso8583"
)

// SMCHeartbeatDaemon manages automated Visa 0800 echo keep-alive messages
type SMCHeartbeatDaemon struct {
	manager  *Manager
	interval time.Duration
	stopChan chan struct{}
	running  bool
	mu       sync.Mutex
}

// NewSMCHeartbeatDaemon creates a new heartbeat daemon for Visa SMC connections
func NewSMCHeartbeatDaemon(manager *Manager, interval time.Duration) *SMCHeartbeatDaemon {
	if interval <= 0 {
		interval = 30 * time.Second
	}
	return &SMCHeartbeatDaemon{
		manager:  manager,
		interval: interval,
	}
}

// Start begins periodic Visa 0800 echo tests if active header is *utils.VisaHeader
func (h *SMCHeartbeatDaemon) Start() {
	h.mu.Lock()
	defer h.mu.Unlock()

	if h.running {
		return
	}

	h.stopChan = make(chan struct{})
	h.running = true
	go h.loop()
}

// Stop terminates the heartbeat daemon
func (h *SMCHeartbeatDaemon) Stop() {
	h.mu.Lock()
	defer h.mu.Unlock()

	if !h.running {
		return
	}

	h.running = false
	close(h.stopChan)
}

// IsRunning returns whether the heartbeat daemon is active
func (h *SMCHeartbeatDaemon) IsRunning() bool {
	h.mu.Lock()
	defer h.mu.Unlock()
	return h.running
}

func (h *SMCHeartbeatDaemon) loop() {
	ticker := time.NewTicker(h.interval)
	defer ticker.Stop()

	for {
		select {
		case <-h.stopChan:
			return
		case <-ticker.C:
			if !h.manager.IsConnected() {
				continue
			}

			if err := h.sendVisaEcho(); err != nil {
				if h.manager.debugMode {
					fmt.Printf("[SMC-HEARTBEAT] ⚠️ Visa 0800 Echo test failed: %v\n", err)
				}
			}
		}
	}
}

func (h *SMCHeartbeatDaemon) sendVisaEcho() error {
	spec := h.manager.GetSpec()
	if spec == nil {
		return fmt.Errorf("message spec not available")
	}

	msg := iso8583.NewMessage(spec)
	msg.MTI("0800")

	// DE 70: Network Management Information Code (0301 = Visa Echo)
	msg.Field(70, "0301")

	// DE 7: Transmission Date & Time (MMDDhhmmss)
	now := time.Now().UTC()
	dtStr := now.Format("0102150405")
	msg.Field(7, dtStr)

	// DE 11: Systems Trace Audit Number (STAN)
	stanStr := fmt.Sprintf("%06d", now.UnixNano()%1000000)
	msg.Field(11, stanStr)

	// DE 63: Network Data
	msg.Field(63, "0002")

	// Set session control indicator on header if VisaHeader
	h.manager.statusMu.RLock()
	if vHdr, ok := h.manager.header.(*utils.VisaHeader); ok && vHdr != nil {
		vHdr.SetSessionControl(true)
		defer vHdr.SetSessionControl(false)
	}
	h.manager.statusMu.RUnlock()

	resp, err := h.manager.Send(msg)
	if err != nil {
		return err
	}

	if resp == nil {
		return fmt.Errorf("no response received for Visa 0800 echo")
	}

	mti, _ := resp.GetMTI()
	respCode := ""
	if f39 := resp.GetField(39); f39 != nil {
		respCode, _ = f39.String()
	}

	if h.manager.debugMode {
		fmt.Printf("[SMC-HEARTBEAT] 🟢 Visa 0800 Echo successful (Response MTI: %s, RC: %s)\n", mti, respCode)
	}

	if respCode != "00" && respCode != "" {
		return fmt.Errorf("Visa 0800 echo rejected with response code %s", respCode)
	}

	return nil
}

// IsVisaHeader returns true if the specified header is a *utils.VisaHeader
func IsVisaHeader(hdr interface{}) bool {
	if hdr == nil {
		return false
	}
	_, ok := hdr.(*utils.VisaHeader)
	return ok
}

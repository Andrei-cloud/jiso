package analyzer

import (
	"encoding/binary"
	"fmt"
	"io"
	"os"
	"sort"

	"github.com/moov-io/iso8583"
)

// Magic numbers for PCAP and PCAPNG
const (
	pcapMagicMicroSecLE = 0xd4c3b2a1
	pcapMagicMicroSecBE = 0xa1b2c3d4
	pcapMagicNanoSecLE  = 0x4d3c2b1a
	pcapMagicNanoSecBE  = 0xa1b23c4d
	pcapngSectionHeader = 0x0a0d0d0a
)

// TrafficDirection represents a directional port flow in a PCAP capture
type TrafficDirection struct {
	Label       string
	TargetPort  uint16
	Mode        string // "dst" (requests), "src" (responses), or "all"
	PacketCount int
	ByteCount   int
}

// IsPCAPFile checks if a byte slice starts with a PCAP or PCAPNG header magic number
func IsPCAPFile(header []byte) bool {
	if len(header) < 4 {
		return false
	}
	magic := binary.BigEndian.Uint32(header[0:4])
	return magic == pcapMagicMicroSecBE || magic == pcapMagicMicroSecLE ||
		magic == pcapMagicNanoSecBE || magic == pcapMagicNanoSecLE ||
		magic == pcapngSectionHeader
}

// ExtractMessagesFromFile automatically detects if the file is a PCAP/PCAPNG capture
// or a raw byte stream, and extracts all framed ISO8583 messages.
func (a *StreamAnalyzer) ExtractMessagesFromFile(filePath string, headerType string) ([]*iso8583.Message, error) {
	return a.ExtractMessagesFromFileWithDirection(filePath, headerType, TrafficDirection{Mode: "all"})
}

// ExtractMessagesFromFileWithDirection extracts messages filtered by traffic direction
func (a *StreamAnalyzer) ExtractMessagesFromFileWithDirection(filePath string, headerType string, dir TrafficDirection) ([]*iso8583.Message, error) {
	f, err := os.Open(filePath)
	if err != nil {
		return nil, fmt.Errorf("failed to open file '%s': %w", filePath, err)
	}
	defer f.Close()

	headerBuf := make([]byte, 24)
	n, _ := io.ReadFull(f, headerBuf)
	if _, err := f.Seek(0, io.SeekStart); err != nil {
		return nil, fmt.Errorf("seek error: %w", err)
	}

	if n >= 4 && IsPCAPFile(headerBuf[:n]) {
		magicBE := binary.BigEndian.Uint32(headerBuf[0:4])
		var tcpPayloads []byte
		var parseErr error

		if magicBE == pcapngSectionHeader {
			tcpPayloads, parseErr = ExtractTCPPayloadsFromPCAPNGFiltered(f, dir)
		} else {
			tcpPayloads, parseErr = ExtractTCPPayloadsFromPCAPFiltered(f, dir)
		}

		if parseErr != nil {
			return nil, fmt.Errorf("extracting TCP payloads from PCAP file: %w", parseErr)
		}

		if len(tcpPayloads) == 0 {
			return nil, fmt.Errorf("no matching TCP payload data found in PCAP capture file '%s' for direction '%s'", filePath, dir.Label)
		}

		return a.ExtractMessagesFromStream(tcpPayloads, headerType)
	}

	// Not a PCAP file, parse as raw framed byte stream
	return a.ExtractMessagesFromReader(f, headerType)
}

// InspectPCAPDirections scans a PCAP/PCAPNG capture file and reports all active port traffic directions
func InspectPCAPDirections(filePath string) ([]TrafficDirection, error) {
	f, err := os.Open(filePath)
	if err != nil {
		return nil, fmt.Errorf("failed to open file: %w", err)
	}
	defer f.Close()

	headerBuf := make([]byte, 24)
	n, _ := io.ReadFull(f, headerBuf)
	if _, err := f.Seek(0, io.SeekStart); err != nil {
		return nil, fmt.Errorf("seek error: %w", err)
	}

	if n < 4 || !IsPCAPFile(headerBuf[:n]) {
		return nil, nil // Not a PCAP file
	}

	type portStats struct {
		dstPackets int
		dstBytes   int
		srcPackets int
		srcBytes   int
	}
	stats := make(map[uint16]*portStats)

	collector := func(srcPort, dstPort uint16, payload []byte) {
		if len(payload) == 0 {
			return
		}
		if _, ok := stats[dstPort]; !ok {
			stats[dstPort] = &portStats{}
		}
		stats[dstPort].dstPackets++
		stats[dstPort].dstBytes += len(payload)

		if _, ok := stats[srcPort]; !ok {
			stats[srcPort] = &portStats{}
		}
		stats[srcPort].srcPackets++
		stats[srcPort].srcBytes += len(payload)
	}

	magicBE := binary.BigEndian.Uint32(headerBuf[0:4])
	if magicBE == pcapngSectionHeader {
		_ = parsePCAPNGPackets(f, collector)
	} else {
		_ = parsePCAPPackets(f, collector)
	}

	var ports []uint16
	for p := range stats {
		ports = append(ports, p)
	}
	sort.Slice(ports, func(i, j int) bool {
		return ports[i] < ports[j]
	})

	var directions []TrafficDirection
	totalPackets := 0
	totalBytes := 0

	for _, p := range ports {
		st := stats[p]
		if st.dstPackets > 0 {
			directions = append(directions, TrafficDirection{
				Label:       fmt.Sprintf("Incoming Requests -> Dst Port %d (%d pkts, %d bytes)", p, st.dstPackets, st.dstBytes),
				TargetPort:  p,
				Mode:        "dst",
				PacketCount: st.dstPackets,
				ByteCount:   st.dstBytes,
			})
			totalPackets += st.dstPackets
			totalBytes += st.dstBytes
		}
		if st.srcPackets > 0 {
			directions = append(directions, TrafficDirection{
				Label:       fmt.Sprintf("Outgoing Responses -> Src Port %d (%d pkts, %d bytes)", p, st.srcPackets, st.srcBytes),
				TargetPort:  p,
				Mode:        "src",
				PacketCount: st.srcPackets,
				ByteCount:   st.srcBytes,
			})
		}
	}

	if len(directions) > 0 {
		directions = append(directions, TrafficDirection{
			Label:       fmt.Sprintf("All Traffic (Both directions, %d bytes total)", totalBytes),
			TargetPort:  0,
			Mode:        "all",
			PacketCount: totalPackets,
			ByteCount:   totalBytes,
		})
	}

	return directions, nil
}

// ExtractTCPPayloadsFromPCAP reads a standard .pcap file and returns concatenated TCP payload bytes

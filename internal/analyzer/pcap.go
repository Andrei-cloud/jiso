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

	type pairKey struct {
		p1, p2 uint16
	}
	type pairStats struct {
		p1, p2       uint16
		p1DstPackets int
		p1DstBytes   int
		p2DstPackets int
		p2DstBytes   int
	}

	pairs := make(map[pairKey]*pairStats)
	totalPackets := 0
	totalBytes := 0

	collector := func(srcPort, dstPort uint16, payload []byte) {
		if len(payload) == 0 {
			return
		}
		totalPackets++
		totalBytes += len(payload)

		p1, p2 := srcPort, dstPort
		if p1 > p2 {
			p1, p2 = p2, p1
		}
		key := pairKey{p1: p1, p2: p2}
		ps, ok := pairs[key]
		if !ok {
			ps = &pairStats{p1: p1, p2: p2}
			pairs[key] = ps
		}

		if dstPort == p1 {
			ps.p1DstPackets++
			ps.p1DstBytes += len(payload)
		} else {
			ps.p2DstPackets++
			ps.p2DstBytes += len(payload)
		}
	}

	magicBE := binary.BigEndian.Uint32(headerBuf[0:4])
	if magicBE == pcapngSectionHeader {
		_ = parsePCAPNGPackets(f, collector)
	} else {
		_ = parsePCAPPackets(f, collector)
	}

	// Order pairs deterministically
	var sortedKeys []pairKey
	for k := range pairs {
		sortedKeys = append(sortedKeys, k)
	}
	sort.Slice(sortedKeys, func(i, j int) bool {
		if sortedKeys[i].p1 != sortedKeys[j].p1 {
			return sortedKeys[i].p1 < sortedKeys[j].p1
		}
		return sortedKeys[i].p2 < sortedKeys[j].p2
	})

	var directions []TrafficDirection
	for _, k := range sortedKeys {
		ps := pairs[k]
		// Identify which port is the server port.
		// Rule: Server port is the listener. If one port has lower port number (e.g. 4005 vs 47772),
		// or if traffic volume is unequal, pick the primary server port.
		serverPort := ps.p1
		clientPort := ps.p2
		serverDstPackets := ps.p1DstPackets
		serverDstBytes := ps.p1DstBytes
		serverSrcPackets := ps.p2DstPackets // Client destination is server source
		serverSrcBytes := ps.p2DstBytes

		// If p2 is a known well-known/lower port or has significantly higher incoming payload count, swap:
		if ps.p2 < ps.p1 {
			serverPort, clientPort = ps.p2, ps.p1
			serverDstPackets, serverDstBytes = ps.p2DstPackets, ps.p2DstBytes
			serverSrcPackets, serverSrcBytes = ps.p1DstPackets, ps.p1DstBytes
		}

		if serverDstPackets > 0 {
			directions = append(directions, TrafficDirection{
				Label:       fmt.Sprintf("-> Dst Port %d (%d pkts, %d bytes)", serverPort, serverDstPackets, serverDstBytes),
				TargetPort:  serverPort,
				Mode:        "dst",
				PacketCount: serverDstPackets,
				ByteCount:   serverDstBytes,
			})
		}
		if serverSrcPackets > 0 {
			directions = append(directions, TrafficDirection{
				Label:       fmt.Sprintf("<- Src Port %d (%d pkts, %d bytes)", serverPort, serverSrcPackets, serverSrcBytes),
				TargetPort:  serverPort,
				Mode:        "src",
				PacketCount: serverSrcPackets,
				ByteCount:   serverSrcBytes,
			})
		}
		_ = clientPort
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

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

// The direction modes a TrafficDirection can be set to. They are stored in the
// filter, compared in the parser, and reported in the analyze result, so all
// three places must spell them the same: a mode the parser does not recognise
// filters nothing, and the operator sees every packet in a capture they asked to
// narrow.
const (
	DirectionDst = "dst" // requests: the port we sent from
	DirectionSrc = "src" // responses: the port the reply came back to
	DirectionAll = "all" // both directions
)

// TrafficDirection represents a directional port flow in a PCAP capture
type TrafficDirection struct {
	Label       string
	TargetPort  uint16
	PeerPort    uint16 // the other end of the conversation (UAT round 7: origin clarity)
	Mode        string // DirectionDst, DirectionSrc or DirectionAll
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

// ExtractMessagesFromFileWithDirection extracts messages filtered by traffic direction
func (a *StreamAnalyzer) ExtractMessagesFromFileWithDirection(filePath, headerType string, dir TrafficDirection) ([]*iso8583.Message, error) {
	messages, _, err := a.ExtractMessagesFromFileCounted(filePath, headerType, dir)

	return messages, err
}

// ExtractMessagesFromFileCounted is the counted variant of
// ExtractMessagesFromFileWithDirection: the second result is the number
// of framed messages that failed to unpack (the §J flow-table's
// "N parsed, M unparsable" line, SCR-510). Framing and direction
// filtering are unchanged.
func (a *StreamAnalyzer) ExtractMessagesFromFileCounted(filePath, headerType string, dir TrafficDirection) ([]*iso8583.Message, int, error) {
	return a.ExtractMessagesFromFileSampled(filePath, headerType, dir, nil)
}

// ExtractMessagesFromFileSampled is the counted file extractor with an
// optional failure-sample collector (UAT round 6 §J reviewer). For a
// PCAP the sample offsets are positions in the concatenated TCP payload
// stream; for a raw capture they are file offsets.
func (a *StreamAnalyzer) ExtractMessagesFromFileSampled(filePath, headerType string, dir TrafficDirection, samples *UnparsableCollector) ([]*iso8583.Message, int, error) {
	f, err := os.Open(filePath)
	if err != nil {
		return nil, 0, fmt.Errorf("failed to open file '%s': %w", filePath, err)
	}
	defer func() { _ = f.Close() }() // read-only handle; nothing to report on close

	headerBuf := make([]byte, 24)
	n, _ := io.ReadFull(f, headerBuf)
	if _, err := f.Seek(0, io.SeekStart); err != nil {
		return nil, 0, fmt.Errorf("seek error: %w", err)
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
			return nil, 0, fmt.Errorf("extracting TCP payloads from PCAP file: %w", parseErr)
		}

		if len(tcpPayloads) == 0 {
			return nil, 0, fmt.Errorf("no matching TCP payload data found in PCAP capture file '%s' for direction '%s'", filePath, dir.Label)
		}

		return a.ExtractMessagesFromStreamSampled(tcpPayloads, samples, headerType)
	}

	// Not a PCAP file, parse as raw framed byte stream
	return a.ExtractMessagesFromReaderSampled(f, headerType, samples)
}

// InspectPCAPDirections scans a PCAP/PCAPNG capture file and reports all active port traffic directions
func InspectPCAPDirections(filePath string) ([]TrafficDirection, error) {
	f, err := os.Open(filePath)
	if err != nil {
		return nil, fmt.Errorf("failed to open file: %w", err)
	}
	defer func() { _ = f.Close() }() // read-only handle; nothing to report on close

	headerBuf := make([]byte, 24)
	n, _ := io.ReadFull(f, headerBuf)
	if _, err := f.Seek(0, io.SeekStart); err != nil {
		return nil, fmt.Errorf("seek error: %w", err)
	}

	if n < 4 || !IsPCAPFile(headerBuf[:n]) {
		return nil, nil // Not a PCAP file
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

	var directions []TrafficDirection
	for _, k := range sortedPairKeys(pairs) {
		directions = append(directions, directionsForPair(pairs[k])...)
	}

	if len(directions) > 0 {
		directions = append(directions, TrafficDirection{
			Label:       fmt.Sprintf("All Traffic (Both directions, %d bytes total)", totalBytes),
			TargetPort:  0,
			Mode:        DirectionAll,
			PacketCount: totalPackets,
			ByteCount:   totalBytes,
		})
	}

	return directions, nil
}

// pairKey is the unordered (low, high) port pair used to bucket traffic.
type pairKey struct {
	p1, p2 uint16
}

// pairStats accumulates per-direction packet/byte counts for one pairKey.
type pairStats struct {
	p1, p2       uint16
	p1DstPackets int
	p1DstBytes   int
	p2DstPackets int
	p2DstBytes   int
}

// sortedPairKeys returns the pair keys in ascending (p1, p2) order so the
// reported directions are deterministic.
func sortedPairKeys(pairs map[pairKey]*pairStats) []pairKey {
	keys := make([]pairKey, 0, len(pairs))
	for k := range pairs {
		keys = append(keys, k)
	}
	sort.Slice(keys, func(i, j int) bool {
		if keys[i].p1 != keys[j].p1 {
			return keys[i].p1 < keys[j].p1
		}

		return keys[i].p2 < keys[j].p2
	})

	return keys
}

// directionsForPair turns one port pair's stats into its dst/src directions,
// treating the lower-numbered port as the server.
func directionsForPair(ps *pairStats) []TrafficDirection {
	serverPort, clientPort := ps.p1, ps.p2
	serverDstPackets, serverDstBytes := ps.p1DstPackets, ps.p1DstBytes
	serverSrcPackets, serverSrcBytes := ps.p2DstPackets, ps.p2DstBytes

	if ps.p2 < ps.p1 {
		serverPort, clientPort = ps.p2, ps.p1
		serverDstPackets, serverDstBytes = ps.p2DstPackets, ps.p2DstBytes
		serverSrcPackets, serverSrcBytes = ps.p1DstPackets, ps.p1DstBytes
	}

	var out []TrafficDirection
	if serverDstPackets > 0 {
		out = append(out, TrafficDirection{
			Label:       fmt.Sprintf("-> Dst Port %d from %d (%d pkts, %d bytes)", serverPort, clientPort, serverDstPackets, serverDstBytes),
			TargetPort:  serverPort,
			PeerPort:    clientPort,
			Mode:        DirectionDst,
			PacketCount: serverDstPackets,
			ByteCount:   serverDstBytes,
		})
	}
	if serverSrcPackets > 0 {
		out = append(out, TrafficDirection{
			Label:       fmt.Sprintf("<- Src Port %d to %d (%d pkts, %d bytes)", serverPort, clientPort, serverSrcPackets, serverSrcBytes),
			TargetPort:  serverPort,
			PeerPort:    clientPort,
			Mode:        DirectionSrc,
			PacketCount: serverSrcPackets,
			ByteCount:   serverSrcBytes,
		})
	}

	return out
}

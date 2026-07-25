package analyzer

import (
	"bytes"
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
func ExtractTCPPayloadsFromPCAP(r io.Reader) ([]byte, error) {
	return ExtractTCPPayloadsFromPCAPFiltered(r, TrafficDirection{Mode: "all"})
}

// ExtractTCPPayloadsFromPCAPFiltered extracts TCP payloads matching the given directional filter
func ExtractTCPPayloadsFromPCAPFiltered(r io.Reader, dir TrafficDirection) ([]byte, error) {
	var payloadBuffer bytes.Buffer
	collector := func(srcPort, dstPort uint16, payload []byte) {
		if len(payload) == 0 {
			return
		}
		if dir.Mode == "dst" && dstPort != dir.TargetPort {
			return
		}
		if dir.Mode == "src" && srcPort != dir.TargetPort {
			return
		}
		payloadBuffer.Write(payload)
	}

	err := parsePCAPPackets(r, collector)
	if err != nil && err != io.EOF && err != io.ErrUnexpectedEOF {
		return nil, err
	}
	return payloadBuffer.Bytes(), nil
}

// ExtractTCPPayloadsFromPCAPNGFiltered extracts TCP payloads from PCAPNG with directional filtering
func ExtractTCPPayloadsFromPCAPNGFiltered(r io.Reader, dir TrafficDirection) ([]byte, error) {
	var payloadBuffer bytes.Buffer
	collector := func(srcPort, dstPort uint16, payload []byte) {
		if len(payload) == 0 {
			return
		}
		if dir.Mode == "dst" && dstPort != dir.TargetPort {
			return
		}
		if dir.Mode == "src" && srcPort != dir.TargetPort {
			return
		}
		payloadBuffer.Write(payload)
	}

	err := parsePCAPNGPackets(r, collector)
	if err != nil && err != io.EOF && err != io.ErrUnexpectedEOF {
		return nil, err
	}
	return payloadBuffer.Bytes(), nil
}

func parsePCAPPackets(r io.Reader, fn func(srcPort, dstPort uint16, payload []byte)) error {
	var globalHdr [24]byte
	if _, err := io.ReadFull(r, globalHdr[:]); err != nil {
		return err
	}

	magicBE := binary.BigEndian.Uint32(globalHdr[0:4])
	var byteOrder binary.ByteOrder
	switch magicBE {
	case pcapMagicMicroSecBE, pcapMagicNanoSecBE:
		byteOrder = binary.BigEndian
	case pcapMagicMicroSecLE, pcapMagicNanoSecLE:
		byteOrder = binary.LittleEndian
	default:
		magicLE := binary.LittleEndian.Uint32(globalHdr[0:4])
		if magicLE == pcapMagicMicroSecBE || magicLE == pcapMagicNanoSecBE {
			byteOrder = binary.LittleEndian
		} else {
			return fmt.Errorf("unsupported PCAP magic number: 0x%x", magicBE)
		}
	}

	linkType := byteOrder.Uint32(globalHdr[20:24])

	for {
		var recordHdr [16]byte
		if _, err := io.ReadFull(r, recordHdr[:]); err != nil {
			return err
		}

		inclLen := byteOrder.Uint32(recordHdr[8:12])
		if inclLen == 0 || inclLen > 65535 {
			break
		}

		packetBuf := make([]byte, inclLen)
		if _, err := io.ReadFull(r, packetBuf); err != nil {
			break
		}

		srcPort, dstPort, tcpPayload := extractTCPPayloadFromPacket(packetBuf, linkType)
		if len(tcpPayload) > 0 {
			fn(srcPort, dstPort, tcpPayload)
		}
	}
	return nil
}

func parsePCAPNGPackets(r io.Reader, fn func(srcPort, dstPort uint16, payload []byte)) error {
	for {
		var blockHdr [8]byte
		if _, err := io.ReadFull(r, blockHdr[:]); err != nil {
			return err
		}
		blockType := binary.LittleEndian.Uint32(blockHdr[0:4])
		blockLen := binary.LittleEndian.Uint32(blockHdr[4:8])
		if blockLen < 12 {
			break
		}

		dataLen := int(blockLen) - 12
		dataBuf := make([]byte, dataLen)
		if _, err := io.ReadFull(r, dataBuf); err != nil {
			break
		}

		pad := (4 - (dataLen % 4)) % 4
		if pad > 0 {
			var dummy [4]byte
			_, _ = io.ReadFull(r, dummy[:pad])
		}

		// EPB (Enhanced Packet Block) type = 0x00000006
		if blockType == 0x00000006 && len(dataBuf) >= 20 {
			capLen := binary.LittleEndian.Uint32(dataBuf[12:16])
			if int(capLen)+20 <= len(dataBuf) {
				packetData := dataBuf[20 : 20+capLen]
				srcPort, dstPort, tcpPayload := extractTCPPayloadFromPacket(packetData, 1)
				if len(tcpPayload) == 0 {
					srcPort, dstPort, tcpPayload = extractTCPPayloadFromPacket(packetData, 0)
				}
				if len(tcpPayload) > 0 {
					fn(srcPort, dstPort, tcpPayload)
				}
			}
		}
	}
	return nil
}

func extractTCPPayloadFromPacket(packet []byte, linkType uint32) (uint16, uint16, []byte) {
	var linkHeaderLen int
	var ethType uint16

	switch linkType {
	case 1: // LINKTYPE_ETHERNET
		if len(packet) < 14 {
			return 0, 0, nil
		}
		linkHeaderLen = 14
		ethType = binary.BigEndian.Uint16(packet[12:14])
		if ethType == 0x8100 { // 802.1Q VLAN Tag
			if len(packet) < 18 {
				return 0, 0, nil
			}
			linkHeaderLen = 18
			ethType = binary.BigEndian.Uint16(packet[16:18])
		}

	case 0, 108: // LINKTYPE_NULL / BSD Loopback
		if len(packet) < 4 {
			return 0, 0, nil
		}
		linkHeaderLen = 4
		switch family := binary.LittleEndian.Uint32(packet[0:4]); family {
		case 2, 0x02000000:
			ethType = 0x0800 // IPv4
		case 24, 28, 30:
			ethType = 0x86dd // IPv6
		}

	case 113: // LINKTYPE_LINUX_SLL (Linux Cooked v1)
		if len(packet) < 16 {
			return 0, 0, nil
		}
		linkHeaderLen = 16
		ethType = binary.BigEndian.Uint16(packet[14:16])

	case 276: // LINKTYPE_LINUX_SLL2 (Linux Cooked v2)
		if len(packet) < 20 {
			return 0, 0, nil
		}
		linkHeaderLen = 20
		ethType = binary.BigEndian.Uint16(packet[0:2])

	case 12, 14, 228: // LINKTYPE_RAW IP
		linkHeaderLen = 0
		if len(packet) > 0 {
			switch ipVer := packet[0] >> 4; ipVer {
			case 4:
				ethType = 0x0800
			case 6:
				ethType = 0x86dd
			}
		}

	default:
		if len(packet) >= 20 && packet[0]>>4 == 4 {
			linkHeaderLen = 0
			ethType = 0x0800
		} else {
			return 0, 0, nil
		}
	}

	if len(packet) <= linkHeaderLen {
		return 0, 0, nil
	}

	ipData := packet[linkHeaderLen:]
	var tcpData []byte
	var proto byte

	if ethType == 0x0800 || (len(ipData) >= 20 && ipData[0]>>4 == 4) { // IPv4
		if len(ipData) < 20 {
			return 0, 0, nil
		}
		ihl := int(ipData[0]&0x0f) * 4
		if ihl < 20 || ihl > len(ipData) {
			return 0, 0, nil
		}
		proto = ipData[9]
		tcpData = ipData[ihl:]
	} else if ethType == 0x86dd || (len(ipData) >= 40 && ipData[0]>>4 == 6) { // IPv6
		if len(ipData) < 40 {
			return 0, 0, nil
		}
		proto = ipData[6]
		tcpData = ipData[40:]
	} else {
		return 0, 0, nil
	}

	if proto != 6 || len(tcpData) < 20 { // Protocol 6 = TCP
		return 0, 0, nil
	}

	srcPort := binary.BigEndian.Uint16(tcpData[0:2])
	dstPort := binary.BigEndian.Uint16(tcpData[2:4])

	dataOffset := int(tcpData[12]>>4) * 4
	if dataOffset < 20 || dataOffset > len(tcpData) {
		return 0, 0, nil
	}

	return srcPort, dstPort, tcpData[dataOffset:]
}

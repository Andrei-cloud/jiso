package analyzer

import (
	"bytes"
	"encoding/binary"
	"errors"
	"fmt"
	"io"
)

// PCAPNG block types and section byte-order magic values. The magic is
// decoded as little-endian: the canonical value means a little-endian file,
// its byte-swapped form means big-endian.
const (
	pcapngSHBType = 0x0A0D0D0A
	pcapngEPBType = 0x00000006

	pcapngByteOrderLE = 0x1A2B3C4D
	pcapngByteOrderBE = 0x4D3C2B1A

	// Sanity cap for a single block/record: guards against a crafted file
	// requesting a multi-GiB allocation.
	maxPCAPNGBlockLen = 128 << 20
	maxPCAPRecordLen  = 128 << 20
)

// ExtractTCPPayloadsFromPCAPFiltered extracts TCP payloads matching the given directional filter
func ExtractTCPPayloadsFromPCAPFiltered(r io.Reader, dir TrafficDirection) ([]byte, error) {
	return extractTCPPayloadsFiltered(r, dir, parsePCAPPackets)
}

// ExtractTCPPayloadsFromPCAPNGFiltered extracts TCP payloads from PCAPNG with directional filtering
func ExtractTCPPayloadsFromPCAPNGFiltered(r io.Reader, dir TrafficDirection) ([]byte, error) {
	return extractTCPPayloadsFiltered(r, dir, parsePCAPNGPackets)
}

func extractTCPPayloadsFiltered(r io.Reader, dir TrafficDirection, parser func(io.Reader, func(uint16, uint16, []byte)) error) ([]byte, error) {
	var payloadBuffer bytes.Buffer
	collector := func(srcPort, dstPort uint16, payload []byte) {
		if len(payload) == 0 {
			return
		}
		if dir.Mode == DirectionDst && dstPort != dir.TargetPort {
			return
		}
		if dir.Mode == DirectionSrc && srcPort != dir.TargetPort {
			return
		}
		payloadBuffer.Write(payload)
	}

	err := parser(r, collector)
	if err != nil && !errors.Is(err, io.EOF) && !errors.Is(err, io.ErrUnexpectedEOF) {
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
		if magicLE != pcapMagicMicroSecBE && magicLE != pcapMagicNanoSecBE {
			return fmt.Errorf("unsupported PCAP magic number: 0x%x", magicBE)
		}
		byteOrder = binary.LittleEndian
	}

	linkType := byteOrder.Uint32(globalHdr[20:24])
	packetBuf := make([]byte, 65536)

	for {
		var recordHdr [16]byte
		if _, err := io.ReadFull(r, recordHdr[:]); err != nil {
			return err
		}

		inclLen := byteOrder.Uint32(recordHdr[8:12])
		if inclLen == 0 {
			break // trailing zero padding after the last record
		}
		if inclLen > maxPCAPRecordLen {
			return fmt.Errorf("pcap: record length %d exceeds sanity cap %d", inclLen, maxPCAPRecordLen)
		}

		if inclLen > uint32(cap(packetBuf)) {
			packetBuf = make([]byte, inclLen)
		}
		slice := packetBuf[:inclLen]
		if _, err := io.ReadFull(r, slice); err != nil {
			break // truncated final record: keep what we have
		}

		srcPort, dstPort, tcpPayload := extractTCPPayloadFromPacket(slice, linkType)
		if len(tcpPayload) > 0 {
			fn(srcPort, dstPort, tcpPayload)
		}
	}
	//nolint:nilerr // a truncated final record is the end of the capture, not a
	// failure: every record already handed to the callback is the result
	return nil
}

func parsePCAPNGPackets(r io.Reader, fn func(srcPort, dstPort uint16, payload []byte)) error {
	dataBuf := make([]byte, 65536)
	var byteOrder binary.ByteOrder = binary.LittleEndian
	firstBlock := true

	for {
		var blockHdr [8]byte
		if _, err := io.ReadFull(r, blockHdr[:]); err != nil {
			return err
		}
		blockType := byteOrder.Uint32(blockHdr[0:4])
		blockLen := byteOrder.Uint32(blockHdr[4:8])

		// The Section Header Block carries the file byte order; every
		// later field width (including this block's length on BE files)
		// must be decoded with it.
		bomRead := 0
		if firstBlock {
			firstBlock = false
			if blockType == pcapngSHBType {
				var bom [4]byte
				if _, err := io.ReadFull(r, bom[:]); err != nil {
					return err
				}
				switch binary.LittleEndian.Uint32(bom[:]) {
				case pcapngByteOrderLE:
					byteOrder = binary.LittleEndian
				case pcapngByteOrderBE:
					byteOrder = binary.BigEndian
					blockLen = binary.BigEndian.Uint32(blockHdr[4:8])
				default:
					return fmt.Errorf("pcapng: invalid byte-order magic 0x%08x", binary.LittleEndian.Uint32(bom[:]))
				}
				bomRead = 4
			}
		}

		if blockLen == 0 {
			break // trailing zero padding after the last block
		}
		if blockLen < 12 {
			return fmt.Errorf("pcapng: block type 0x%08x has invalid total length %d", blockType, blockLen)
		}
		if blockLen > maxPCAPNGBlockLen {
			return fmt.Errorf("pcapng: block type 0x%08x length %d exceeds sanity cap %d", blockType, blockLen, maxPCAPNGBlockLen)
		}

		// Read the block body plus the trailing Total Block Length copy;
		// the old code left the trailing length unread and desynchronized
		// the stream after the first block.
		rest := int(blockLen) - 8 - bomRead
		var slice []byte
		if rest <= cap(dataBuf) {
			slice = dataBuf[:rest]
		} else {
			slice = make([]byte, rest)
		}
		if _, err := io.ReadFull(r, slice); err != nil {
			break // truncated final block: keep what we have
		}

		if blockType == pcapngEPBType {
			emitEnhancedPacket(slice, byteOrder, fn)
		}
	}
	//nolint:nilerr // a truncated final block is the end of the capture, not a
	// failure: every block already handed to the callback is the result
	return nil
}

func extractTCPPayloadFromPacket(packet []byte, linkType uint32) (srcPort, dstPort uint16, payload []byte) {
	linkHeaderLen, ethType, ok := linkLayerHeader(packet, linkType)
	if !ok {
		return 0, 0, nil
	}

	ipData := packet[linkHeaderLen:]

	// The link layer's ethertype normally says which IP version follows; on a
	// link type that carries no ethertype, the version nibble in the data is all
	// there is to go on. Anything that is neither version is not a packet we can
	// find TCP in.
	var tcpData []byte
	var proto byte

	switch {
	case ethType == 0x0800 || (len(ipData) >= 20 && ipData[0]>>4 == 4): // IPv4
		if len(ipData) < 20 {
			return 0, 0, nil
		}
		ihl := int(ipData[0]&0x0f) * 4
		if ihl < 20 || ihl > len(ipData) {
			return 0, 0, nil
		}
		proto = ipData[9]
		tcpData = ipData[ihl:]

	case ethType == 0x86dd || (len(ipData) >= 40 && ipData[0]>>4 == 6): // IPv6
		if len(ipData) < 40 {
			return 0, 0, nil
		}
		proto = ipData[6]
		tcpData = ipData[40:]

	default:
		return 0, 0, nil
	}

	if proto != 6 || len(tcpData) < 20 { // Protocol 6 = TCP
		return 0, 0, nil
	}

	srcPort = binary.BigEndian.Uint16(tcpData[0:2])
	dstPort = binary.BigEndian.Uint16(tcpData[2:4])

	dataOffset := int(tcpData[12]>>4) * 4
	if dataOffset < 20 || dataOffset > len(tcpData) {
		return 0, 0, nil
	}

	return srcPort, dstPort, tcpData[dataOffset:]
}

// linkLayerHeader resolves a link-layer type to the length of its header and the
// ethertype (or IP version) that follows it. ok=false when the packet is too
// short for that link type or is a shape with no IP payload that could carry TCP.
func linkLayerHeader(packet []byte, linkType uint32) (headerLen int, ethType uint16, ok bool) {
	switch linkType {
	case 1: // LINKTYPE_ETHERNET
		return ethernetHeader(packet)

	case 0, 108: // LINKTYPE_NULL / BSD Loopback
		if len(packet) < 4 {
			return 0, 0, false
		}
		headerLen = 4
		switch family := binary.LittleEndian.Uint32(packet[0:4]); family {
		case 2, 0x02000000:
			ethType = 0x0800 // IPv4
		case 24, 28, 30:
			ethType = 0x86dd // IPv6
		}

	case 113: // LINKTYPE_LINUX_SLL (Linux Cooked v1)
		if len(packet) < 16 {
			return 0, 0, false
		}
		headerLen = 16
		ethType = binary.BigEndian.Uint16(packet[14:16])

	case 276: // LINKTYPE_LINUX_SLL2 (Linux Cooked v2)
		if len(packet) < 20 {
			return 0, 0, false
		}
		headerLen = 20
		ethType = binary.BigEndian.Uint16(packet[0:2])

	case 12, 14, 228: // LINKTYPE_RAW IP
		headerLen = 0
		if len(packet) > 0 {
			switch ipVer := packet[0] >> 4; ipVer {
			case 4:
				ethType = 0x0800
			case 6:
				ethType = 0x86dd
			}
		}

	default:
		if len(packet) < 20 || packet[0]>>4 != 4 {
			return 0, 0, false
		}
		headerLen = 0
		ethType = 0x0800
	}

	if len(packet) <= headerLen {
		return 0, 0, false
	}

	return headerLen, ethType, true
}

// emitEnhancedPacket unpacks a pcapng Enhanced Packet Block: it re-tries the
// link types until one yields a TCP payload, then hands it to the callback.
func emitEnhancedPacket(slice []byte, byteOrder binary.ByteOrder, fn func(srcPort, dstPort uint16, payload []byte)) {
	if len(slice) < 20 {
		return
	}
	capLen := byteOrder.Uint32(slice[12:16])
	if int(capLen)+20 > len(slice) {
		return
	}
	packetData := slice[20 : 20+capLen]
	srcPort, dstPort, tcpPayload := extractTCPPayloadFromPacket(packetData, 1)
	if len(tcpPayload) == 0 {
		srcPort, dstPort, tcpPayload = extractTCPPayloadFromPacket(packetData, 0)
	}
	if len(tcpPayload) > 0 {
		fn(srcPort, dstPort, tcpPayload)
	}
}

// ethernetHeader resolves an Ethernet frame's header length and ethertype,
// stepping over an 802.1Q VLAN tag when present. ok=false when the frame is too
// short to carry the header it announces.
func ethernetHeader(packet []byte) (headerLen int, ethType uint16, ok bool) {
	if len(packet) < 14 {
		return 0, 0, false
	}
	headerLen = 14
	ethType = binary.BigEndian.Uint16(packet[12:14])
	if ethType == 0x8100 { // 802.1Q VLAN Tag
		if len(packet) < 18 {
			return 0, 0, false
		}
		headerLen = 18
		ethType = binary.BigEndian.Uint16(packet[16:18])
	}

	return headerLen, ethType, true
}

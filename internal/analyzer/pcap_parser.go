package analyzer

import (
	"bytes"
	"encoding/binary"
	"fmt"
	"io"
)

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

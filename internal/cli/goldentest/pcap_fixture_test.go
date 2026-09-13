package goldentest

import (
	"bytes"
	"encoding/binary"
	"fmt"
	"os"

	"github.com/moov-io/iso8583"

	"jiso/internal/utils"
)

// Fixture topology (PAR-307): two server ports carrying ASCII4-framed ISO8583
// over TCP — 8080 with three request/response exchanges and 9999 with one.
// `analyze --yes` must therefore pick 8080 (highest message count), and
// scenario mode must correlate three pairs on it.
const (
	analyzePcapMainPort  = 8080
	analyzePcapMinorPort = 9999
)

// buildAnalyzePCAP writes a minimal but real libpcap capture (Ethernet +
// IPv4 + TCP) with packed ISO8583 payloads framed by 4-ASCII-digit lengths,
// generated from the same spec fixture the analyze cases pass via --spec.
func buildAnalyzePCAP(pcapPath, specPath string) error {
	spec, err := utils.CreateSpecFromFile(specPath)
	if err != nil {
		return fmt.Errorf("load spec for pcap: %w", err)
	}

	var packets [][]byte

	add := func(seq int, srcPort, dstPort uint16, mti, stan string) error {
		payload, err := packAnalyzeMessage(spec, mti, stan)
		if err != nil {
			return err
		}
		packets = append(packets, ethIPv4TCPPacket(srcPort, dstPort, payload))
		_ = seq

		return nil
	}

	clientMain, clientMinor := uint16(50001), uint16(50002)

	for i, stan := range []string{"000001", "000002", "000003"} {
		if err := add(i*2, clientMain, analyzePcapMainPort, "0200", stan); err != nil {
			return err
		}
		if err := add(i*2+1, analyzePcapMainPort, clientMain, "0210", stan); err != nil {
			return err
		}
	}
	if err := add(6, clientMinor, analyzePcapMinorPort, "0200", "100001"); err != nil {
		return err
	}
	if err := add(7, analyzePcapMinorPort, clientMinor, "0210", "100001"); err != nil {
		return err
	}

	f, err := os.Create(pcapPath)
	if err != nil {
		return fmt.Errorf("create pcap: %w", err)
	}
	defer func() { _ = f.Close() }()

	// Global header: magic d4c3b2a1 (little-endian microsecond pcap); the
	// parser derives little-endian field order from that magic, so all
	// header/record fields are written little-endian.
	var global [24]byte
	copy(global[0:4], []byte{0xd4, 0xc3, 0xb2, 0xa1})
	binary.LittleEndian.PutUint16(global[4:6], 2)
	binary.LittleEndian.PutUint16(global[6:8], 4)
	binary.LittleEndian.PutUint32(global[16:20], 65535)
	binary.LittleEndian.PutUint32(global[20:24], 1) // LINKTYPE_ETHERNET
	if _, err := f.Write(global[:]); err != nil {
		return fmt.Errorf("write pcap global header: %w", err)
	}

	for i, pkt := range packets {
		var record [16]byte
		binary.LittleEndian.PutUint32(record[0:4], uint32(1700000000+i))
		binary.LittleEndian.PutUint32(record[8:12], uint32(len(pkt)))
		binary.LittleEndian.PutUint32(record[12:16], uint32(len(pkt)))
		if _, err := f.Write(record[:]); err != nil {
			return fmt.Errorf("write pcap record: %w", err)
		}
		if _, err := f.Write(pkt); err != nil {
			return fmt.Errorf("write pcap packet: %w", err)
		}
	}

	return f.Close()
}

// packAnalyzeMessage packs one ISO8583 message with the fixture spec and
// prefixes it with the ascii4 (4 ASCII digits) length header.
func packAnalyzeMessage(spec *iso8583.MessageSpec, mti, stan string) ([]byte, error) {
	msg := iso8583.NewMessage(spec)
	msg.MTI(mti)
	if err := msg.Field(11, stan); err != nil {
		return nil, fmt.Errorf("set STAN: %w", err)
	}

	if utils.IsResponseMTI(mti) {
		if err := msg.Field(39, "00"); err != nil {
			return nil, fmt.Errorf("set DE39: %w", err)
		}
	} else {
		for id, value := range map[int]string{
			2: "4242424242424242", 3: "000000", 4: "1000",
			12: "120000", 13: "0907", 41: "GOLDEN01",
		} {
			if err := msg.Field(id, value); err != nil {
				return nil, fmt.Errorf("set DE%d: %w", id, err)
			}
		}
	}

	packed, err := msg.Pack()
	if err != nil {
		return nil, fmt.Errorf("pack %s: %w", mti, err)
	}

	return append([]byte(fmt.Sprintf("%04d", len(packed))), packed...), nil
}

// ethIPv4TCPPacket frames a payload as Ethernet + IPv4 + TCP. Checksums are
// zero: the analyzer's parser never validates them.
func ethIPv4TCPPacket(srcPort, dstPort uint16, payload []byte) []byte {
	var buf bytes.Buffer

	eth := []byte{
		0, 0, 0, 0, 0, 0, // dst mac
		0, 0, 0, 0, 0, 0, // src mac
		0x08, 0x00, // IPv4
	}
	buf.Write(eth)

	ipLen := 20 + 20 + len(payload)
	ip := make([]byte, 20)
	ip[0] = 0x45
	binary.BigEndian.PutUint16(ip[2:4], uint16(ipLen))
	ip[8] = 64 // TTL
	ip[9] = 6  // TCP
	ip[12], ip[13] = 10, 0
	ip[14], ip[15] = 0, 1
	buf.Write(ip)

	tcp := make([]byte, 20)
	binary.BigEndian.PutUint16(tcp[0:2], srcPort)
	binary.BigEndian.PutUint16(tcp[2:4], dstPort)
	tcp[12] = 0x50 // data offset: 20 bytes
	binary.BigEndian.PutUint16(tcp[14:16], 65535)
	buf.Write(tcp)

	buf.Write(payload)

	return buf.Bytes()
}

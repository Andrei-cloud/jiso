package analyzer

import (
	"bytes"
	"encoding/binary"
	"net"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// buildISO8583FlowPacket returns a minimal Ethernet/IPv4/TCP frame whose
// TCP payload is payload.
func buildISO8583FlowPacket(payload string) []byte {
	pkt := make([]byte, 0, 14+20+20+len(payload))

	eth := make([]byte, 14)
	binary.BigEndian.PutUint16(eth[12:14], 0x0800)
	pkt = append(pkt, eth...)

	ip := make([]byte, 20)
	ip[0] = 0x45
	binary.BigEndian.PutUint16(ip[2:4], uint16(20+20+len(payload)))
	ip[8] = 64
	ip[9] = 6
	copy(ip[12:16], net.ParseIP("127.0.0.1").To4())
	copy(ip[16:20], net.ParseIP("127.0.0.1").To4())
	pkt = append(pkt, ip...)

	tcp := make([]byte, 20)
	binary.BigEndian.PutUint16(tcp[0:2], 4000)
	binary.BigEndian.PutUint16(tcp[2:4], 5000)
	tcp[12] = 0x50
	pkt = append(pkt, tcp...)

	pkt = append(pkt, []byte(payload)...)
	return pkt
}

// buildPCAPNG assembles an SHB plus one EPB per payload with the given byte
// order. The old parser never consumed the trailing Total Block Length copy,
// so it desynchronized after the first block and extracted nothing from
// multi-block files; big-endian files were mis-parsed entirely.
func buildPCAPNG(order binary.ByteOrder, payloads ...string) []byte {
	var buf bytes.Buffer
	u32 := func(v uint32) { _ = binary.Write(&buf, order, v) }

	shbBodyLen := 4 + 4 + 8 // byte-order magic + version + section length
	shbLen := 8 + shbBodyLen + 4
	u32(0x0A0D0D0A) // SHB type (palindromic)
	u32(uint32(shbLen))
	u32(0x1A2B3C4D) // byte-order magic, written in the file's own order
	u32(0x00010000) // version 1.0
	_ = binary.Write(&buf, order, uint64(0xFFFFFFFFFFFFFFFF))
	u32(uint32(shbLen))

	for _, payload := range payloads {
		pkt := buildISO8583FlowPacket(payload)
		pad := (4 - (len(pkt) % 4)) % 4
		epbBodyLen := 20 + len(pkt) + pad
		epbLen := 8 + epbBodyLen + 4
		u32(0x00000006) // EPB type
		u32(uint32(epbLen))
		u32(0) // interface id
		u32(0) // timestamp high
		u32(0) // timestamp low
		u32(uint32(len(pkt)))
		u32(uint32(len(pkt)))
		buf.Write(pkt)
		buf.Write(make([]byte, pad))
		u32(uint32(epbLen))
	}
	return buf.Bytes()
}

func TestExtractPCAPNGMultipleBlocksLittleEndian(t *testing.T) {
	t.Parallel()

	file := buildPCAPNG(binary.LittleEndian, "PING", "PONG")

	payload, err := ExtractTCPPayloadsFromPCAPNGFiltered(bytes.NewReader(file), TrafficDirection{Mode: "all"})
	require.NoError(t, err)
	assert.Equal(t, "PINGPONG", string(payload))
}

func TestExtractPCAPNGMultipleBlocksBigEndian(t *testing.T) {
	t.Parallel()

	file := buildPCAPNG(binary.BigEndian, "PING", "PONG")

	payload, err := ExtractTCPPayloadsFromPCAPNGFiltered(bytes.NewReader(file), TrafficDirection{Mode: "all"})
	require.NoError(t, err)
	assert.Equal(t, "PINGPONG", string(payload))
}

func TestExtractPCAPNGTruncatedTailKeepsCollectedPayloads(t *testing.T) {
	t.Parallel()

	file := buildPCAPNG(binary.LittleEndian, "PING", "PONG")
	// Chop the final block mid-body: a truncated tail must not discard
	// the packets already collected, and must not error.
	truncated := file[:len(file)-10]

	payload, err := ExtractTCPPayloadsFromPCAPNGFiltered(bytes.NewReader(truncated), TrafficDirection{Mode: "all"})
	require.NoError(t, err)
	assert.Equal(t, "PING", string(payload))
}

func TestExtractPCAPNGRejectsOversizedBlock(t *testing.T) {
	t.Parallel()

	var buf bytes.Buffer
	u32 := func(v uint32) { _ = binary.Write(&buf, binary.LittleEndian, v) }

	u32(0x0A0D0D0A)
	u32(28)
	u32(0x4D3C2B1A)
	u32(0x00010000)
	_ = binary.Write(&buf, binary.LittleEndian, uint64(0xFFFFFFFFFFFFFFFF))
	u32(28)

	// EPB claiming a 1 GiB body: must fail fast instead of allocating.
	u32(0x00000006)
	u32(1 << 30)
	buf.Write(make([]byte, 64))

	_, err := ExtractTCPPayloadsFromPCAPNGFiltered(bytes.NewReader(buf.Bytes()), TrafficDirection{Mode: "all"})
	require.Error(t, err)
}

func TestExtractPCAPRejectsOversizedRecord(t *testing.T) {
	t.Parallel()

	var buf bytes.Buffer
	le := binary.LittleEndian
	u32 := func(v uint32) { _ = binary.Write(&buf, le, v) }

	_ = binary.Write(&buf, le, uint32(0xa1b2c3d4)) // magic, little-endian file
	_ = binary.Write(&buf, le, uint16(2))          // version major
	_ = binary.Write(&buf, le, uint16(4))          // version minor
	u32(0)                                         // thiszone
	u32(0)                                         // sigfigs
	u32(65535)                                     // snaplen
	u32(1)                                         // LINKTYPE_ETHERNET

	// Record header with an inclLen past the sanity cap.
	u32(0) // ts sec
	u32(0) // ts usec
	u32(0x0800_0001)
	u32(0x0800_0001)
	buf.Write(make([]byte, 64))

	_, err := ExtractTCPPayloadsFromPCAPFiltered(bytes.NewReader(buf.Bytes()), TrafficDirection{Mode: "all"})
	require.Error(t, err)
}

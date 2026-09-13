package analyzer

import (
	"bytes"
	"errors"
	"fmt"
	"io"
	"sort"
	"strconv"

	"github.com/moov-io/iso8583"
	isoerrors "github.com/moov-io/iso8583/errors"

	"jiso/internal/utils"
)

// CapturedFlow represents an aggregated transaction flow captured from network traffic
type CapturedFlow struct {
	MTI      string
	DE3      string
	DE22     string
	Messages []*iso8583.Message
	Count    int
}

// StreamAnalyzer extracts and aggregates ISO8583 messages from raw byte streams
type StreamAnalyzer struct {
	spec *iso8583.MessageSpec
}

// UnparsableSample is one framed message whose Unpack failed, captured
// for the §J reviewer (UAT round 6: the operator must see WHERE framing
// breaks and WHAT the analyzer choked on, not just a black-box count).
// Offset and Length are positions in the byte stream being carved (for
// a PCAP, the concatenated TCP payload stream).
//
// UAT round 7: a framed message usually unpacks PARTIALLY before it
// fails — the library fills MTI, the bitmap, then present fields in
// ascending order and stops at the first that will not decode. So the
// sample also carries the fields that DID parse (Fields, describe form)
// and the message-relative byte offset where parsing stopped (FailedAt,
// -1 when unknown); the reviewer describes the parsed fields and paints
// the head bytes at or after FailedAt as unparsed.
type UnparsableSample struct {
	Offset   int64
	Length   int
	Reason   string
	Head     []byte
	FailedAt int
	Fields   []SampleField
}

// SampleField is one field that unpacked BEFORE the failure, flattened to
// the same ID/Name/Value projection the send review's describe uses.
type SampleField struct {
	ID    string
	Name  string
	Value string
}

// Sample caps: 50 samples ("showing first 50 of 96"), each head widened
// to 128 bytes so the reviewer can SEE the unparsed region it marks — a
// 48-byte window hid failures past byte 48 — while still bounding the
// memory a pathological capture could otherwise make the wizard hold.
const (
	MaxUnparsableSamples = 50
	unparsableHeadBytes  = 128
)

// UnparsableCollector accumulates failure samples across one
// enumeration; a nil collector is a no-op (the counted variants). The
// extractor returns the true failure count itself, so the collector
// holds only the capped samples a reviewer shows; the field is
// unexported so the accessor stays nil-safe.
type UnparsableCollector struct {
	samples []UnparsableSample
}

// Samples returns the kept failure samples (nil for a nil collector);
// at most MaxUnparsableSamples are held.
func (c *UnparsableCollector) Samples() []UnparsableSample {
	if c == nil {
		return nil
	}

	return c.samples
}

// add records one failure's sample while under the cap; the count is
// tracked by the extractor, not here. The partially-unpacked message is
// inspected for the fields that parsed before the failure and the byte
// offset where unpacking stopped, so the reviewer can describe the good
// fields and mark the rest unparsed.
func (c *UnparsableCollector) add(offset int64, msg *iso8583.Message, payload []byte, reason error) {
	if c == nil || len(c.samples) >= MaxUnparsableSamples {
		return
	}
	head := payload
	if len(head) > unparsableHeadBytes {
		head = head[:unparsableHeadBytes]
	}
	failed := failedFieldID(reason)
	c.samples = append(c.samples, UnparsableSample{
		Offset:   offset,
		Length:   len(payload),
		Reason:   reason.Error(),
		Head:     append([]byte(nil), head...),
		FailedAt: parseStop(msg, failed),
		Fields:   parsedFields(msg, failed),
	})
}

// failedFieldID returns the field number the unpacker stopped on (the
// moov UnpackError carries it), or -1 when the error is not an
// UnpackError and no reliable stop point exists.
func failedFieldID(reason error) int {
	var ue *isoerrors.UnpackError
	if errors.As(reason, &ue) {
		if n, err := strconv.Atoi(ue.FieldID); err == nil {
			return n
		}
	}

	return -1
}

// parsedFields describes the fields that unpacked BEFORE the failing one
// (the library leaves the failing field half-filled, so it is excluded),
// in ascending field-number order. A field whose value will not stringify
// is skipped rather than dropping the whole sample.
func parsedFields(msg *iso8583.Message, failed int) []SampleField {
	if msg == nil {
		return nil
	}
	fields := msg.GetFields()
	ids := make([]int, 0, len(fields))
	for id := range fields {
		if id != failed {
			ids = append(ids, id)
		}
	}
	sort.Ints(ids)

	views := make([]SampleField, 0, len(ids))
	for _, id := range ids {
		f := fields[id]
		value, err := f.String()
		if err != nil {
			continue
		}
		name := ""
		if spec := f.Spec(); spec != nil {
			name = spec.Description
		}
		views = append(views, SampleField{ID: strconv.Itoa(id), Name: name, Value: value})
	}

	return views
}

// parseStop reconstructs the message-relative byte offset where unpacking
// stopped. The library unpacks MTI, then the bitmap, then present fields
// in ascending number and stops at the first failure, so the packed
// length of every field parsed BEFORE the failing one sums to exactly the
// offset where the unparsed bytes begin (Pack round-trips the bytes each
// field consumed). Returns -1 when there is no reliable stop point.
func parseStop(msg *iso8583.Message, failed int) int {
	if msg == nil || failed < 0 {
		return -1
	}
	stop := 0
	for id, f := range msg.GetFields() {
		if id == failed {
			continue
		}
		if b, err := f.Pack(); err == nil {
			stop += len(b)
		}
	}

	return stop
}

// countingReader tracks the byte position so a sample can name its
// offset while the extractor reads through an io.Reader.
type countingReader struct {
	r io.Reader
	n int64
}

func (c *countingReader) Read(p []byte) (int, error) {
	n, err := c.r.Read(p)
	c.n += int64(n)

	return n, err
}

// NewStreamAnalyzer creates a new StreamAnalyzer with an ISO8583 message spec
func NewStreamAnalyzer(spec *iso8583.MessageSpec) *StreamAnalyzer {
	return &StreamAnalyzer{
		spec: spec,
	}
}

// ExtractMessagesFromReader extracts framed ISO8583 messages from an io.Reader using the specified header type
func (a *StreamAnalyzer) ExtractMessagesFromReader(r io.Reader, headerType string) ([]*iso8583.Message, error) {
	messages, _, err := a.ExtractMessagesFromReaderCounted(r, headerType)

	return messages, err
}

// ExtractMessagesFromReaderCounted is the counted variant of
// ExtractMessagesFromReader: the second result is the number of framed
// messages whose Unpack failed (the §J wizard's "N parsed, M unparsable"
// enumeration line, SCR-510). Framing behaviour is unchanged.
func (a *StreamAnalyzer) ExtractMessagesFromReaderCounted(r io.Reader, headerType string) ([]*iso8583.Message, int, error) {
	return a.ExtractMessagesFromReaderSampled(r, headerType, nil)
}

// ExtractMessagesFromReaderSampled is the reader core: it counts
// framed-but-unpackable messages and, when a collector is given, keeps
// capped samples (offset/length/reason/head) for the §J reviewer
// (UAT round 6).
func (a *StreamAnalyzer) ExtractMessagesFromReaderSampled(r io.Reader, headerType string, samples *UnparsableCollector) ([]*iso8583.Message, int, error) {
	if headerType == "" {
		headerType = "binary2"
	}
	hdr, err := utils.SelectLength(headerType)
	if err != nil {
		return nil, 0, fmt.Errorf("invalid header type '%s': %w", headerType, err)
	}

	readLenFunc := utils.ReadMessageLengthWrapper(hdr)
	if headerType == "NAPS" {
		readLenFunc = utils.NapsReadLengthWrapper(readLenFunc)
	}

	cr := &countingReader{r: r}
	var messages []*iso8583.Message
	unparsed := 0

	for {
		msgLen, err := readLenFunc(cr)
		if err != nil {
			if errors.Is(err, io.EOF) || errors.Is(err, io.ErrUnexpectedEOF) {
				break
			}
			// End of readable stream or unparseable framing
			break
		}

		if msgLen <= 0 || msgLen > utils.MaxMessageSize {
			break
		}

		bodyOff := cr.n
		payload := make([]byte, msgLen)
		_, err = io.ReadFull(cr, payload)
		if err != nil {
			break
		}

		msg := iso8583.NewMessage(a.spec)
		if err := msg.Unpack(payload); err == nil {
			messages = append(messages, msg)
		} else {
			unparsed++
			samples.add(bodyOff, msg, payload, err)
		}
	}

	//nolint:nilerr // a stream that stops mid-message, or a message that will not
	// unpack, is the end of what this reader can carve: the caller asks for the
	// count precisely so that partial results are data rather than an error.
	return messages, unparsed, nil
}

// ExtractMessagesFromStream extracts ISO8583 messages from a byte slice using the given header type
func (a *StreamAnalyzer) ExtractMessagesFromStream(streamData []byte, headerType ...string) ([]*iso8583.Message, error) {
	messages, _, err := a.ExtractMessagesFromStreamCounted(streamData, headerType...)

	return messages, err
}

// ExtractMessagesFromStreamCounted is the counted variant of
// ExtractMessagesFromStream (SCR-510 §J enumeration).
func (a *StreamAnalyzer) ExtractMessagesFromStreamCounted(streamData []byte, headerType ...string) ([]*iso8583.Message, int, error) {
	return a.ExtractMessagesFromStreamSampled(streamData, nil, headerType...)
}

// ExtractMessagesFromStreamSampled is the stream core the §J reviewer
// uses (UAT round 6): it keeps capped failure samples in a collector
// (nil collector = the counted behaviour).
func (a *StreamAnalyzer) ExtractMessagesFromStreamSampled(streamData []byte, samples *UnparsableCollector, headerType ...string) ([]*iso8583.Message, int, error) {
	hType := "binary2"
	if len(headerType) > 0 && headerType[0] != "" {
		hType = headerType[0]
	}

	return a.ExtractMessagesFromReaderSampled(bytes.NewReader(streamData), hType, samples)
}

// AggregateFlows groups extracted messages by MTI + DE3 (Processing Code) + DE22 (POS Entry Mode)
func (a *StreamAnalyzer) AggregateFlows(messages []*iso8583.Message) map[string]*CapturedFlow {
	flows := make(map[string]*CapturedFlow)

	for _, msg := range messages {
		mti, _ := msg.GetMTI()
		de3 := ""
		if f := msg.GetField(3); f != nil {
			de3, _ = f.String()
		}
		de22 := ""
		if f := msg.GetField(22); f != nil {
			de22, _ = f.String()
		}

		var key string
		if de22 != "" {
			key = fmt.Sprintf("%s_%s_%s", mti, de3, de22)
		} else {
			key = fmt.Sprintf("%s_%s", mti, de3)
		}

		flow, exists := flows[key]
		if !exists {
			flow = &CapturedFlow{
				MTI:      mti,
				DE3:      de3,
				DE22:     de22,
				Messages: make([]*iso8583.Message, 0),
			}
			flows[key] = flow
		}

		flow.Messages = append(flow.Messages, msg)
		flow.Count++
	}

	return flows
}

package main

import (
	"bytes"
	"flag"
	"fmt"
	"io"
	"log"
	"net"
	"os"
	"os/signal"
	"strconv"
	"syscall"
	"time"

	"github.com/moov-io/iso8583"
	"github.com/moov-io/iso8583/network"
	"github.com/moov-io/iso8583/specs"
)

type Binary2BytesAdapter struct {
	binary2Bytes *network.Binary2Bytes
}

func (a *Binary2BytesAdapter) SetLength(length int) {
	a.binary2Bytes.SetLength(length)
}

func (a *Binary2BytesAdapter) Length() int {
	return a.binary2Bytes.Length()
}

func (a *Binary2BytesAdapter) WriteTo(w io.Writer) (int, error) {
	n, err := a.binary2Bytes.WriteTo(w)
	return n, err
}

func (a *Binary2BytesAdapter) ReadFrom(r io.Reader) (int, error) {
	n, err := a.binary2Bytes.ReadFrom(r)
	if err != nil {
		return 0, fmt.Errorf("reading from reader: %w", err)
	}

	return n, nil
}

const (
	defaultPort = 9999
	defaultHost = "localhost"
)

func hexDump(data []byte) string {
	var buf bytes.Buffer
	for i := 0; i < len(data); i += 16 {
		// offset
		fmt.Fprintf(&buf, "%08x  ", i)
		// hex bytes
		for j := 0; j < 16; j++ {
			if i+j < len(data) {
				fmt.Fprintf(&buf, "%02x ", data[i+j])
			} else {
				buf.WriteString("   ")
			}
			if j == 7 {
				buf.WriteString(" ")
			}
		}
		buf.WriteString(" |")
		// ASCII
		for j := 0; j < 16 && i+j < len(data); j++ {
			b := data[i+j]
			if b >= 32 && b <= 126 {
				buf.WriteByte(b)
			} else {
				buf.WriteByte('.')
			}
		}
		buf.WriteString("|\n")
	}
	return buf.String()
}

type TestServer struct {
	host     string
	port     int
	spec     *iso8583.MessageSpec
	header   network.Header
	listener net.Listener
	running  bool
	verbose  bool
	hex      bool
}

func NewTestServer(
	host string,
	port int,
	specFilePath string,
	header network.Header,
	verbose bool,
	hex bool,
) (*TestServer, error) {
	fd, err := os.Open(specFilePath)
	if err != nil {
		return nil, fmt.Errorf("opening spec file %s: %w", specFilePath, err)
	}
	defer fd.Close()

	raw, err := io.ReadAll(fd)
	if err != nil {
		return nil, fmt.Errorf("reading spec file %s: %w", specFilePath, err)
	}

	spec, err := specs.Builder.ImportJSON(raw)
	if err != nil {
		return nil, fmt.Errorf("failed to load spec file: %w", err)
	}

	return &TestServer{
		host:    host,
		port:    port,
		spec:    spec,
		header:  header,
		running: false,
		verbose: verbose,
		hex:     hex,
	}, nil
}

func (s *TestServer) Start() error {
	addr := fmt.Sprintf("%s:%d", s.host, s.port)

	listener, err := net.Listen("tcp", addr)
	if err != nil {
		return fmt.Errorf("failed to start server: %w", err)
	}

	s.listener = listener
	s.running = true

	fmt.Printf("Test server starting on %s\n", addr)

	go s.acceptConnections()

	return nil
}

func (s *TestServer) Stop() {
	s.running = false
	if s.listener != nil {
		s.listener.Close()
	}
}

func (s *TestServer) acceptConnections() {
	for s.running {
		conn, err := s.listener.Accept()
		if err != nil {
			if s.running {
				log.Printf("Accept error: %v", err)
			}
			continue
		}

		log.Printf("New connection from %s", conn.RemoteAddr())
		go s.handleConnection(conn)
	}
}

func (s *TestServer) handleConnection(conn net.Conn) {
	defer conn.Close()

	for s.running {
		// Read message length
		_, err := s.header.ReadFrom(conn)
		if err != nil {
			if s.running {
				log.Printf("Error reading length: %v", err)
			}
			return
		}

		messageLength := s.header.Length()

		// Read the message
		messageBuf := make([]byte, messageLength)
		_, err = conn.Read(messageBuf)
		if err != nil {
			log.Printf("Error reading message: %v", err)
			return
		}

		if s.hex {
			log.Printf("Request HEX:\n%s", hexDump(messageBuf))
		}

		// Unpack the message
		message := iso8583.NewMessage(s.spec)
		var response *iso8583.Message
		err = message.Unpack(messageBuf)
		if err != nil {
			log.Printf("Error unpacking message: %v", err)
			// Create error response
			response = iso8583.NewMessage(s.spec)
			response.MTI("0810")
			response.Field(39, "96") // Invalid message
		} else {
			// Handle the message
			s.handleMessage(message)

			if s.verbose {
				var buf bytes.Buffer
				iso8583.Describe(message, &buf, iso8583.DoNotFilterFields()...)
				log.Printf("Parsed request:\n%s", buf.String())
			}

			// Create and send response
			response = s.createResponse(message)
		}

		if s.verbose && response != nil {
			var buf bytes.Buffer
			iso8583.Describe(response, &buf, iso8583.DoNotFilterFields()...)
			log.Printf("Parsed response:\n%s", buf.String())
		}
		responsePacked, err := response.Pack()
		if err != nil {
			log.Printf("Error packing response: %v", err)
			continue
		}

		if s.hex {
			log.Printf("Response HEX:\n%s", hexDump(responsePacked))
		}

		// Write response length
		s.header.SetLength(len(responsePacked))
		_, err = s.header.WriteTo(conn)
		if err != nil {
			log.Printf("Error writing response length: %v", err)
			return
		}

		// Write response
		_, err = conn.Write(responsePacked)
		if err != nil {
			log.Printf("Error writing response: %v", err)
			return
		}
	}
}

func (s *TestServer) handleMessage(message *iso8583.Message) {
	mti, err := message.GetMTI()
	if err != nil {
		log.Printf("Failed to get MTI: %v", err)
		return
	}

	log.Printf("Received message with MTI: %s", mti)

	switch mti {
	case "0800": // Network Management Request
		s.handleNetworkManagement(message)
	case "0200": // Transaction Request
		s.handleTransaction(message)
	case "0100": // Authorization Request
		s.handleAuthorization(message)
	default:
		log.Printf("Unknown MTI: %s", mti)
	}
}

func (s *TestServer) handleNetworkManagement(request *iso8583.Message) {
	networkCode, err := request.GetString(70)
	if err != nil {
		log.Printf("Failed to get network code: %v", err)
		return
	}

	switch networkCode {
	case "1":
		log.Println("Processing Sign On request")
	case "2":
		log.Println("Processing Sign Off request")
	case "301":
		log.Println("Processing Echo/Test request")
	default:
		log.Printf("Unknown network code: %s", networkCode)
	}
}

func (s *TestServer) handleTransaction(request *iso8583.Message) {
	log.Println("Processing transaction request")
}

func (s *TestServer) handleAuthorization(request *iso8583.Message) {
	log.Println("Processing authorization request")
}

func (s *TestServer) createResponse(request *iso8583.Message) *iso8583.Message {
	responseMTI := s.getResponseMTI(request)

	response := iso8583.NewMessage(s.spec)
	response.MTI(responseMTI)

	// Copy relevant fields from request
	if stan, err := request.GetString(11); err == nil {
		response.Field(11, stan)
	}
	if date, err := request.GetString(13); err == nil {
		response.Field(13, date)
	}
	if time, err := request.GetString(12); err == nil {
		response.Field(12, time)
	}

	// Set response code
	response.Field(39, "00") // Success

	// Set current timestamp for field 7 and 12
	now := time.Now()
	transmissionTime := now.Format("0102150405")
	response.Field(7, transmissionTime)

	localTime := now.Format("150405")
	response.Field(12, localTime)

	localDate := now.Format("0102")
	response.Field(13, localDate)

	return response
}

func (s *TestServer) getResponseMTI(request *iso8583.Message) string {
	mti, _ := request.GetMTI()

	switch mti {
	case "0800":
		return "0810" // Network Management Response
	case "0200":
		return "0210" // Transaction Response
	case "0100":
		return "0110" // Authorization Response
	default:
		return "0810" // Default to network management response
	}
}

func main() {
	log.SetFlags(log.LstdFlags | log.Lmicroseconds)
	var host string
	var port int
	var specFile string
	var headerType string
	var verbose bool
	var hex bool

	flag.StringVar(&host, "host", defaultHost, "Server host")
	flag.IntVar(&port, "port", defaultPort, "Server port")
	flag.StringVar(&specFile, "spec", "../specs/spec.json", "ISO 8583 spec file path")
	flag.StringVar(&headerType, "header", "binary2", "Header type (ascii4, binary2, bcd2, NAPS)")
	flag.BoolVar(&verbose, "verbose", false, "Enable verbose output of parsed messages")
	flag.BoolVar(&hex, "hex", false, "Enable hex dump tracing for request and response")

	flag.Parse()

	// Allow positional arguments for backward compatibility
	args := flag.Args()
	if len(args) > 0 {
		specFile = args[0]
	}
	if len(args) > 1 {
		if p, err := strconv.Atoi(args[1]); err == nil {
			port = p
		}
	}
	if len(args) > 2 {
		host = args[2]
	}
	if len(args) > 3 {
		headerType = args[3]
	}

	// Create header
	var header network.Header
	switch headerType {
	case "ascii4":
		header = network.NewASCII4BytesHeader()
	case "binary2", "NAPS":
		header = &Binary2BytesAdapter{network.NewBinary2BytesHeader()}
	case "bcd2":
		header = network.NewBCD2BytesHeader()
	default:
		log.Fatalf("Unknown header type: %s", headerType)
	}

	fmt.Printf(
		"Starting JISO Test Server on %s:%d using spec file: %s and header: %s\n",
		host,
		port,
		specFile,
		headerType,
	)
	fmt.Println("Press Ctrl+C to stop")

	server, err := NewTestServer(host, port, specFile, header, verbose, hex)
	if err != nil {
		log.Fatalf("Failed to create server: %v", err)
	}

	if err := server.Start(); err != nil {
		log.Fatalf("Failed to start server: %v", err)
	}

	// Wait for interrupt signal
	sigChan := make(chan os.Signal, 1)
	signal.Notify(sigChan, syscall.SIGINT, syscall.SIGTERM)
	<-sigChan

	fmt.Println("\nStopping server...")
	server.Stop()
	fmt.Println("Server stopped")
}

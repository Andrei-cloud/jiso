package main

import (
	"fmt"
	"io"
	"log"
	"net"
	"os"
	"runtime/pprof"
	"time"

	"jiso/internal/cli"
	cfg "jiso/internal/config"
	"jiso/internal/utils"

	"github.com/moov-io/iso8583"
)

func main() {
	// 1. Start mock server on a free port
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		log.Fatalf("Failed to listen: %v", err)
	}
	defer listener.Close()

	_, portStr, err := net.SplitHostPort(listener.Addr().String())
	if err != nil {
		log.Fatalf("Failed to split host port: %v", err)
	}

	spec, err := utils.CreateSpecFromFile("./specs/spec.json")
	if err != nil {
		log.Fatalf("Failed to load spec: %v", err)
	}

	go func() {
		for {
			conn, err := listener.Accept()
			if err != nil {
				return
			}
			go handleConnection(conn, spec)
		}
	}()

	fmt.Printf("Mock ISO8583 server listening on port %s\n", portStr)

	// 2. Configure global CLI config
	config := cfg.GetConfig()
	config.SetHost("127.0.0.1")
	config.SetPort(portStr)
	config.SetSpec("./specs/spec.json")
	config.SetFile("./transactions/transaction.json")
	config.SetReconnectAttempts(3)
	config.SetConnectTimeout(5 * time.Second)
	config.SetTotalConnectTimeout(10 * time.Second)
	config.SetResponseTimeout(5 * time.Second)

	// 3. Initialize CLI
	cliTool := cli.NewCLI()
	err = cliTool.Prepare()
	if err != nil {
		log.Fatalf("Failed to prepare CLI: %v", err)
	}

	err = cliTool.Connect("binary2")
	if err != nil {
		log.Fatalf("Failed to connect CLI to server: %v", err)
	}
	defer cliTool.Close()

	// 4. Start CPU Profiling
	cpuFile, err := os.Create("cpu.prof")
	if err != nil {
		log.Fatalf("Could not create CPU profile: %v", err)
	}
	defer cpuFile.Close()

	fmt.Println("Starting CPU profile...")
	if err := pprof.StartCPUProfile(cpuFile); err != nil {
		log.Fatalf("Could not start CPU profile: %v", err)
	}
	defer pprof.StopCPUProfile()

	// 5. Start stress test worker programmatically
	// Let's run a 5-second test with 1000 TPS target using 5 workers
	fmt.Println("Starting stress test worker programmatically...")
	startTime := time.Now()
	workerID, err := cliTool.StartStressTestWorker(
		[]string{"Sign On"},
		1000,            // target TPS
		1*time.Second,   // ramp up duration
		3*time.Second,   // test duration
		5,               // concurrent workers
	)
	if err != nil {
		log.Fatalf("Failed to start stress test worker: %v", err)
	}

	fmt.Printf("Running stress test worker %s...\n", workerID)

	// Wait for the duration of the test + a bit extra to let it finish
	time.Sleep(5 * time.Second)

	fmt.Println("Stopping stress test worker...")
	_ = cliTool.StopWorker(workerID)

	pprof.StopCPUProfile()
	fmt.Println("Stopped CPU profile. Saved to cpu.prof")

	// 6. Write Memory Profile
	memFile, err := os.Create("mem.prof")
	if err != nil {
		log.Fatalf("Could not create memory profile: %v", err)
	}
	defer memFile.Close()

	fmt.Println("Writing memory profile...")
	if err := pprof.WriteHeapProfile(memFile); err != nil {
		log.Fatalf("Could not write memory profile: %v", err)
	}
	fmt.Println("Memory profile saved to mem.prof")

	fmt.Printf("Completed profiling run. Elapsed time: %v\n", time.Since(startTime))
}

func handleConnection(conn net.Conn, spec *iso8583.MessageSpec) {
	defer conn.Close()
	header := utils.NewBinary2BytesAdapter()

	for {
		// Read message length
		_, err := header.ReadFrom(conn)
		if err != nil {
			return
		}
		messageLength := header.Length()

		// Read the message
		messageBuf := make([]byte, messageLength)
		_, err = io.ReadFull(conn, messageBuf)
		if err != nil {
			return
		}

		// Unpack message
		message := iso8583.NewMessage(spec)
		err = message.Unpack(messageBuf)
		if err != nil {
			log.Printf("Server unpack error: %v", err)
			continue
		}

		// Prepare response
		response := iso8583.NewMessage(spec)
		mti, _ := message.GetMTI()
		respMti := utils.ResponseMTI(mti)
		response.MTI(respMti)

		// Copy STAN
		if stan, err := message.GetString(11); err == nil {
			response.Field(11, stan)
		}
		// Success code
		response.Field(39, "00")

		responsePacked, err := response.Pack()
		if err != nil {
			log.Printf("Server pack error: %v", err)
			continue
		}

		// Write response
		header.SetLength(len(responsePacked))
		_, err = header.WriteTo(conn)
		if err != nil {
			return
		}
		_, err = conn.Write(responsePacked)
		if err != nil {
			return
		}
	}
}

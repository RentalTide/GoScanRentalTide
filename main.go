package main

import (
	"encoding/hex"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"log"
	"net/http"
	"runtime"
	"strings"
	"time"

	"go.bug.st/serial"
)

// SerialResponse represents the response sent to the frontend
type SerialResponse struct {
	Status    string `json:"status"`
	RawData   string `json:"rawData"`
	RawHex    string `json:"rawHex"`
	Message   string `json:"message,omitempty"`
	Timestamp string `json:"timestamp"`
}

func corsMiddleware(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Access-Control-Allow-Origin", "*")
		w.Header().Set("Access-Control-Allow-Methods", "GET, POST, OPTIONS")
		w.Header().Set("Access-Control-Allow-Headers", "Content-Type, Authorization")
		if r.Method == http.MethodOptions {
			w.WriteHeader(http.StatusOK)
			return
		}
		next.ServeHTTP(w, r)
	})
}

func writeJSONError(w http.ResponseWriter, status int, err error) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	json.NewEncoder(w).Encode(map[string]string{
		"status":  "error",
		"message": err.Error(),
	})
}

func findScannerPort(portOverride string) (string, error) {
	// If a port is explicitly provided, use that
	if portOverride != "" {
		fmt.Println("Using specified port override:", portOverride)
		return portOverride, nil
	}

	ports, err := serial.GetPortsList()
	if err != nil {
		return "", err
	}
	if len(ports) == 0 {
		return "", errors.New("no serial ports found")
	}

	fmt.Println("Available ports:", ports)

	// First, look specifically for COM4
	for _, port := range ports {
		if strings.ToUpper(port) == "COM4" {
			fmt.Println("Found preferred port COM4")
			return port, nil
		}
	}
	
	// If COM4 not found, fall back to first COM port
	for _, port := range ports {
		fmt.Println("Checking port:", port)
		if runtime.GOOS == "windows" && strings.HasPrefix(strings.ToLower(port), "com") {
			return port, nil
		} else if runtime.GOOS == "darwin" && strings.Contains(strings.ToLower(port), "usbserial") {
			return port, nil
		} else if runtime.GOOS == "linux" && (strings.Contains(port, "ttyUSB") || strings.Contains(port, "usb")) {
			return port, nil
		}
	}
	return "", errors.New("no compatible port found")
}

func readWithTimeout(port serial.Port, buf []byte, timeout time.Duration) (int, error) {
	type readResult struct {
		n   int
		err error
	}
	ch := make(chan readResult, 1)
	go func() {
		n, err := port.Read(buf)
		ch <- readResult{n, err}
	}()
	select {
	case res := <-ch:
		return res.n, res.err
	case <-time.After(timeout):
		return 0, errors.New("read timeout")
	}
}

func sendScannerCommand(commandStr string, portOverride string, useMacSettings bool, readTimeout time.Duration) (string, error) {
	portName, err := findScannerPort(portOverride)
	if err != nil {
		return "", err
	}

	var mode *serial.Mode
	
	// Always use 1200 baud for COM4
	if strings.ToUpper(portName) == "COM4" {
		mode = &serial.Mode{
			BaudRate: 1200,
			DataBits: 7,
			Parity:   serial.NoParity,
			StopBits: serial.OneStopBit,
		}
		fmt.Println("Using COM4 settings: BaudRate=1200, DataBits=7")
	} else if useMacSettings {
		// Use settings from the Mac version for other ports
		mode = &serial.Mode{
			BaudRate: 9600,
			DataBits: 8,
			Parity:   serial.NoParity,
			StopBits: serial.OneStopBit,
		}
		fmt.Println("Using Mac settings: BaudRate=9600, DataBits=8")
	} else {
		// Default Windows settings for other ports
		mode = &serial.Mode{
			BaudRate: 1200,
			DataBits: 7,
			Parity:   serial.NoParity,
			StopBits: serial.OneStopBit,
		}
		fmt.Println("Using Windows settings: BaudRate=1200, DataBits=7")
	}
	
	fmt.Printf("Opening port %s with settings: BaudRate=%d, DataBits=%d\n", 
		portName, mode.BaudRate, mode.DataBits)
	
	port, err := serial.Open(portName, mode)
	if err != nil {
		return "", fmt.Errorf("open port %s failed: %w", portName, err)
	}
	defer port.Close()

	cmd := append([]byte{0x01}, append([]byte(commandStr), 0x04)...)
	fmt.Printf("Sending raw bytes (hex): %s\n", hex.EncodeToString(cmd))
	fmt.Printf("Sending raw bytes (human-readable): %q\n", string(cmd))
	
	if _, err := port.Write(cmd); err != nil {
		return "", err
	}

	var responseBuffer strings.Builder
	maxWaitTime := 5 * time.Second  // Maximum overall wait time
	maxDataWaitTime := 3 * time.Second // Maximum wait time after receiving data
	deadline := time.Now().Add(maxWaitTime)
	tmp := make([]byte, 128)

	fmt.Printf("Waiting for response... (timeout: %v, max wait: %v, max data wait: %v)\n", 
		readTimeout, maxWaitTime, maxDataWaitTime)
	fmt.Println("PLEASE SCAN YOUR LICENSE NOW - You have 10 seconds")
	
	hasReceivedData := false
	firstDataTime := time.Time{}

	for time.Now().Before(deadline) {
		n, err := readWithTimeout(port, tmp, readTimeout)
		if err != nil {
			if err.Error() == "read timeout" {
				// If we've received some data but hit a timeout, consider it complete
				if hasReceivedData {
					// Check if we've waited at least 3 seconds since first data
					if time.Since(firstDataTime) >= maxDataWaitTime {
						fmt.Println("Max data wait time reached")
						break
					}
					fmt.Println("Read timeout after receiving data, still waiting for more data...")
					continue
				}
				// Otherwise keep waiting until the overall deadline
				fmt.Println("Read timeout, still waiting for scan...")
				continue
			}
			return "", err
		}
		
		// If this is the first data we've received, record the time
		if !hasReceivedData {
			hasReceivedData = true
			firstDataTime = time.Now()
		}
		
		// Add the received bytes to our response buffer
		responseBuffer.Write(tmp[:n])
		
		// Enhanced debugging of received data
		fmt.Printf("Received %d bytes (hex): %s\n", n, hex.EncodeToString(tmp[:n]))
		
		// Try to display as readable text, but safely handle binary data
		var readable string
		for _, b := range tmp[:n] {
			if b >= 32 && b <= 126 { // Printable ASCII
				readable += string(b)
			} else {
				readable += fmt.Sprintf("\\x%02x", b)
			}
		}
		fmt.Printf("Received %d bytes (human-readable): %s\n", n, readable)
		
		// If we've been receiving data for more than maxDataWaitTime seconds, stop
		if time.Since(firstDataTime) >= maxDataWaitTime {
			fmt.Println("Max data wait time reached")
			break
		}
	}
	
	if !hasReceivedData {
		fmt.Println("No data received from scanner during timeout period")
	}
	
	result := responseBuffer.String()
	fmt.Println("===== COMPLETE RESPONSE =====")
	fmt.Printf("Raw response (hex): %s\n", hex.EncodeToString([]byte(result)))
	fmt.Printf("Raw response (string): %q\n", result)
	fmt.Println("===== END RESPONSE =====")
	
	return result, nil
}

func scannerHandler(w http.ResponseWriter, r *http.Request, portOverride string, scannerPort string, useSimpleCommand bool, useMacSettings bool, readTimeout time.Duration) {
	var command string
	if useSimpleCommand {
		command = "<TXPING>"
		fmt.Println("Using simple command format: <TXPING>")
	} else {
		command = fmt.Sprintf("<TXPING,%s>", scannerPort)
		fmt.Printf("Using port-specific command format: <TXPING,%s>\n", scannerPort)
	}
	
	fmt.Printf("Sending command: %s via port: %s\n", command, portOverride)
	result, err := sendScannerCommand(command, portOverride, useMacSettings, readTimeout)

	if err != nil {
		fmt.Printf("Error: %v\n", err)
		writeJSONError(w, http.StatusInternalServerError, err)
		return
	}
	
	// Check if the response is empty
	if strings.TrimSpace(result) == "" {
		writeJSONError(w, http.StatusNotFound, errors.New("empty response from scanner"))
		return
	}
	
	// Check for NAK (0x15) only response (scanner didn't return data)
	trimmedResult := strings.TrimSpace(result)
	if trimmedResult == string(byte(0x15)) || (len(trimmedResult) <= 2 && strings.HasPrefix(trimmedResult, "\x15")) {
		writeJSONError(w, http.StatusNotFound, errors.New("no license scanned (NAK received)"))
		return
	}

	// Create the response object with raw data
	response := SerialResponse{
		Status:    "success",
		RawData:   result,
		RawHex:    hex.EncodeToString([]byte(result)),
		Timestamp: time.Now().Format(time.RFC3339),
	}

	// Send the response as JSON
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(response)
}

func main() {
	scannerPortFlag := flag.String("scanner-port", "CON3", "Scanner port (e.g., CON3, CON4)")
	portFlag := flag.String("port", "COM4", "Serial port to connect to (e.g., COM1, /dev/ttyUSB0)")
	httpPortFlag := flag.Int("http-port", 3500, "HTTP server port")
	useSimpleCommandFlag := flag.Bool("simple-command", true, "Use simple command format without port parameter")
	useMacSettingsFlag := flag.Bool("mac-settings", true, "Use Mac serial port settings (9600 baud, 8 data bits)")
	readTimeoutFlag := flag.Int("timeout", 10, "Read timeout in seconds")
	flag.Parse()
	
	readTimeout := time.Duration(*readTimeoutFlag) * time.Second
	
	fmt.Printf("Starting with scanner port: %s, serial port: %s, HTTP port: %d, read timeout: %d seconds\n", 
		*scannerPortFlag, *portFlag, *httpPortFlag, *readTimeoutFlag)
	fmt.Printf("Simple command: %v, Mac settings: %v\n", *useSimpleCommandFlag, *useMacSettingsFlag)
	
	mux := http.NewServeMux()
	
	// Scanner endpoint - now just passes raw data
	mux.HandleFunc("/scanner/scan", func(w http.ResponseWriter, r *http.Request) {
		scannerHandler(w, r, *portFlag, *scannerPortFlag, *useSimpleCommandFlag, *useMacSettingsFlag, readTimeout)
	})
	
	log.Printf("Starting server on http://localhost:%d", *httpPortFlag)
	log.Printf("Scanner endpoint: http://localhost:%d/scanner/scan", *httpPortFlag)
	
	if err := http.ListenAndServe(fmt.Sprintf(":%d", *httpPortFlag), corsMiddleware(mux)); err != nil {
		log.Fatal(err)
	}
}
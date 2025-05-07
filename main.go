package main

import (
	"bytes"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"log"
	"net/http"
	"regexp"
	"runtime"
	"strings"
	"time"
	"flag"
	"go.bug.st/serial"
)

type LicenseData struct {
	FirstName     string `json:"firstName"`
	MiddleName    string `json:"middleName"`
	LastName      string `json:"lastName"`
	Address       string `json:"address"`
	City          string `json:"city"`
	State         string `json:"state"`
	Postal        string `json:"postal"`
	LicenseNumber string `json:"licenseNumber"`
	IssueDate     string `json:"issueDate"`
	ExpiryDate    string `json:"expiryDate"`
	Height        string `json:"height"`
	Sex           string `json:"sex"`
	LicenseClass  string `json:"licenseClass"`
	Dob           string `json:"dob"`
	RawData       string `json:"rawData"` // Added to show raw data for debugging
}

func parseLicenseData(raw string) LicenseData {
	fmt.Println("Parsing license data from raw input:")
	fmt.Println(raw)
	
	lines := strings.Split(raw, "\n")
	var parsedLines []string
	for _, line := range lines {
		trimmed := strings.TrimSpace(line)
		if trimmed != "" {
			parsedLines = append(parsedLines, trimmed)
			fmt.Println("Parsed line:", trimmed)
		}
	}

	data := make(map[string]string)
	var licenseClass string

	for _, line := range parsedLines {
		switch {
		case strings.HasPrefix(line, "DCS"):
			data["lastName"] = strings.TrimSpace(line[3:])
			fmt.Println("Found lastName:", data["lastName"])
		case strings.HasPrefix(line, "DAC"):
			data["firstName"] = strings.TrimSpace(line[3:])
			fmt.Println("Found firstName:", data["firstName"])
		case strings.HasPrefix(line, "DAD"):
			data["middleName"] = strings.TrimSpace(line[3:])
			fmt.Println("Found middleName:", data["middleName"])
		case strings.HasPrefix(line, "DBA"):
			d := strings.TrimSpace(line[3:])
			if len(d) >= 8 {
				data["expiryDate"] = fmt.Sprintf("%s/%s/%s", d[0:4], d[4:6], d[6:8])
				fmt.Println("Found expiryDate:", data["expiryDate"])
			}
		case strings.HasPrefix(line, "DBD"):
			d := strings.TrimSpace(line[3:])
			if len(d) >= 8 {
				data["issueDate"] = fmt.Sprintf("%s/%s/%s", d[0:4], d[4:6], d[6:8])
				fmt.Println("Found issueDate:", data["issueDate"])
			}
		case strings.HasPrefix(line, "DBB"):
			d := strings.TrimSpace(line[3:])
			if len(d) >= 8 {
				data["dob"] = fmt.Sprintf("%s/%s/%s", d[0:4], d[4:6], d[6:8])
				fmt.Println("Found dob:", data["dob"])
			}
		case strings.HasPrefix(line, "DBC"):
			s := strings.TrimSpace(line[3:])
			if s == "1" {
				data["sex"] = "M"
			} else if s == "2" {
				data["sex"] = "F"
			} else {
				data["sex"] = s
			}
			fmt.Println("Found sex:", data["sex"])
		case strings.HasPrefix(line, "DAU"):
			data["height"] = strings.ReplaceAll(strings.TrimSpace(line[3:]), " ", "")
			fmt.Println("Found height:", data["height"])
		case strings.HasPrefix(line, "DAG"):
			data["address"] = strings.TrimSpace(line[3:])
			fmt.Println("Found address:", data["address"])
		case strings.HasPrefix(line, "DAI"):
			data["city"] = strings.TrimSpace(line[3:])
			fmt.Println("Found city:", data["city"])
		case strings.HasPrefix(line, "DAJ"):
			data["state"] = strings.TrimSpace(line[3:])
			fmt.Println("Found state:", data["state"])
		case strings.HasPrefix(line, "DAK"):
			data["postal"] = strings.TrimSpace(line[3:])
			fmt.Println("Found postal:", data["postal"])
		case strings.HasPrefix(line, "DAQ"):
			ln := strings.TrimSpace(line[3:])
			if len(ln) == 15 {
				ln = fmt.Sprintf("%s-%s-%s", ln[0:5], ln[5:10], ln[10:15])
			}
			data["licenseNumber"] = ln
			fmt.Println("Found licenseNumber:", data["licenseNumber"])
		}

		if strings.Contains(line, "DCAG") {
			re := regexp.MustCompile(`DCAG(\w+)`)
			matches := re.FindStringSubmatch(line)
			if len(matches) > 1 {
				licenseClass = matches[1]
				fmt.Println("Found licenseClass:", licenseClass)
			}
		}
	}

	if licenseClass == "" {
		licenseClass = "NA"
	}

	return LicenseData{
		FirstName:     data["firstName"],
		MiddleName:    data["middleName"],
		LastName:      data["lastName"],
		Address:       data["address"],
		City:          data["city"],
		State:         data["state"],
		Postal:        data["postal"],
		LicenseNumber: data["licenseNumber"],
		IssueDate:     data["issueDate"],
		ExpiryDate:    data["expiryDate"],
		Height:        data["height"],
		Sex:           data["sex"],
		LicenseClass:  licenseClass,
		Dob:           data["dob"],
		RawData:       raw, // Include raw data for debugging
	}
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
	if useMacSettings {
		// Use settings from the Mac version
		mode = &serial.Mode{
			BaudRate: 9600,
			DataBits: 8,
			Parity:   serial.NoParity,
			StopBits: serial.OneStopBit,
		}
		fmt.Println("Using Mac settings: BaudRate=9600, DataBits=8")
	} else {
		// Use settings for Windows COM4
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

	var responseBuffer bytes.Buffer
	maxWaitTime := 30 * time.Second  // Maximum overall wait time
	deadline := time.Now().Add(maxWaitTime)
	tmp := make([]byte, 128)

	fmt.Printf("Waiting for response... (timeout: %v, max wait: %v)\n", 
		readTimeout, maxWaitTime)
	fmt.Println("PLEASE SCAN YOUR LICENSE NOW - You have 30 seconds")
	
	hasReceivedData := false

	for time.Now().Before(deadline) {
		n, err := readWithTimeout(port, tmp, readTimeout)
		if err != nil {
			if err.Error() == "read timeout" {
				// If we've received some data but hit a timeout, consider it complete
				if hasReceivedData {
					fmt.Println("Read timeout reached after receiving data")
					break
				}
				// Otherwise keep waiting until the overall deadline
				fmt.Println("Read timeout, still waiting for scan...")
				continue
			}
			return "", err
		}
		
		hasReceivedData = true
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
	}
	
	if !hasReceivedData {
		fmt.Println("No data received from scanner during timeout period")
	}
	
	result := responseBuffer.String()
	fmt.Println("===== COMPLETE RESPONSE =====")
	fmt.Printf("Raw response (hex): %s\n", hex.EncodeToString(responseBuffer.Bytes()))
	fmt.Printf("Raw response (string): %q\n", result)
	fmt.Println("===== END RESPONSE =====")
	
	return result, nil
}

func writeJSONError(w http.ResponseWriter, status int, err error) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	json.NewEncoder(w).Encode(map[string]string{
		"status":  "error",
		"message": err.Error(),
	})
}

func corsMiddleware(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Access-Control-Allow-Origin", "*")
		w.Header().Set("Access-Control-Allow-Methods", "GET, OPTIONS")
		w.Header().Set("Access-Control-Allow-Headers", "Content-Type, Authorization")
		if r.Method == http.MethodOptions {
			return
		}
		next.ServeHTTP(w, r)
	})
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
	
	// Check for NAK (0x15) or if the response is empty
	if strings.TrimSpace(result) == string(byte(0x15)) || strings.TrimSpace(result) == "" {
		if strings.TrimSpace(result) == "" {
			writeJSONError(w, http.StatusNotFound, errors.New("empty response from scanner"))
		} else {
			writeJSONError(w, http.StatusNotFound, errors.New("no license scanned (NAK received)"))
		}
		return
	}

	licenseData := parseLicenseData(result)
	
	// Check if all fields are empty (except licenseClass which defaults to "NA")
	allFieldsEmpty := licenseData.FirstName == "" && 
		licenseData.LastName == "" && 
		licenseData.Address == "" && 
		licenseData.City == "" && 
		licenseData.LicenseNumber == ""
	
	if allFieldsEmpty {
		// Include the raw data for debugging
		resp := map[string]interface{}{
			"status":        "warning",
			"message":       "Received data but no license fields were populated",
			"licenseData":   licenseData,
			"rawResponse":   result,
			"rawResponseHex": hex.EncodeToString([]byte(result)),
		}
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(resp)
		return
	}

	resp := map[string]interface{}{
		"status":      "success",
		"licenseData": licenseData,
		"rawData":     result, // Include raw data in the response
	}
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(resp)
}

func main() {
	scannerPortFlag := flag.String("scanner-port", "CON3", "Scanner port (e.g., CON3, CON4)")
	portFlag := flag.String("port", "COM4", "Serial port to connect to (e.g., COM1, /dev/ttyUSB0)")
	httpPortFlag := flag.Int("http-port", 3500, "HTTP server port")
	useSimpleCommandFlag := flag.Bool("simple-command", false, "Use simple command format without port parameter")
	useMacSettingsFlag := flag.Bool("mac-settings", false, "Use Mac serial port settings (9600 baud, 8 data bits)")
	readTimeoutFlag := flag.Int("timeout", 10, "Read timeout in seconds")
	flag.Parse()
	
	readTimeout := time.Duration(*readTimeoutFlag) * time.Second
	
	fmt.Printf("Starting with scanner port: %s, serial port: %s, HTTP port: %d, read timeout: %d seconds\n", 
		*scannerPortFlag, *portFlag, *httpPortFlag, *readTimeoutFlag)
	fmt.Printf("Simple command: %v, Mac settings: %v\n", *useSimpleCommandFlag, *useMacSettingsFlag)
	
	mux := http.NewServeMux()
	mux.HandleFunc("/scanner/scan", func(w http.ResponseWriter, r *http.Request) {
		scannerHandler(w, r, *portFlag, *scannerPortFlag, *useSimpleCommandFlag, *useMacSettingsFlag, readTimeout)
	})
	
	log.Printf("Starting server on http://localhost:%d", *httpPortFlag)
	if err := http.ListenAndServe(fmt.Sprintf(":%d", *httpPortFlag), corsMiddleware(mux)); err != nil {
		log.Fatal(err)
	}
}
package main

import (
	"bytes"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"log"
	"net/http"
	"os"
	"regexp"
	"runtime"
	"strings"
	"time"

	"go.bug.st/serial"
)

var manualPort string
var debugMode = false // Enable with -debug flag

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
}

func parseLicenseData(raw string) LicenseData {
	lines := strings.Split(raw, "\n")
	var parsedLines []string
	for _, line := range lines {
		trimmed := strings.TrimSpace(line)
		if len(trimmed) > 0 {
			parsedLines = append(parsedLines, trimmed)
		}
	}

	data := make(map[string]string)
	var licenseClass string

	for _, line := range parsedLines {
		switch {
		case strings.HasPrefix(line, "DCS"):
			data["lastName"] = strings.TrimSpace(line[3:])
		case strings.HasPrefix(line, "DAC"):
			data["firstName"] = strings.TrimSpace(line[3:])
		case strings.HasPrefix(line, "DAD"):
			data["middleName"] = strings.TrimSpace(line[3:])
		case strings.HasPrefix(line, "DBA"):
			d := strings.TrimSpace(line[3:])
			if len(d) >= 8 {
				data["expiryDate"] = fmt.Sprintf("%s/%s/%s", d[0:4], d[4:6], d[6:8])
			}
		case strings.HasPrefix(line, "DBD"):
			d := strings.TrimSpace(line[3:])
			if len(d) >= 8 {
				data["issueDate"] = fmt.Sprintf("%s/%s/%s", d[0:4], d[4:6], d[6:8])
			}
		case strings.HasPrefix(line, "DBB"):
			d := strings.TrimSpace(line[3:])
			if len(d) >= 8 {
				data["dob"] = fmt.Sprintf("%s/%s/%s", d[0:4], d[4:6], d[6:8])
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
		case strings.HasPrefix(line, "DAU"):
			data["height"] = strings.ReplaceAll(strings.TrimSpace(line[3:]), " ", "")
		case strings.HasPrefix(line, "DAG"):
			data["address"] = strings.TrimSpace(line[3:])
		case strings.HasPrefix(line, "DAI"):
			data["city"] = strings.TrimSpace(line[3:])
		case strings.HasPrefix(line, "DAJ"):
			data["state"] = strings.TrimSpace(line[3:])
		case strings.HasPrefix(line, "DAK"):
			data["postal"] = strings.TrimSpace(line[3:])
		case strings.HasPrefix(line, "DAQ"):
			ln := strings.TrimSpace(line[3:])
			if len(ln) == 15 {
				ln = fmt.Sprintf("%s-%s-%s", ln[0:5], ln[5:10], ln[10:15])
			}
			data["licenseNumber"] = ln
		}

		if strings.Contains(line, "DCAG") {
			re := regexp.MustCompile(`DCAG(\w+)`)
			matches := re.FindStringSubmatch(line)
			if len(matches) > 1 {
				licenseClass = matches[1]
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
	}
}

func findScannerPort() (string, error) {
	if manualPort != "" {
		log.Printf("Using manually specified port: %s", manualPort)
		return manualPort, nil
	}

	ports, err := serial.GetPortsList()
	if err != nil {
		return "", fmt.Errorf("failed to list serial ports: %w", err)
	}
	log.Printf("Available serial ports: %v", ports)

	if len(ports) == 0 {
		return "", errors.New("no serial ports found")
	}

	if runtime.GOOS == "windows" {
		commonPorts := []string{"COM4", "COM3", "COM5", "COM1"}
		for _, commonPort := range commonPorts {
			for _, port := range ports {
				if strings.EqualFold(port, commonPort) {
					log.Printf("Found likely scanner port: %s", port)
					return port, nil
				}
			}
		}
		for _, port := range ports {
			if strings.HasPrefix(strings.ToUpper(port), "COM") {
				log.Printf("Using first available COM port: %s", port)
				return port, nil
			}
		}
	} else if runtime.GOOS == "darwin" {
		for _, port := range ports {
			if strings.Contains(strings.ToLower(port), "usbserial") {
				return port, nil
			}
		}
	} else {
		for _, port := range ports {
			if strings.Contains(strings.ToLower(port), "ttyusb") || strings.Contains(strings.ToLower(port), "usb") {
				return port, nil
			}
		}
	}

	if len(ports) > 0 {
		log.Printf("No preferred port found. Using first available port: %s", ports[0])
		return ports[0], nil
	}

	return "", errors.New("no compatible serial port found")
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

func sendScannerCommand(commandStr string) (string, error) {
	portName, err := findScannerPort()
	if err != nil {
		return "", err
	}

	mode := &serial.Mode{
		BaudRate: 9600,
		DataBits: 8,
		Parity:   serial.NoParity,
		StopBits: serial.OneStopBit,
	}
	port, err := serial.Open(portName, mode)
	if err != nil {
		return "", fmt.Errorf("failed to open port %s: %w", portName, err)
	}
	defer port.Close()

	cmdBuffer := []byte{0x01}
	cmdBuffer = append(cmdBuffer, []byte(commandStr)...)
	cmdBuffer = append(cmdBuffer, 0x04)

	if debugMode {
		log.Printf("Sending command: %q", commandStr)
		log.Printf("Raw bytes sent: % X", cmdBuffer)
	}

	n, err := port.Write(cmdBuffer)
	if err != nil {
		return "", fmt.Errorf("failed to write to port: %w", err)
	}
	if n != len(cmdBuffer) {
		return "", errors.New("incomplete write to port")
	}

	var responseBuffer bytes.Buffer
	readTimeout := 3 * time.Second
	maxDuration := 10 * time.Second
	deadline := time.Now().Add(maxDuration)
	tmp := make([]byte, 128)

	for {
		n, err := readWithTimeout(port, tmp, readTimeout)
		if err != nil {
			if err.Error() == "read timeout" {
				break
			}
			return "", fmt.Errorf("error reading from port: %w", err)
		}
		if debugMode {
			log.Printf("Received %d bytes: % X", n, tmp[:n])
			log.Printf("Received string chunk: %q", string(tmp[:n]))
		}
		responseBuffer.Write(tmp[:n])
		if time.Now().After(deadline) {
			break
		}
	}

	if debugMode {
		log.Printf("Full response string:\n%s", responseBuffer.String())
	}

	return responseBuffer.String(), nil
}

func writeJSONError(w http.ResponseWriter, status int, err error) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	json.NewEncoder(w).Encode(map[string]interface{}{
		"status":  "error",
		"message": err.Error(),
	})
}

func corsMiddleware(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Access-Control-Allow-Origin", "*")
		w.Header().Set("Access-Control-Allow-Methods", "GET, POST, OPTIONS")
		w.Header().Set("Access-Control-Allow-Headers", "Content-Type, Authorization")
		if r.Method == http.MethodOptions {
			return
		}
		next.ServeHTTP(w, r)
	})
}

func scannerHandler(w http.ResponseWriter, r *http.Request) {
	command := "<TXPING>"
	result, err := sendScannerCommand(command)
	if err != nil {
		writeJSONError(w, http.StatusInternalServerError, err)
		return
	}

	if strings.TrimSpace(result) == string(byte(0x15)) {
		writeJSONError(w, http.StatusNotFound, errors.New("No license scanned or scanner not triggered"))
		return
	}

	licenseData := parseLicenseData(result)
	if debugMode {
		log.Printf("Parsed license data: %+v", licenseData)
	}

	response := map[string]interface{}{
		"status":      "success",
		"licenseData": licenseData,
	}
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(response)
}

func main() {
	log.SetOutput(os.Stdout)
	log.SetFlags(log.Ldate | log.Ltime | log.Lmicroseconds | log.Lshortfile)
	log.Printf("Starting Scanner Application")

	portFlag := flag.String("port", "", "Manually specify COM port (e.g., COM4)")
	debugFlag := flag.Bool("debug", false, "Enable debug logging")
	flag.Parse()

	if *portFlag != "" {
		log.Printf("Using manually specified port: %s", *portFlag)
		manualPort = *portFlag
	}
	debugMode = *debugFlag

	ports, err := serial.GetPortsList()
	if err != nil {
		log.Printf("Warning: Could not list serial ports: %v", err)
	} else {
		log.Printf("Available serial ports: %v", ports)
	}

	mux := http.NewServeMux()
	mux.HandleFunc("/scanner/scan", scannerHandler)
	handler := corsMiddleware(mux)

	port := 3500
	log.Printf("Starting server on http://localhost:%d", port)
	if err := http.ListenAndServe(fmt.Sprintf(":%d", port), handler); err != nil {
		log.Fatal(err)
	}
}

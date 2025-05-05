package main

import (
	"bytes"
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
}

func parseLicenseData(raw string) LicenseData {
	lines := strings.Split(raw, "\n")
	var parsedLines []string
	for _, line := range lines {
		trimmed := strings.TrimSpace(line)
		if trimmed != "" {
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

func findScannerPort(portOverride string) (string, error) {
	// If a port is explicitly provided, use that
	if portOverride != "" {
		return portOverride, nil
	}

	ports, err := serial.GetPortsList()
	if err != nil {
		return "", err
	}
	if len(ports) == 0 {
		return "", errors.New("no serial ports found")
	}

	fmt.Println("Available ports:", ports) // Add this line for debugging

	for _, port := range ports {
		fmt.Println("Checking port:", port) // More debugging
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

func sendScannerCommand(commandStr string, portOverride string) (string, error) {
	portName, err := findScannerPort(portOverride)
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
		return "", fmt.Errorf("open port %s failed: %w", portName, err)
	}
	defer port.Close()

	cmd := append([]byte{0x01}, append([]byte(commandStr), 0x04)...)
	if _, err := port.Write(cmd); err != nil {
		return "", err
	}

	var responseBuffer bytes.Buffer
	deadline := time.Now().Add(10 * time.Second)
	tmp := make([]byte, 128)

	for time.Now().Before(deadline) {
		n, err := readWithTimeout(port, tmp, 3*time.Second)
		if err != nil {
			if err.Error() == "read timeout" {
				break
			}
			return "", err
		}
		responseBuffer.Write(tmp[:n])
	}
	return responseBuffer.String(), nil
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

func scannerHandler(w http.ResponseWriter, r *http.Request, portOverride string, scannerPort string) {
	command := fmt.Sprintf("<TXPING,%s>", scannerPort)
	result, err := sendScannerCommand(command, portOverride)

	if err != nil {
		writeJSONError(w, http.StatusInternalServerError, err)
		return
	}
	if strings.TrimSpace(result) == string(byte(0x15)) {
		writeJSONError(w, http.StatusNotFound, errors.New("no license scanned"))
		return
	}

	licenseData := parseLicenseData(result)
	resp := map[string]interface{}{
		"status":      "success",
		"licenseData": licenseData,
	}
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(resp)
}

func main() {
	scannerPortFlag := flag.String("scanner-port", "CON3", "Scanner port (e.g., CON3, CON4)")
	portFlag := flag.String("port", "", "Serial port to connect to (e.g., COM1, /dev/ttyUSB0)")
	httpPortFlag := flag.Int("http-port", 3500, "HTTP server port")
	flag.Parse()
	
	mux := http.NewServeMux()
	mux.HandleFunc("/scanner/scan", func(w http.ResponseWriter, r *http.Request) {
		scannerHandler(w, r, *portFlag, *scannerPortFlag)
	})
	
	log.Printf("Starting server on http://localhost:%d", *httpPortFlag)
	if err := http.ListenAndServe(fmt.Sprintf(":%d", *httpPortFlag), corsMiddleware(mux)); err != nil {
		log.Fatal(err)
	}
}
package main

import (
	"bytes"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"log"
	"net/http"
	"os"
	"os/exec"
	"regexp"
	"runtime"
	"strings"
	"time"
	"flag"
	"io/ioutil"
	"go.bug.st/serial"
)

// LicenseData type for driver's license data
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
	RawData       string `json:"rawData,omitempty"` // Added to show raw data for debugging
}

// Receipt Item represents an item on a receipt
type ReceiptItem struct {
	Name     string  `json:"name"`
	Quantity int     `json:"quantity"`
	Price    float64 `json:"price"`
	SKU      string  `json:"sku,omitempty"`
}

// ReceiptData represents the data for a receipt
type ReceiptData struct {
	TransactionID      string        `json:"transactionId"`
	Items              []ReceiptItem `json:"items"`
	Subtotal           float64       `json:"subtotal"`
	Tax                float64       `json:"tax"`
	Total              float64       `json:"total"`
	Tip                float64       `json:"tip,omitempty"`
	CustomerName       string        `json:"customerName,omitempty"`
	Date               string        `json:"date"`
	Location           interface{}   `json:"location"` // Can be a string or an object with a name field
	PaymentType        string        `json:"paymentType"`
	RefundAmount       float64       `json:"refundAmount,omitempty"`
	DiscountAmount     float64       `json:"discountAmount,omitempty"`
	DiscountPercentage float64       `json:"discountPercentage,omitempty"`
	CashGiven          float64       `json:"cashGiven,omitempty"`
	ChangeDue          float64       `json:"changeDue,omitempty"`
	Copies             int           `json:"copies"`
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
		w.Header().Set("Access-Control-Allow-Methods", "GET, POST, OPTIONS")
		w.Header().Set("Access-Control-Allow-Headers", "Content-Type, Authorization")
		if r.Method == http.MethodOptions {
			w.WriteHeader(http.StatusOK)
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

// generateESCPOSCommands creates raw ESC/POS commands for thermal printers
func generateESCPOSCommands(receipt ReceiptData) ([]byte, error) {
	var cmd bytes.Buffer
	
	// Initialize printer
	cmd.Write([]byte{0x1B, 0x40}) // ESC @
	
	// Center align
	cmd.Write([]byte{0x1B, 0x61, 0x01}) // ESC a 1
	
	// Get location name
	var locationName string
	switch loc := receipt.Location.(type) {
	case string:
		locationName = loc
	case map[string]interface{}:
		if name, ok := loc["name"].(string); ok {
			locationName = name
		}
	}
	
	// Print header
	cmd.WriteString(locationName + "\n")
	cmd.Write([]byte{0x1B, 0x45, 0x00}) // ESC E 0 (cancel bold)
	
	if receipt.CustomerName != "" {
		cmd.WriteString("Customer: " + receipt.CustomerName + "\n")
	}
	
	cmd.WriteString(receipt.Date + "\n\n")
	
	// Left align
	cmd.Write([]byte{0x1B, 0x61, 0x00}) // ESC a 0
	
	// Print transaction info
	cmd.WriteString("Transaction ID: " + receipt.TransactionID + "\n")
	cmd.WriteString("Payment: " + strings.Title(receipt.PaymentType) + "\n\n")
	
	// Print items
	cmd.WriteString("ITEMS\n")
	cmd.Write([]byte{0x1B, 0x2D, 0x01}) // ESC - 1 (underline)
	cmd.WriteString("                              \n") // Underline space
	cmd.Write([]byte{0x1B, 0x2D, 0x00}) // ESC - 0 (cancel underline)
	
	for _, item := range receipt.Items {
		cmd.WriteString(item.Name + "\n")
		quantityPrice := fmt.Sprintf("%d x $%.2f", item.Quantity, item.Price)
		// Fix: Convert int to float64 before multiplication
		itemTotal := fmt.Sprintf("$%.2f", float64(item.Quantity)*item.Price)
		cmd.WriteString(fmt.Sprintf("  %-20s %10s\n", quantityPrice, itemTotal))
		
		if item.SKU != "" {
			cmd.WriteString("  SKU: " + item.SKU + "\n")
		}
		cmd.WriteString("\n")
	}
	
	// Print divider
	cmd.Write([]byte{0x1B, 0x2D, 0x01}) // ESC - 1 (underline)
	cmd.WriteString("                              \n") // Underline space
	cmd.Write([]byte{0x1B, 0x2D, 0x00}) // ESC - 0 (cancel underline)
	
	// Print totals
	cmd.WriteString(fmt.Sprintf("%-20s $%.2f\n", "Subtotal:", receipt.Subtotal))
	
	if receipt.DiscountPercentage > 0 && receipt.DiscountAmount > 0 {
		cmd.WriteString(fmt.Sprintf("%-20s -$%.2f\n", fmt.Sprintf("Discount (%.0f%%):", receipt.DiscountPercentage), receipt.DiscountAmount))
	}
	
	cmd.WriteString(fmt.Sprintf("%-20s $%.2f\n", "Tax:", receipt.Tax))
	
	// Calculate GST and PST
	gst := receipt.Subtotal * 0.05
	pst := receipt.Subtotal * 0.07
	cmd.WriteString(fmt.Sprintf("  GST (5%%): $%.2f\n", gst))
	cmd.WriteString(fmt.Sprintf("  PST (7%%): $%.2f\n", pst))
	
	if receipt.RefundAmount > 0 {
		cmd.WriteString(fmt.Sprintf("%-20s -$%.2f\n", "Refund:", receipt.RefundAmount))
	}
	
	if receipt.Tip > 0 {
		cmd.WriteString(fmt.Sprintf("%-20s $%.2f\n", "Tip:", receipt.Tip))
	}
	
	// Print total in bold
	cmd.Write([]byte{0x1B, 0x45, 0x01}) // ESC E 1 (bold)
	cmd.WriteString(fmt.Sprintf("\n%-20s $%.2f\n", "TOTAL:", receipt.Total))
	cmd.Write([]byte{0x1B, 0x45, 0x00}) // ESC E 0 (cancel bold)
	
	// Print cash details if applicable
	if receipt.PaymentType == "cash" && receipt.CashGiven > 0 {
		cmd.WriteString(fmt.Sprintf("%-20s $%.2f\n", "Cash:", receipt.CashGiven))
		cmd.WriteString(fmt.Sprintf("%-20s $%.2f\n", "Change:", receipt.ChangeDue))
	}
	
	// Print divider
	cmd.Write([]byte{0x1B, 0x2D, 0x01}) // ESC - 1 (underline)
	cmd.WriteString("                              \n") // Underline space
	cmd.Write([]byte{0x1B, 0x2D, 0x00}) // ESC - 0 (cancel underline)
	
	// Center align for footer
	cmd.Write([]byte{0x1B, 0x61, 0x01}) // ESC a 1
	
	// Print footer
	cmd.WriteString("\nThank you for your purchase!\n")
	cmd.WriteString("Visit us again at " + locationName + "\n\n\n")
	
	// Cut paper
	cmd.Write([]byte{0x1D, 0x56, 0x41, 0x10}) // GS V A 16 (partial cut with feed)
	
	return cmd.Bytes(), nil
}

// printReceipt handles the receipt printing functionality
func printReceiptHandler(w http.ResponseWriter, r *http.Request) {
	// Only allow POST method
	if r.Method != http.MethodPost {
		writeJSONError(w, http.StatusMethodNotAllowed, errors.New("only POST method is allowed"))
		return
	}
	
	// Read the request body
	body, err := ioutil.ReadAll(r.Body)
	if err != nil {
		writeJSONError(w, http.StatusBadRequest, errors.New("error reading request body"))
		return
	}
	defer r.Body.Close()
	
	// Parse the JSON data
	var receipt ReceiptData
	if err := json.Unmarshal(body, &receipt); err != nil {
		writeJSONError(w, http.StatusBadRequest, fmt.Errorf("error parsing JSON data: %v", err))
		return
	}
	
	// Validate receipt
	if receipt.TransactionID == "" {
		writeJSONError(w, http.StatusBadRequest, errors.New("transaction ID is required"))
		return
	}
	
	// Set default copies if not specified
	if receipt.Copies <= 0 {
		receipt.Copies = 1
	}
	
	// Generate ESC/POS commands
	escposData, err := generateESCPOSCommands(receipt)
	if err != nil {
		writeJSONError(w, http.StatusInternalServerError, errors.New("error generating ESC/POS commands"))
		return
	}
	
	// Create a temporary file for the ESC/POS data
	tempFile, err := ioutil.TempFile("", "receipt-*.bin")
	if err != nil {
		writeJSONError(w, http.StatusInternalServerError, errors.New("error creating temporary file"))
		return
	}
	defer os.Remove(tempFile.Name())
	
	// Write the ESC/POS data to the temporary file
	if _, err := tempFile.Write(escposData); err != nil {
		writeJSONError(w, http.StatusInternalServerError, errors.New("error writing to temporary file"))
		return
	}
	
	// Close the temporary file
	if err := tempFile.Close(); err != nil {
		writeJSONError(w, http.StatusInternalServerError, errors.New("error closing temporary file"))
		return
	}
	
	// Define the printer command based on the OS
	var cmd *exec.Cmd
	
	successCount := 0
	for i := 0; i < receipt.Copies; i++ {
		if runtime.GOOS == "windows" {
			// Windows: use PowerShell to send to default printer
			cmd = exec.Command("powershell", "-Command", fmt.Sprintf("Get-Content -Path '%s' -Raw | Out-Printer", tempFile.Name()))
			fmt.Printf("Printing copy %d/%d using default Windows printer\n", i+1, receipt.Copies)
		} else if runtime.GOOS == "darwin" {
			// macOS: use lp command with default printer
			cmd = exec.Command("lp", tempFile.Name())
			fmt.Printf("Printing copy %d/%d using default macOS printer\n", i+1, receipt.Copies)
		} else {
			// Linux: use lp command with default printer
			cmd = exec.Command("lp", tempFile.Name())
			fmt.Printf("Printing copy %d/%d using default Linux printer\n", i+1, receipt.Copies)
		}
		
		// Execute the command
		output, err := cmd.CombinedOutput()
		if err != nil {
			log.Printf("Print error (copy %d/%d): %v - %s", i+1, receipt.Copies, err, string(output))
			continue
		}
		successCount++
		fmt.Printf("Successfully printed copy %d/%d\n", i+1, receipt.Copies)
	}
	
	// Return response
	if successCount > 0 {
		resp := map[string]interface{}{
			"status":  "success",
			"message": fmt.Sprintf("Printed %d/%d copies successfully", successCount, receipt.Copies),
		}
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(resp)
	} else {
		writeJSONError(w, http.StatusInternalServerError, errors.New("failed to print any copies"))
	}
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
	
	// Scanner endpoint
	mux.HandleFunc("/scanner/scan", func(w http.ResponseWriter, r *http.Request) {
		scannerHandler(w, r, *portFlag, *scannerPortFlag, *useSimpleCommandFlag, *useMacSettingsFlag, readTimeout)
	})
	
	// Receipt printing endpoint
	mux.HandleFunc("/print/receipt", printReceiptHandler)
	
	log.Printf("Starting server on http://localhost:%d", *httpPortFlag)
	log.Printf("Scanner endpoint: http://localhost:%d/scanner/scan", *httpPortFlag)
	log.Printf("Receipt printer endpoint: http://localhost:%d/print/receipt", *httpPortFlag)
	
	if err := http.ListenAndServe(fmt.Sprintf(":%d", *httpPortFlag), corsMiddleware(mux)); err != nil {
		log.Fatal(err)
	}
}
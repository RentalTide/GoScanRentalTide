package main

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"io/ioutil"
	"log"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"runtime"
	"strings"
	"time"

	"go.bug.st/serial"
)

// LicenseData type for NA driver's license data.
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

// ReceiptData represents the data structure for a receipt
type ReceiptData struct {
	TransactionID      string                   `json:"transactionId"`
	Items              []map[string]interface{} `json:"items"`
	Subtotal           float64                  `json:"subtotal"`
	Tax                float64                  `json:"tax"`
	Total              float64                  `json:"total"`
	Tip                *float64                 `json:"tip,omitempty"`
	CustomerName       *string                  `json:"customerName,omitempty"`
	Date               string                   `json:"date"`
	Location           interface{}              `json:"location"`
	PaymentType        string                   `json:"paymentType"`
	RefundAmount       *float64                 `json:"refundAmount,omitempty"`
	DiscountAmount     *float64                 `json:"discountAmount,omitempty"`
	DiscountPercentage *float64                 `json:"discountPercentage,omitempty"`
	CashGiven          *float64                 `json:"cashGiven,omitempty"`
	ChangeDue          *float64                 `json:"changeDue,omitempty"`
}

// parse and struct
func parseLicenseData(raw string) LicenseData {
	// Split into lines and remove empty ones.
	lines := strings.Split(raw, "\n")
	var parsedLines []string
	for _, line := range lines {
		trimmed := strings.TrimSpace(line)
		if len(trimmed) > 0 {
			parsedLines = append(parsedLines, trimmed)
		}
	}

	// Pirate map
	data := make(map[string]string)
	var licenseClass string

	for _, line := range parsedLines {
		switch {
		case strings.HasPrefix(line, "DCS"):
			// Last Name
			data["lastName"] = strings.TrimSpace(line[3:])
		case strings.HasPrefix(line, "DAC"):
			// First Name
			data["firstName"] = strings.TrimSpace(line[3:])
		case strings.HasPrefix(line, "DAD"):
			// Middle Name
			data["middleName"] = strings.TrimSpace(line[3:])
		case strings.HasPrefix(line, "DBA"):
			// Expiry Date in YYYYMMDD format -> YYYY/MM/DD
			d := strings.TrimSpace(line[3:])
			if len(d) >= 8 {
				data["expiryDate"] = fmt.Sprintf("%s/%s/%s", d[0:4], d[4:6], d[6:8])
			}
		case strings.HasPrefix(line, "DBD"):
			// Issue Date
			d := strings.TrimSpace(line[3:])
			if len(d) >= 8 {
				data["issueDate"] = fmt.Sprintf("%s/%s/%s", d[0:4], d[4:6], d[6:8])
			}
		case strings.HasPrefix(line, "DBB"):
			// Date of Birth
			d := strings.TrimSpace(line[3:])
			if len(d) >= 8 {
				data["dob"] = fmt.Sprintf("%s/%s/%s", d[0:4], d[4:6], d[6:8])
			}
		case strings.HasPrefix(line, "DBC"):
			// Sex: assume '1' is Male, '2' is Female
			s := strings.TrimSpace(line[3:])
			if s == "1" {
				data["sex"] = "M"
			} else if s == "2" {
				data["sex"] = "F"
			} else {
				data["sex"] = s
			}
		case strings.HasPrefix(line, "DAU"):
			// Height: remove extra spaces (e.g., "178 cm" -> "178cm")
			data["height"] = strings.ReplaceAll(strings.TrimSpace(line[3:]), " ", "")
		case strings.HasPrefix(line, "DAG"):
			// Street Address
			data["address"] = strings.TrimSpace(line[3:])
		case strings.HasPrefix(line, "DAI"):
			// City
			data["city"] = strings.TrimSpace(line[3:])
		case strings.HasPrefix(line, "DAJ"):
			// State/Province
			data["state"] = strings.TrimSpace(line[3:])
		case strings.HasPrefix(line, "DAK"):
			// Postal Code
			data["postal"] = strings.TrimSpace(line[3:])
		case strings.HasPrefix(line, "DAQ"):
			// License Number: if 15 characters, insert hyphens after 5 and 10 characters
			ln := strings.TrimSpace(line[3:])
			if len(ln) == 15 {
				ln = fmt.Sprintf("%s-%s-%s", ln[0:5], ln[5:10], ln[10:15])
			}
			data["licenseNumber"] = ln
		}

		// If the line contains "DCAG", use it to determine license class.
		if strings.Contains(line, "DCAG") {
			re := regexp.MustCompile(`DCAG(\w+)`)
			matches := re.FindStringSubmatch(line)
			if len(matches) > 1 {
				licenseClass = matches[1]
			}
		}
	}

	// Default license class to "NA" if not found.
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

// findScannerPort finds ports
func findScannerPort() (string, error) {
	ports, err := serial.GetPortsList()
	if err != nil {
		return "", fmt.Errorf("failed to list serial ports: %w", err)
	}
	if len(ports) == 0 {
		return "", errors.New("no serial ports found")
	}

	// On Windows, choose a port starting with "COM".
	// On macOS, look for "usbserial" in the name.
	for _, port := range ports {
		if runtime.GOOS == "windows" {
			if strings.HasPrefix(strings.ToLower(port), "com") {
				return port, nil
			}
		} else if runtime.GOOS == "darwin" {
			if strings.Contains(strings.ToLower(port), "usbserial") {
				return port, nil
			}
		} else {
			// For Linux, adjust criteria as needed.
			if strings.Contains(strings.ToLower(port), "ttyusb") || strings.Contains(strings.ToLower(port), "usb") {
				return port, nil
			}
		}
	}
	return "", errors.New("no compatible serial port found")
}

// readWithTimeout wraps the port.Read call in a goroutine with a timeout.
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

// sendScannerCommand opens the serial port, sends the command, and returns the scanner's raw response.
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

	// build SOH (0x01) + command string + EOT (0x04)
	cmdBuffer := []byte{0x01}
	cmdBuffer = append(cmdBuffer, []byte(commandStr)...)
	cmdBuffer = append(cmdBuffer, 0x04)

	n, err := port.Write(cmdBuffer)
	if err != nil {
		return "", fmt.Errorf("failed to write to port: %w", err)
	}
	if n != len(cmdBuffer) {
		return "", errors.New("incomplete write to port")
	}

	var responseBuffer bytes.Buffer
	readTimeout := 3 * time.Second  // timeout after 3 seconds
	maxDuration := 10 * time.Second // maximum overall timeout of 10
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
		responseBuffer.Write(tmp[:n])
		if time.Now().After(deadline) {
			break
		}
	}

	return responseBuffer.String(), nil
}

// writes JSON errors
func writeJSONError(w http.ResponseWriter, status int, err error) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	json.NewEncoder(w).Encode(map[string]interface{}{
		"status":  "error",
		"message": err.Error(),
	})
}

// support cors
func corsMiddleware(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// For development, allow all origins. Adjust as needed.
		w.Header().Set("Access-Control-Allow-Origin", "*")
		w.Header().Set("Access-Control-Allow-Methods", "GET, POST, OPTIONS")
		w.Header().Set("Access-Control-Allow-Headers", "Content-Type, Authorization")
		if r.Method == http.MethodOptions {
			return
		}
		next.ServeHTTP(w, r)
	})
}

// handle http request
func scannerHandler(w http.ResponseWriter, r *http.Request) {
	command := "<TXPING>"
	result, err := sendScannerCommand(command)
	if err != nil {
		writeJSONError(w, http.StatusInternalServerError, err)
		return
	}

	// If the response is only a NAK (0x15), return a JSON 404 error.
	if strings.TrimSpace(result) == string(byte(0x15)) {
		writeJSONError(w, http.StatusNotFound, errors.New("No license scanned or scanner not triggered"))
		return
	}

	licenseData := parseLicenseData(result)
	response := map[string]interface{}{
		"status":      "success",
		"licenseData": licenseData,
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(response)
}

// New function to generate ESC/POS commands for receipt
func generateESCPOSReceipt(receipt ReceiptData) []byte {
	var buf bytes.Buffer
	
	// ESC/POS initialization
	buf.Write([]byte{0x1B, 0x40}) // Initialize printer
	buf.Write([]byte{0x1B, 0x21, 0x01}) // Set font to small
	
	// Get location name
	locationName := ""
	switch loc := receipt.Location.(type) {
	case string:
		locationName = loc
	case map[string]interface{}:
		if name, ok := loc["name"].(string); ok {
			locationName = name
		}
	}
	
	// Write header - center aligned
	buf.Write([]byte{0x1B, 0x61, 0x01}) // Center alignment
	buf.Write([]byte(locationName + "\n"))
	buf.Write([]byte(receipt.Date + "\n"))
	if receipt.CustomerName != nil {
		buf.Write([]byte("Customer: " + *receipt.CustomerName + "\n"))
	}
	
	// Write divider
	buf.Write([]byte{0x1B, 0x61, 0x00}) // Left alignment
	buf.Write([]byte("--------------------------------\n"))
	
	// Transaction info
	buf.Write([]byte("Transaction ID: " + receipt.TransactionID + "\n"))
	buf.Write([]byte("Payment: " + strings.Title(receipt.PaymentType) + "\n"))
	
	// Divider
	buf.Write([]byte("--------------------------------\n"))
	
	// Items header
	buf.Write([]byte{0x1B, 0x21, 0x08}) // Bold
	buf.Write([]byte("ITEMS\n"))
	buf.Write([]byte{0x1B, 0x21, 0x01}) // Regular text
	
	// Items
	for _, item := range receipt.Items {
		name, _ := item["name"].(string)
		quantity, _ := item["quantity"].(float64)
		price, _ := item["price"].(float64)
		total := quantity * price
		sku, _ := item["sku"].(string)
		
		buf.Write([]byte{0x1B, 0x21, 0x08}) // Bold
		buf.Write([]byte(name + "\n"))
		buf.Write([]byte{0x1B, 0x21, 0x01}) // Regular
		
		// Quantity and price
		qtyLine := fmt.Sprintf("  %.0f x $%.2f", quantity, price)
		// Pad spaces to align total to the right
		spaces := 32 - len(qtyLine) - len(fmt.Sprintf("$%.2f", total))
		for i := 0; i < spaces; i++ {
			qtyLine += " "
		}
		qtyLine += fmt.Sprintf("$%.2f\n", total)
		buf.Write([]byte(qtyLine))
		
		if sku != "" {
			buf.Write([]byte("  SKU: " + sku + "\n"))
		}
	}
	
	// Divider
	buf.Write([]byte("--------------------------------\n"))
	
	// Totals
	writeTotal := func(label string, value float64, bold bool) {
		line := label
		spaces := 32 - len(label) - len(fmt.Sprintf("$%.2f", value))
		for i := 0; i < spaces; i++ {
			line += " "
		}
		line += fmt.Sprintf("$%.2f\n", value)
		
		if bold {
			buf.Write([]byte{0x1B, 0x21, 0x08}) // Bold
		}
		buf.Write([]byte(line))
		if bold {
			buf.Write([]byte{0x1B, 0x21, 0x01}) // Regular
		}
	}
	
	writeTotal("Subtotal:", receipt.Subtotal, false)
	
	// Add discount if applicable
	if receipt.DiscountAmount != nil && receipt.DiscountPercentage != nil {
		discountLabel := fmt.Sprintf("Discount (%.0f%%):", *receipt.DiscountPercentage)
		writeTotal(discountLabel, -*receipt.DiscountAmount, false)
	}
	
	// Add tax
	writeTotal("Tax:", receipt.Tax, false)
	buf.Write([]byte(fmt.Sprintf("  GST (5%%): $%.2f\n", receipt.Subtotal*0.05)))
	buf.Write([]byte(fmt.Sprintf("  PST (7%%): $%.2f\n", receipt.Subtotal*0.07)))
	
	// Add refund if applicable
	if receipt.RefundAmount != nil && *receipt.RefundAmount > 0 {
		writeTotal("Refund:", -*receipt.RefundAmount, false)
	}
	
	// Add tip if applicable
	if receipt.Tip != nil && *receipt.Tip > 0 {
		writeTotal("Tip:", *receipt.Tip, false)
	}
	
	// Total
	buf.Write([]byte{0x1B, 0x45, 0x01}) // Emphasize on
	writeTotal("TOTAL:", receipt.Total, true)
	buf.Write([]byte{0x1B, 0x45, 0x00}) // Emphasize off
	
	// Cash payment details
	if receipt.PaymentType == "cash" && receipt.CashGiven != nil && receipt.ChangeDue != nil {
		buf.Write([]byte("\n"))
		writeTotal("Cash:", *receipt.CashGiven, false)
		writeTotal("Change:", *receipt.ChangeDue, false)
	}
	
	// Divider
	buf.Write([]byte("--------------------------------\n"))
	
	// Footer
	buf.Write([]byte{0x1B, 0x61, 0x01}) // Center alignment
	buf.Write([]byte{0x1B, 0x21, 0x08}) // Bold
	buf.Write([]byte("Thank you for your purchase!\n"))
	buf.Write([]byte{0x1B, 0x21, 0x01}) // Regular
	buf.Write([]byte("Visit us again at " + locationName + "\n\n\n\n"))
	
	// Cut paper
	buf.Write([]byte{0x1D, 0x56, 0x42, 0x00}) // Full cut
	
	return buf.Bytes()
}

// Modified printReceipt function to send ESC/POS directly to printer
func printReceipt(receipt ReceiptData) error {
	log.Printf("=== PRINT RECEIPT FUNCTION STARTED ===")
	
	// Generate ESC/POS commands
	escposCommands := generateESCPOSReceipt(receipt)
	log.Printf("Generated ESC/POS commands (%d bytes)", len(escposCommands))
	
	if runtime.GOOS == "windows" {
		// On Windows, we'll need to write to a file and use Windows' print spooler
		// Create temp file
		tmpFile, err := ioutil.TempFile(os.TempDir(), "receipt-*.bin")
		if err != nil {
			log.Printf("ERROR: Failed to create temp file: %v", err)
			return fmt.Errorf("error creating temp file: %w", err)
		}
		tmpFilePath := tmpFile.Name()
		log.Printf("Created temporary file at system temp location: %s", tmpFilePath)
		
		// Write the ESC/POS commands to the temp file
		if _, err := tmpFile.Write(escposCommands); err != nil {
			log.Printf("ERROR: Failed to write to temp file: %v", err)
			tmpFile.Close()
			os.Remove(tmpFilePath)
			return fmt.Errorf("error writing to temp file: %w", err)
		}
		
		if err := tmpFile.Close(); err != nil {
			log.Printf("ERROR: Failed to close temp file: %v", err)
			os.Remove(tmpFilePath)
			return fmt.Errorf("error closing temp file: %w", err)
		}
		
		// Get printer name - can be adjusted based on your receipt printer name
		printerCmd := exec.Command("powershell", "-Command", "Get-WmiObject -Query \"SELECT * FROM Win32_Printer WHERE Default=$true\" | Select-Object -ExpandProperty Name")
		var printerBuf bytes.Buffer
		printerCmd.Stdout = &printerBuf
		
		if err := printerCmd.Run(); err != nil {
			log.Printf("WARNING: Could not get default printer name: %v", err)
			// Continue with empty printer name, Windows will use default
		}
		
		printerName := strings.TrimSpace(printerBuf.String())
		log.Printf("Using printer: %s", printerName)
		
		// Try different methods to print to the receipt printer
		
		// Method 1: Try direct printing with the printer name
		log.Printf("Trying Method 1: Direct print to printer")
		printCommand := exec.Command("cmd", "/c", fmt.Sprintf("type \"%s\" > \"%s\"", tmpFilePath, printerName))
		var outputBuf1, errorBuf1 bytes.Buffer
		printCommand.Stdout = &outputBuf1
		printCommand.Stderr = &errorBuf1
		
		if err := printCommand.Run(); err != nil {
			log.Printf("Method 1 ERROR: Failed to print directly: %v", err)
			log.Printf("Method 1 STDERR: %s", errorBuf1.String())
			
			// Method 2: Try using the copy command to LPT1
			log.Printf("Trying Method 2: Copy to LPT1 port...")
			copyCmd := exec.Command("cmd", "/c", fmt.Sprintf("copy /b \"%s\" LPT1", tmpFilePath))
			var outputBuf2, errorBuf2 bytes.Buffer
			copyCmd.Stdout = &outputBuf2
			copyCmd.Stderr = &errorBuf2
			
			if err := copyCmd.Run(); err != nil {
				log.Printf("Method 2 ERROR: Copy to LPT1 failed: %v", err)
				log.Printf("Method 2 STDERR: %s", errorBuf2.String())
				
				// Method 3: Try using printui.dll approach
				log.Printf("Trying Method 3: PrintUI.dll approach...")
				printUICmd := exec.Command("cmd", "/c", 
					fmt.Sprintf("rundll32 printui.dll,PrintUIEntry /k /n\"%s\" /f\"%s\" /j\"Raw Print\"", 
					printerName, tmpFilePath))
				var outputBuf3, errorBuf3 bytes.Buffer
				printUICmd.Stdout = &outputBuf3
				printUICmd.Stderr = &errorBuf3
				
				if err := printUICmd.Run(); err != nil {
					log.Printf("Method 3 ERROR: PrintUI approach failed: %v", err)
					log.Printf("Method 3 STDERR: %s", errorBuf3.String())
					
					// Last resort - try opening browser
					log.Printf("All direct print methods failed. Attempting fallback...")
					return fmt.Errorf("all print methods failed: %w", err)
				}
			}
		}
		
		// Clean up
		os.Remove(tmpFilePath)
		
	} else if runtime.GOOS == "darwin" || runtime.GOOS == "linux" {
		// For Unix-like systems, try to find the printer device
		var printerDevice string
		
		if runtime.GOOS == "darwin" {
			// On macOS, USB printers are often mapped to /dev/usb*
			files, _ := filepath.Glob("/dev/usb*")
			if len(files) > 0 {
				printerDevice = files[0]
			}
		} else {
			// On Linux, USB printers are often mapped to /dev/usb/lp*
			files, _ := filepath.Glob("/dev/usb/lp*")
			if len(files) == 0 {
				// Try alternative paths
				files, _ = filepath.Glob("/dev/lp*")
			}
			if len(files) > 0 {
				printerDevice = files[0]
			}
		}
		
		if printerDevice != "" {
			log.Printf("Found printer device: %s", printerDevice)
			
			// Open the printer device
			f, err := os.OpenFile(printerDevice, os.O_WRONLY, 0)
			if err != nil {
				log.Printf("ERROR: Failed to open printer device: %v", err)
				return fmt.Errorf("error opening printer device: %w", err)
			}
			defer f.Close()
			
			// Write ESC/POS commands directly to the printer
			_, err = f.Write(escposCommands)
			if err != nil {
				log.Printf("ERROR: Failed to write to printer device: %v", err)
				return fmt.Errorf("error writing to printer device: %w", err)
			}
			
			log.Printf("Successfully wrote commands to printer device")
		} else {
			// Fallback to using 'lp' command with raw mode
			log.Printf("No printer device found, using lp command")
			
			// Create temp file
			tmpFile, err := ioutil.TempFile("", "receipt-*.bin")
			if err != nil {
				log.Printf("ERROR: Failed to create temp file: %v", err)
				return fmt.Errorf("error creating temp file: %w", err)
			}
			tmpFilePath := tmpFile.Name()
			
			// Write the ESC/POS commands
			if _, err := tmpFile.Write(escposCommands); err != nil {
				log.Printf("ERROR: Failed to write to temp file: %v", err)
				tmpFile.Close()
				os.Remove(tmpFilePath)
				return fmt.Errorf("error writing to temp file: %w", err)
			}
			
			if err := tmpFile.Close(); err != nil {
				log.Printf("ERROR: Failed to close temp file: %v", err)
				os.Remove(tmpFilePath)
				return fmt.Errorf("error closing temp file: %w", err)
			}
			
			// Send to printer using lp in raw mode
			lpCmd := exec.Command("lp", "-o", "raw", tmpFilePath)
			var outputBuf, errorBuf bytes.Buffer
			lpCmd.Stdout = &outputBuf
			lpCmd.Stderr = &errorBuf
			
			if err := lpCmd.Run(); err != nil {
				log.Printf("ERROR: Print command failed: %v", err)
				log.Printf("STDERR: %s", errorBuf.String())
				os.Remove(tmpFilePath)
				return fmt.Errorf("error printing receipt: %w, stderr: %s", err, errorBuf.String())
			}
			
			// Clean up
			os.Remove(tmpFilePath)
		}
	}
	
	log.Printf("=== PRINT RECEIPT FUNCTION COMPLETED SUCCESSFULLY ===")
	return nil
}

// Update handlePrintReceipt to call this directly
func handlePrintReceipt(w http.ResponseWriter, r *http.Request) {
	// Existing code to parse receipt
	var receipt ReceiptData
	if err := json.NewDecoder(r.Body).Decode(&receipt); err != nil {
		writeJSONError(w, http.StatusBadRequest, fmt.Errorf("invalid request body: %w", err))
		return
	}
	
	// Print the receipt
	if err := printReceipt(receipt); err != nil {
		writeJSONError(w, http.StatusInternalServerError, err)
		return
	}
	
	// Return success response
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(map[string]interface{}{
		"status":  "success",
		"message": "Receipt printed successfully",
	})
}

func main() {
	// Set up logging
	log.SetOutput(os.Stdout)
	log.SetFlags(log.Ldate | log.Ltime | log.Lmicroseconds | log.Lshortfile)
	log.Printf("Starting Receipt and Scanner Application")
	
	mux := http.NewServeMux()
	mux.HandleFunc("/scanner/scan", scannerHandler)
	mux.HandleFunc("/print/receipt", handlePrintReceipt)

	handler := corsMiddleware(mux)
	port := 3500 // change port will break front end so don't
	log.Printf("Starting server on http://localhost:%d", port)
	if err := http.ListenAndServe(fmt.Sprintf(":%d", port), handler); err != nil {
		log.Fatal(err)
	}
}
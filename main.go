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


func generatePrintableReceiptHTML(receipt ReceiptData) string {
    // Extract location name
    locationName := ""
    switch loc := receipt.Location.(type) {
    case string:
        locationName = loc
    case map[string]interface{}:
        if name, ok := loc["name"].(string); ok {
            locationName = name
        }
    }
    
    // Build a self-printing HTML file
    html := `<!DOCTYPE html>
<html>
<head>
  <meta charset="utf-8">
  <title>Receipt</title>
  <style>
    @page {
      size: 80mm auto;
      margin: 0;
    }
    body {
      font-family: Arial, sans-serif;
      width: 80mm;
      margin: 0;
      padding: 10px;
      font-size: 10pt;
    }
    .header {
      text-align: center;
      margin-bottom: 10px;
    }
    .business-name {
      font-weight: bold;
      font-size: 12pt;
    }
    .divider {
      border-bottom: 1px dashed #000;
      margin: 10px 0;
    }
    .items-section {
      margin-bottom: 10px;
    }
    .item {
      margin-bottom: 5px;
    }
    .totals-section {
      margin-bottom: 10px;
    }
    .footer {
      text-align: center;
      margin-top: 10px;
      font-size: 9pt;
    }
  </style>
  <script>
    // Auto-print and close script
    window.onload = function() {
      // Wait a second to ensure the page is fully loaded
      setTimeout(function() {
        window.print();
        // Wait a bit to allow print dialog to appear and be processed
        setTimeout(function() {
          window.close();
        }, 2000);
      }, 500);
    };
  </script>
</head>
<body>
  <div class="header">
    <div class="business-name">` + locationName + `</div>
    <div>` + receipt.Date + `</div>`

    if receipt.CustomerName != nil {
        html += `    <div>Customer: ` + *receipt.CustomerName + `</div>`
    }

    html += `  </div>
  
  <div class="divider"></div>
  
  <div>Transaction ID: ` + receipt.TransactionID + `</div>
  <div>Payment: ` + strings.Title(receipt.PaymentType) + `</div>
  
  <div class="divider"></div>
  
  <div class="items-section">
    <div style="font-weight: bold;">ITEMS</div>`

    // Add items
    for _, item := range receipt.Items {
        name, _ := item["name"].(string)
        quantity, _ := item["quantity"].(float64)
        price, _ := item["price"].(float64)
        
        html += fmt.Sprintf(`
    <div class="item">
      <div>%s</div>
      <div style="display: flex; justify-content: space-between;">
        <span>%.0f x $%.2f</span>
        <span>$%.2f</span>
      </div>
    </div>`, name, quantity, price, quantity*price)
    }

    html += `
  </div>
  
  <div class="divider"></div>
  
  <div class="totals-section">
    <div style="display: flex; justify-content: space-between;">
      <span>Subtotal:</span>
      <span>$` + fmt.Sprintf("%.2f", receipt.Subtotal) + `</span>
    </div>
    <div style="display: flex; justify-content: space-between;">
      <span>Tax:</span>
      <span>$` + fmt.Sprintf("%.2f", receipt.Tax) + `</span>
    </div>
    <div style="font-weight: bold; display: flex; justify-content: space-between; margin-top: 5px;">
      <span>TOTAL:</span>
      <span>$` + fmt.Sprintf("%.2f", receipt.Total) + `</span>
    </div>
  </div>
  
  <div class="divider"></div>
  
  <div class="footer">
    <div>Thank you for your purchase!</div>
  </div>
</body>
</html>`

    return html
}

func printReceipt(html string) error {
    log.Printf("=== PRINT RECEIPT FUNCTION STARTED ===")
    
    // Get current directory
    currentDir, err := os.Getwd()
    if err != nil {
        log.Printf("ERROR: Failed to get current directory: %v", err)
        return fmt.Errorf("error getting current directory: %w", err)
    }
    
    // Create a file in the current directory
    receiptFileName := fmt.Sprintf("receipt-%d.html", time.Now().UnixNano())
    receiptFilePath := filepath.Join(currentDir, receiptFileName)
    
    log.Printf("Creating receipt file at: %s", receiptFilePath)
    
    // Write HTML to the file
    err = ioutil.WriteFile(receiptFilePath, []byte(html), 0644)
    if err != nil {
        log.Printf("ERROR: Failed to write receipt file: %v", err)
        return fmt.Errorf("error writing receipt file: %w", err)
    }
    
    // Open the HTML file in the default browser
    log.Printf("Opening receipt file in browser")
    cmd := exec.Command("cmd", "/c", "start", receiptFilePath)
    err = cmd.Run()
    if err != nil {
        log.Printf("ERROR: Failed to open receipt in browser: %v", err)
        return fmt.Errorf("error opening receipt in browser: %w", err)
    }
    
    log.Printf("Receipt opened in browser successfully")
    
    // Return success immediately, start cleanup in background
    go func() {
        // Wait much longer before cleanup to ensure printing completes
        log.Printf("Waiting for print job to complete...")
        time.Sleep(60 * time.Second)
        
        // Clean up the receipt file
        log.Printf("Cleaning up receipt file: %s", receiptFilePath)
        err := os.Remove(receiptFilePath)
        if err != nil {
            log.Printf("WARNING: Failed to remove receipt file: %v", err)
        } else {
            log.Printf("Successfully removed receipt file")
        }
        
        log.Printf("=== PRINT RECEIPT FUNCTION CLEANUP COMPLETED ===")
    }()
    
    return nil
}

func handlePrintReceipt(w http.ResponseWriter, r *http.Request) {
    log.Printf("Received print receipt request from %s", r.RemoteAddr)
    
    // Only accept POST requests
    if r.Method != http.MethodPost {
        log.Printf("ERROR: Received non-POST request method: %s", r.Method)
        writeJSONError(w, http.StatusMethodNotAllowed, errors.New("only POST method is allowed"))
        return
    }

    // Decode the request body
    var receipt ReceiptData
    if err := json.NewDecoder(r.Body).Decode(&receipt); err != nil {
        log.Printf("ERROR: Failed to decode request body: %v", err)
        writeJSONError(w, http.StatusBadRequest, fmt.Errorf("invalid request body: %w", err))
        return
    }
    
    log.Printf("Processing receipt - Transaction ID: %s, Items: %d", 
        receipt.TransactionID, len(receipt.Items))

    // Generate HTML for the receipt
    html := generatePrintableReceiptHTML(receipt)
    
    // Return success response immediately
    log.Printf("Returning success response to client")
    w.Header().Set("Content-Type", "application/json")
    json.NewEncoder(w).Encode(map[string]interface{}{
        "status":  "success",
        "message": "Receipt print job submitted successfully",
    })
    
    // Start printing process after returning response
    go func() {
        if err := printReceipt(html); err != nil {
            log.Printf("ERROR: Failed to print receipt: %v", err)
        }
    }()
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
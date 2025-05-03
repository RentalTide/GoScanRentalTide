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

// generateReceiptHTML creates HTML content for the receipt
func generateReceiptHTML(receipt ReceiptData) string {
	locationName := ""
	switch loc := receipt.Location.(type) {
	case string:
		locationName = loc
	case map[string]interface{}:
		if name, ok := loc["name"].(string); ok {
			locationName = name
		}
	}

	// Start building the HTML content
	html := `<!DOCTYPE html>
<html>
<head>
  <meta charset="utf-8">
  <title>Receipt</title>
  <style>
    @page {
      margin: 0.5cm;
      size: 80mm auto;  /* Receipt paper size */
    }
    body {
      font-family: 'Arial', sans-serif;
      margin: 0;
      padding: 10px;
      font-size: 10pt;
      width: 80mm;  /* Receipt width */
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
      border-bottom: 1px dashed #ccc;
      margin: 10px 0;
    }
    .item {
      margin-bottom: 5px;
    }
    .item-name {
      font-weight: bold;
    }
    .item-details {
      display: flex;
      justify-content: space-between;
      margin-left: 10px;
    }
    .total-section {
      margin-top: 10px;
      font-weight: bold;
    }
    .footer {
      text-align: center;
      margin-top: 15px;
      font-size: 9pt;
    }
  </style>
  <script>
    // Add an auto-print script that executes as soon as the page loads
    window.onload = function() {
      window.print();
    }
  </script>
</head>
<body>
  <div class="header">
    <div class="business-name">` + locationName + `</div>
    <div class="date">` + receipt.Date + `</div>`

	if receipt.CustomerName != nil {
		html += `    <div class="customer">Customer: ` + *receipt.CustomerName + `</div>`
	}

	html += `  </div>

  <div class="divider"></div>
  
  <div class="transaction-info">
    <div>Transaction ID: ` + receipt.TransactionID + `</div>
    <div>Payment: ` + strings.Title(receipt.PaymentType) + `</div>
  </div>
  
  <div class="divider"></div>
  
  <div class="items-section">
    <div style="font-weight: bold; margin-bottom: 5px;">ITEMS</div>`

	// Add items
	for _, item := range receipt.Items {
		name, _ := item["name"].(string)
		quantity, _ := item["quantity"].(float64)
		price, _ := item["price"].(float64)
		sku, _ := item["sku"].(string)

		html += fmt.Sprintf(`
    <div class="item">
      <div class="item-name">%s</div>
      <div class="item-details">
        <div>%.0f x $%.2f</div>
        <div>$%.2f</div>
      </div>`, name, quantity, price, quantity*price)

		if sku != "" {
			html += fmt.Sprintf(`
      <div style="margin-left: 10px; font-size: 8pt;">SKU: %s</div>`, sku)
		}

		html += `
    </div>`
	}

	html += `
  </div>
  
  <div class="divider"></div>
  
  <div class="totals-section">
    <div style="display: flex; justify-content: space-between;">
      <div>Subtotal:</div>
      <div>$` + fmt.Sprintf("%.2f", receipt.Subtotal) + `</div>
    </div>`

	// Add discount if applicable
	if receipt.DiscountAmount != nil && receipt.DiscountPercentage != nil {
		html += fmt.Sprintf(`
    <div style="display: flex; justify-content: space-between;">
      <div>Discount (%.0f%%):</div>
      <div>-$%.2f</div>
    </div>`, *receipt.DiscountPercentage, *receipt.DiscountAmount)
	}

	// Add tax
	html += `
    <div style="display: flex; justify-content: space-between;">
      <div>Tax:</div>
      <div>$` + fmt.Sprintf("%.2f", receipt.Tax) + `</div>
    </div>
    <div style="margin-left: 10px; font-size: 8pt;">
      <div>GST (5%): $` + fmt.Sprintf("%.2f", receipt.Subtotal*0.05) + `</div>
      <div>PST (7%): $` + fmt.Sprintf("%.2f", receipt.Subtotal*0.07) + `</div>
    </div>`

	// Add refund if applicable
	if receipt.RefundAmount != nil && *receipt.RefundAmount > 0 {
		html += fmt.Sprintf(`
    <div style="display: flex; justify-content: space-between;">
      <div>Refund:</div>
      <div>-$%.2f</div>
    </div>`, *receipt.RefundAmount)
	}

	// Add tip if applicable
	if receipt.Tip != nil && *receipt.Tip > 0 {
		html += fmt.Sprintf(`
    <div style="display: flex; justify-content: space-between;">
      <div>Tip:</div>
      <div>$%.2f</div>
    </div>`, *receipt.Tip)
	}

	// Total
	html += `
    <div style="display: flex; justify-content: space-between; margin-top: 10px; padding: 5px; background-color: #f5f5f5; font-weight: bold;">
      <div>TOTAL:</div>
      <div>$` + fmt.Sprintf("%.2f", receipt.Total) + `</div>
    </div>`

	// Cash payment details if applicable
	if receipt.PaymentType == "cash" && receipt.CashGiven != nil && receipt.ChangeDue != nil {
		html += fmt.Sprintf(`
    <div style="margin-top: 10px; padding: 5px; background-color: #f8f8f8;">
      <div style="display: flex; justify-content: space-between;">
        <div>Cash:</div>
        <div>$%.2f</div>
      </div>
      <div style="display: flex; justify-content: space-between;">
        <div>Change:</div>
        <div>$%.2f</div>
      </div>
    </div>`, *receipt.CashGiven, *receipt.ChangeDue)
	}

	// Footer
	html += `
  </div>
  
  <div class="divider"></div>
  
  <div class="footer">
    <div style="font-weight: bold;">Thank you for your purchase!</div>
    <div style="margin-top: 5px;">Visit us again at ` + locationName + `</div>
  </div>
</body>
</html>`

	return html
}

// printReceipt uses system commands to send HTML content to the default printer
func printReceipt(html string) error {
    log.Printf("=== PRINT RECEIPT FUNCTION STARTED ===")
    
    var tmpFilePath string
    var shouldDeleteFile bool = true

    if runtime.GOOS == "windows" {
        // Create temp file in the system temp directory
        tmpFile, err := ioutil.TempFile(os.TempDir(), "receipt-*.html")
        if err != nil {
            log.Printf("ERROR: Failed to create temp file: %v", err)
            return fmt.Errorf("error creating temp file: %w", err)
        }
        tmpFilePath = tmpFile.Name()
        log.Printf("Created temporary file at system temp location: %s", tmpFilePath)
        
        // Write the HTML content to the temp file
        log.Printf("Writing HTML content to file (%d bytes)", len(html))
        if _, err := tmpFile.Write([]byte(html)); err != nil {
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
        
        // Get absolute path for the file
        absolutePath, err := filepath.Abs(tmpFilePath)
        if err != nil {
            log.Printf("ERROR: Failed to get absolute path: %v", err)
            os.Remove(tmpFilePath)
            return fmt.Errorf("error getting absolute path: %w", err)
        }
        log.Printf("Absolute path: %s", absolutePath)
        
        // Skip Method 1 and go directly to Method 2
        log.Printf("Using Method 2: MSEdge with silent printing...")
        
        // Use the Edge browser to print in silent mode
        // This approach is more reliable for HTML content
        edgeCmd := exec.Command(
            "msedge",
            "--disable-gpu",
            "--disable-extensions",
            "--disable-software-rasterizer",
            "--headless",
            "--print-to-pdf-no-header",
            "--disable-print-preview",
            "--kiosk-printing",
            fmt.Sprintf("file:///%s", absolutePath),
        )
        
        var edgeOutputBuf, edgeErrorBuf bytes.Buffer
        edgeCmd.Stdout = &edgeOutputBuf
        edgeCmd.Stderr = &edgeErrorBuf
        
        log.Printf("Executing Edge print command")
        printSuccess := false
        
        if err := edgeCmd.Run(); err != nil {
            log.Printf("Edge method ERROR: %v", err)
            log.Printf("Edge method STDERR: %s", edgeErrorBuf.String())
            
            // Fallback to direct printer access via PowerShell
            log.Printf("Trying Method 3: Direct printer access via PowerShell...")
            
            // Get default printer name
            printerCmd := exec.Command("powershell", "-Command", "Get-WmiObject -Query \"SELECT * FROM Win32_Printer WHERE Default=$true\" | Select-Object -ExpandProperty Name")
            var printerBuf bytes.Buffer
            printerCmd.Stdout = &printerBuf
            
            if err := printerCmd.Run(); err != nil {
                log.Printf("WARNING: Could not get default printer name: %v", err)
                // Continue with empty printer name, Windows will use default
            }
            
            printerName := strings.TrimSpace(printerBuf.String())
            
            // Use Print.dll for direct printing - no reliance on content display
            printDllCmd := fmt.Sprintf(`$printerName = "%s"; $filePath = "%s"; Add-Type -Assembly "System.Drawing"; Add-Type -Assembly "System.Windows.Forms"; $printDoc = New-Object System.Drawing.Printing.PrintDocument; $printDoc.PrinterSettings.PrinterName = $printerName; $printDoc.PrintPage += { param($sender, $args) $webBrowser = New-Object System.Windows.Forms.WebBrowser; $webBrowser.DocumentText = Get-Content $filePath -Raw; $webBrowser.DrawToBitmap($args.Graphics, "0,0,$($webBrowser.Width),$($webBrowser.Height)") }; $printDoc.Print()`, printerName, absolutePath)
            
            log.Printf("Executing PowerShell direct print command")
            psDllCmd := exec.Command("powershell", "-Command", printDllCmd)
            
            var psDllOutput, psDllError bytes.Buffer
            psDllCmd.Stdout = &psDllOutput
            psDllCmd.Stderr = &psDllError
            
            if err := psDllCmd.Run(); err != nil {
                log.Printf("Method 3 ERROR: PowerShell direct printing failed: %v", err)
                log.Printf("Method 3 STDERR: %s", psDllError.String())
                
                // Last resort - open in browser and let user print
                log.Printf("Falling back to browser method...")
                browserCmd := exec.Command("cmd", "/c", "start", absolutePath)
                if err := browserCmd.Run(); err != nil {
                    log.Printf("ERROR: All print methods failed. Last error: %v", err)
                    // Keep the file and don't try to delete it
                    shouldDeleteFile = false
                    return fmt.Errorf("all print methods failed: %w", err)
                } else {
                    // Browser opened successfully
                    printSuccess = true
                    log.Printf("Opened receipt in browser. Waiting for user to print...")
                    time.Sleep(30 * time.Second)
                }
            } else {
                // PowerShell direct printing succeeded
                printSuccess = true
                log.Printf("Method 3: PowerShell direct printing succeeded")
                // Wait for printing to complete
                time.Sleep(15 * time.Second)
            }
        } else {
            // Edge printing succeeded
            printSuccess = true
            log.Printf("Edge printing succeeded")
            // Wait for printing to complete
            time.Sleep(10 * time.Second)
        }
        
        // Only return success if one of the print methods succeeded
        if !printSuccess {
            shouldDeleteFile = false
            return errors.New("failed to print receipt using any available method")
        }

	} else {
		// For non-Windows systems, use the default temp file approach
		tmpFile, err := ioutil.TempFile("", "receipt-*.html")
		if err != nil {
			log.Printf("ERROR: Failed to create temp file: %v", err)
			return fmt.Errorf("error creating temp file: %w", err)
		}
		tmpFilePath = tmpFile.Name()
		log.Printf("Created temporary file: %s", tmpFilePath)
		
		// Write the HTML content to the temp file
		log.Printf("Writing HTML content to file")
		if _, err := tmpFile.Write([]byte(html)); err != nil {
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
		
		// Print the file using the operating system's default printer
		var cmd *exec.Cmd
		if runtime.GOOS == "darwin" {
			// On macOS, use lp command
			log.Printf("Using lp command to print on macOS")
			cmd = exec.Command("lp", tmpFilePath)
		} else {
			// On Linux, use lp command
			log.Printf("Using lp command to print on Linux")
			cmd = exec.Command("lp", tmpFilePath)
		}
		
		// Execute the print command
		var outputBuf, errorBuf bytes.Buffer
		cmd.Stdout = &outputBuf
		cmd.Stderr = &errorBuf
		
		log.Printf("Executing print command")
		if err := cmd.Run(); err != nil {
			log.Printf("ERROR: Print command failed: %v", err)
			log.Printf("STDERR: %s", errorBuf.String())
			shouldDeleteFile = false
			return fmt.Errorf("error printing receipt: %w, stderr: %s", err, errorBuf.String())
		}
		
		log.Printf("Print command executed successfully")
		
		// Add a larger delay to ensure the print job is sent before we delete the file
		log.Printf("Waiting for print job to complete")
		time.Sleep(5 * time.Second)
	}
	
	// Return success now - before file cleanup
	log.Printf("=== PRINT RECEIPT FUNCTION COMPLETED SUCCESSFULLY ===")
	
	// Clean up temporary file only if we should
	if shouldDeleteFile {
        // We'll clean up in a separate goroutine to allow the HTTP response to return first
        go func(filePath string) {
            // Wait longer before removing the file
            time.Sleep(10 * time.Second)
            log.Printf("Cleaning up temporary file: %s", filePath)
            if err := os.Remove(filePath); err != nil {
                log.Printf("WARNING: Failed to remove temporary file: %v", err)
            } else {
                log.Printf("Successfully removed temporary file")
            }
        }(tmpFilePath)
    } else {
        log.Printf("Keeping temporary file for debugging: %s", tmpFilePath)
    }
	
	return nil
}

// handlePrintReceipt processes print requests
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
	
	log.Printf("Processing receipt - Transaction ID: %s, Location: %v, Items: %d", 
		receipt.TransactionID, receipt.Location, len(receipt.Items))

	// Generate HTML for the receipt
	html := generateReceiptHTML(receipt)
	log.Printf("Generated HTML receipt content (%d bytes)", len(html))

	// Print the receipt
	if err := printReceipt(html); err != nil {
		log.Printf("ERROR: Failed to print receipt: %v", err)
		writeJSONError(w, http.StatusInternalServerError, err)
		return
	}

	log.Printf("Receipt printed successfully")
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
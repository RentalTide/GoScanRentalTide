package main

import (
	"bytes"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"html/template"
	"io/ioutil"
	"log"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"runtime"
	"strconv"
	"strings"
	"time"
	"flag"
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

// ReceiptItem represents an item on a receipt
type ReceiptItem struct {
	Name     string      `json:"name"`
	Quantity interface{} `json:"quantity"` // Can be int or float64
	Price    float64     `json:"price"`
	SKU      string      `json:"sku,omitempty"`
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
	PromoAmount        float64       `json:"promoAmount,omitempty"`
	CashGiven          float64       `json:"cashGiven,omitempty"`
	ChangeDue          float64       `json:"changeDue,omitempty"`
	Copies             int           `json:"copies"`
	Type               string        `json:"type,omitempty"`      // Added for 'noSale' type
	Timestamp          string        `json:"timestamp,omitempty"` // Added for timestamp
	
	// Enhanced fields
	TerminalId           string                 `json:"terminalId,omitempty"`
	CardDetails          map[string]interface{} `json:"cardDetails,omitempty"`
	AccountId            string                 `json:"accountId,omitempty"`
	AccountBalanceBefore float64                `json:"accountBalanceBefore,omitempty"`
	AccountBalanceAfter  float64                `json:"accountBalanceAfter,omitempty"`
	SettlementAmount     float64                `json:"settlementAmount,omitempty"`
	TransactionFee       float64                `json:"transactionFee,omitempty"`
	InterchangeFee       float64                `json:"interchangeFee,omitempty"`
	GLCodeSummary        []map[string]interface{} `json:"glCodeSummary,omitempty"`
	IsSettlement         bool                   `json:"isSettlement,omitempty"`
	IsRetail             bool                   `json:"isRetail,omitempty"`
	HasCombinedTransaction bool                 `json:"hasCombinedTransaction,omitempty"`
	SkipTaxCalculation   bool                   `json:"skipTaxCalculation,omitempty"`
	HasNoTax             bool                   `json:"hasNoTax,omitempty"`
	LogoUrl              string                 `json:"logoUrl,omitempty"`
	
	// Derived fields (calculated before template rendering)
	ShowTaxBreakdown    bool                   `json:"-"`
}

// HTML template for the receipt
const receiptTemplate = `
<!DOCTYPE html>
<html>
<head>
    <meta charset="UTF-8">
    <title>Receipt</title>
    <style>
        body {
            font-family: 'Courier New', monospace;
            font-size: 12px;
            width: 80mm;
            margin: 0;
            padding: 10px;
        }
        .header {
            text-align: center;
            margin-bottom: 10px;
        }
        .items {
            width: 100%;
        }
        .item {
            margin-bottom: 5px;
        }
        .divider {
            border-top: 1px dashed #000;
            margin: 10px 0;
        }
        .total {
            font-weight: bold;
            margin-top: 5px;
        }
        .footer {
            text-align: center;
            margin-top: 20px;
        }
        .right-align {
            text-align: right;
        }
        .bold {
            font-weight: bold;
        }
    </style>
</head>
<body>
    {{if eq .Type "noSale"}}
    <div class="header bold">
        <div style="font-size: 16px;">NO SALE</div>
        <div>{{if .Timestamp}}{{.Timestamp}}{{else}}{{now}}{{end}}</div>
        {{if .Location}}
        {{if isString .Location}}
        <div>{{.Location}}</div>
        {{else}}
        <div>{{.Location.name}}</div>
        {{end}}
        {{end}}
    </div>
    {{else}}
    <div class="header">
        {{if isString .Location}}
        <div class="bold">{{.Location}}</div>
        {{else}}
        <div class="bold">{{.Location.name}}</div>
        {{end}}
        {{if .CustomerName}}<div>Customer: {{.CustomerName}}</div>{{end}}
        <div>{{.Date}}</div>
    </div>
    
    <div>Transaction ID: {{.TransactionID}}</div>
    <div>Payment: {{title .PaymentType}}</div>
    
    <div class="bold" style="margin-top: 10px;">ITEMS</div>
    <div class="divider"></div>
    
    {{range .Items}}
    <div class="item">
        <div>{{.Name}}</div>
        <div style="display: flex; justify-content: space-between;">
            <span>{{.Quantity}} x ${{printf "%.2f" .Price}}</span>
            <span>${{printf "%.2f" (multiply .Quantity .Price)}}</span>
        </div>
        {{if .SKU}}<div>SKU: {{.SKU}}</div>{{end}}
    </div>
    {{end}}
    
    <div class="divider"></div>
    
    <div style="display: flex; justify-content: space-between;">
        <span>Subtotal:</span>
        <span>${{printf "%.2f" .Subtotal}}</span>
    </div>
    
    {{if and (gt .DiscountPercentage 0) (gt .DiscountAmount 0)}}
    <div style="display: flex; justify-content: space-between;">
        <span>Discount ({{printf "%.0f" .DiscountPercentage}}%):</span>
        <span>-${{printf "%.2f" .DiscountAmount}}</span>
    </div>
    {{end}}
    
    <div style="display: flex; justify-content: space-between;">
        <span>Tax:</span>
        <span>${{printf "%.2f" .Tax}}</span>
    </div>
    
    <div style="margin-left: 10px;">
        <div style="display: flex; justify-content: space-between;">
            <span>GST (5%):</span>
            <span>${{printf "%.2f" (multiply .Subtotal 0.05)}}</span>
        </div>
        <div style="display: flex; justify-content: space-between;">
            <span>PST (7%):</span>
            <span>${{printf "%.2f" (multiply .Subtotal 0.07)}}</span>
        </div>
    </div>
    
    {{if gt .RefundAmount 0}}
    <div style="display: flex; justify-content: space-between;">
        <span>Refund:</span>
        <span>-${{printf "%.2f" .RefundAmount}}</span>
    </div>
    {{end}}
    
    {{if gt .Tip 0}}
    <div style="display: flex; justify-content: space-between;">
        <span>Tip:</span>
        <span>${{printf "%.2f" .Tip}}</span>
    </div>
    {{end}}
    
    <div class="total" style="display: flex; justify-content: space-between; margin-top: 10px;">
        <span>TOTAL:</span>
        <span>${{printf "%.2f" .Total}}</span>
    </div>
    
    {{if and (eq .PaymentType "cash") (gt .CashGiven 0)}}
    <div style="display: flex; justify-content: space-between;">
        <span>Cash:</span>
        <span>${{printf "%.2f" .CashGiven}}</span>
    </div>
    <div style="display: flex; justify-content: space-between;">
        <span>Change:</span>
        <span>${{printf "%.2f" .ChangeDue}}</span>
    </div>
    {{end}}
    
    <div class="divider"></div>
    
    <div class="footer">
        <div>Thank you for your purchase!</div>
        {{if isString .Location}}
        <div>Visit us again at {{.Location}}</div>
        {{else}}
        <div>Visit us again at {{.Location.name}}</div>
        {{end}}
    </div>
    {{end}}
</body>
</html>
`

// Template functions
var templateFuncs = template.FuncMap{
    "multiply": func(a interface{}, b interface{}) float64 {
        // Convert operands to float64 regardless of their original type
        var aFloat, bFloat float64
        
        switch v := a.(type) {
        case int:
            aFloat = float64(v)
        case float64:
            aFloat = v
        default:
            aFloat = 0
        }
        
        switch v := b.(type) {
        case int:
            bFloat = float64(v)
        case float64:
            bFloat = v
        default:
            bFloat = 0
        }
        
        return aFloat * bFloat
    },
    "title": strings.Title,
    "now": func() string {
        return time.Now().Format("2006-01-02 15:04:05")
    },
    "isString": func(v interface{}) bool {
        _, ok := v.(string)
        return ok
    },
    "contains": strings.Contains,
    "gt": func(a, b interface{}) bool {
        aFloat := toFloat64(a)
        bFloat := toFloat64(b)
        return aFloat > bFloat
    },
    "lt": func(a, b interface{}) bool {
        aFloat := toFloat64(a)
        bFloat := toFloat64(b)
        return aFloat < bFloat
    },
    "eq": func(a, b interface{}) bool {
        aFloat := toFloat64(a)
        bFloat := toFloat64(b)
        return aFloat == bFloat
    },
    "and": func(a, b bool) bool {
        return a && b
    },
    "or": func(a, b bool) bool {
        return a || b
    },
}

func parseBCLicenseData(raw string) LicenseData {
	fmt.Println("Parsing BC license data from raw input:")
	fmt.Println(raw)

	license := LicenseData{
		RawData:      raw,
		LicenseClass: "NA",
	}

	// Clean control characters
	raw = strings.TrimPrefix(raw, "\x15")
	raw = strings.ReplaceAll(raw, "\r", "")
	raw = strings.ReplaceAll(raw, "\n", "")

	parts := strings.Split(raw, "^")

	// City
	if len(parts) >= 1 && strings.HasPrefix(parts[0], "%BC") {
		license.City = strings.TrimSpace(strings.TrimPrefix(parts[0], "%BC"))
	}

	// Name
	if len(parts) >= 2 {
		nameParts := strings.Split(parts[1], ",")
		if len(nameParts) >= 2 {
			license.LastName = strings.TrimSpace(strings.TrimPrefix(nameParts[0], "$"))
			fullName := strings.TrimSpace(strings.TrimPrefix(nameParts[1], "$"))
			fnParts := strings.SplitN(fullName, " ", 2)
			license.FirstName = fnParts[0]
			if len(fnParts) > 1 {
				license.MiddleName = fnParts[1]
			}
		}
	}

	// Address, Province, Postal
	if len(parts) >= 3 {
		addressPart := parts[2]
		if strings.Contains(addressPart, "$") {
			addressParts := strings.Split(addressPart, "$")
			license.Address = strings.TrimSpace(addressParts[0])

			if len(addressParts) > 1 {
				statePostalPart := strings.TrimSpace(addressParts[1])
				if strings.Contains(statePostalPart, "BC") {
					license.State = "BC"
				}
				postalRegex := regexp.MustCompile(`[A-Z]\d[A-Z]\s?\d[A-Z]\d`)
				if match := postalRegex.FindString(statePostalPart); match != "" {
					license.Postal = match
				}
			}
		} else {
			license.Address = strings.TrimSpace(addressPart)
		}
	}

	// License number: extract last 7 digits after semicolon
	licenseNumMatch := regexp.MustCompile(`;(\d{13,16})=`).FindStringSubmatch(raw)
	if len(licenseNumMatch) > 1 {
		full := licenseNumMatch[1]
		if len(full) >= 7 {
			license.LicenseNumber = full[len(full)-7:]
		}
	}


	// Dates from =271220021204=
	dateMatch := regexp.MustCompile(`=(\d{12})=`).FindStringSubmatch(raw)
	if len(dateMatch) > 1 {
		dateStr := dateMatch[1]

		// Expiry: first 6 digits
		expiryDay := dateStr[0:2]
		expiryMonth := dateStr[2:4]
		expiryYear := "20" + dateStr[4:6]

		// DOB: next 6 digits
		dobYear := "20" + dateStr[6:8]
		dobMonth := dateStr[8:10]
		dobDay := dateStr[10:12]

		license.ExpiryDate = fmt.Sprintf("%s-%s-%s", expiryYear, expiryMonth, expiryDay)
		license.Dob = fmt.Sprintf("%s-%s-%s", dobYear, dobMonth, dobDay)
	}

	// Sex and Height
	sexHeight := regexp.MustCompile(`([MF])(\d{3})`).FindStringSubmatch(raw)
	if len(sexHeight) == 3 {
		license.Sex = sexHeight[1]
		license.Height = sexHeight[2] + "cm"
	}

	return license
}


// Original AAMVA format parser for other jurisdictions
func parseAAMVALicenseData(raw string) LicenseData {
	fmt.Println("Parsing AAMVA license data from raw input:")
	fmt.Println(raw)
	
	// Remove any NAK (0x15) character at the beginning
	raw = strings.TrimPrefix(raw, "\x15")
	
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
		case strings.HasPrefix(line, "DCF"):
			data["licenseNumber"] = strings.TrimSpace(line[3:])
			fmt.Println("Found licenseNumber (DCF):", data["licenseNumber"])
		
		case strings.HasPrefix(line, "DAQ"):
			if _, exists := data["licenseNumber"]; !exists {
				data["licenseNumber"] = strings.TrimSpace(line[3:])
				fmt.Println("Found licenseNumber (DAQ fallback):", data["licenseNumber"])
			}
		
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
		RawData:       raw,
	}
}

// Main parser that determines which format to use
func parseLicenseData(raw string) LicenseData {
	// Remove any NAK (0x15) character from the beginning for format detection
	cleanRaw := strings.TrimPrefix(raw, "\x15")
	
	// Determine the format of the license data
	if strings.Contains(cleanRaw, "%BC") {
		// This is a BC driver's license format
		return parseBCLicenseData(raw)
	} else if strings.Contains(cleanRaw, "%AB") {
		// This is an Alberta driver's license (also uses BC format parser)
		return parseBCLicenseData(raw)
	} else if strings.Contains(cleanRaw, "ANSI ") {
		// This is an AAMVA format license
		return parseAAMVALicenseData(raw)
	} else if strings.Contains(cleanRaw, "DCS") || strings.Contains(cleanRaw, "DAQ") {
		// This is likely an AAMVA format license
		return parseAAMVALicenseData(raw)
	} else {
		// Try BC format by default
		license := parseBCLicenseData(raw)
		
		// If we couldn't extract basic info, try AAMVA as a fallback
		if license.FirstName == "" && license.LastName == "" && license.LicenseNumber == "" {
			return parseAAMVALicenseData(raw)
		}
		
		return license
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
	maxWaitTime := 3 * time.Second  // Maximum overall wait time
	deadline := time.Now().Add(maxWaitTime)
	tmp := make([]byte, 128)

	fmt.Printf("Waiting for response... (timeout: %v, max wait: %v)\n", 
		readTimeout, maxWaitTime)
	fmt.Println("PLEASE SCAN YOUR LICENSE NOW - You have 10 seconds")
	
	hasReceivedData := false

	for time.Now().Before(deadline) {
		n, err := readWithTimeout(port, tmp, 3*time.Second)
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

// Convert interface to float64
func toFloat64(v interface{}) float64 {
    switch val := v.(type) {
    case int:
        return float64(val)
    case float32:
        return float64(val)
    case float64:
        return val
    case string:
        f, err := strconv.ParseFloat(val, 64)
        if err == nil {
            return f
        }
    case json.Number:
        f, err := val.Float64()
        if err == nil {
            return f
        }
    }
    return 0
}

// generateHTMLReceipt creates an HTML receipt from ReceiptData
func generateHTMLReceipt(receipt ReceiptData) (string, error) {
    // Parse the template
    tmpl, err := template.New("receipt").Funcs(templateFuncs).Parse(receiptTemplate)
    if err != nil {
        return "", fmt.Errorf("error parsing template: %v", err)
    }

    // Create a buffer to store the rendered HTML
    var buf bytes.Buffer
    if err := tmpl.Execute(&buf, receipt); err != nil {
        return "", fmt.Errorf("error executing template: %v", err)
    }

    return buf.String(), nil
}

// printReceipt generates HTML, converts to PDF, and prints
func printReceipt(receipt ReceiptData, printerName string) error {
    // Calculate derived fields
    receipt.ShowTaxBreakdown = !receipt.IsSettlement && !receipt.SkipTaxCalculation && !receipt.HasNoTax
    
    // Generate HTML receipt
    html, err := generateHTMLReceipt(receipt)
    if err != nil {
        return fmt.Errorf("error generating HTML receipt: %v", err)
    }

    // Create temporary HTML file
    htmlFile, err := ioutil.TempFile("", "receipt-*.html")
    if err != nil {
        return fmt.Errorf("error creating temporary HTML file: %v", err)
    }
    htmlPath := htmlFile.Name()
    defer os.Remove(htmlPath)

    // Write HTML to file
    if _, err := htmlFile.WriteString(html); err != nil {
        htmlFile.Close()
        return fmt.Errorf("error writing HTML to file: %v", err)
    }
    htmlFile.Close()

    // Create temporary PDF file path
    pdfPath := strings.TrimSuffix(htmlPath, filepath.Ext(htmlPath)) + ".pdf"
    defer os.Remove(pdfPath)

    // Convert HTML to PDF using headless browser
    fmt.Printf("Converting HTML to PDF using browser: %s\n", htmlPath)
    
    // Try different browsers in order of preference
    var cmd *exec.Cmd
    var output []byte
    var browserErr error
    
    // Start with Chrome
    chromeArgs := []string{
        "--headless",
        "--disable-gpu",
        "--no-margins",
        "--print-to-pdf=" + pdfPath,
        htmlPath,
    }
    
    // Try Microsoft Edge (Windows)
    if runtime.GOOS == "windows" {
        edgePath := "C:\\Program Files (x86)\\Microsoft\\Edge\\Application\\msedge.exe"
        if _, err := os.Stat(edgePath); os.IsNotExist(err) {
            // Try the other common location
            edgePath = "C:\\Program Files\\Microsoft\\Edge\\Application\\msedge.exe"
        }
        
        // Check if Edge exists
        if _, err := os.Stat(edgePath); err == nil {
            fmt.Println("Using Microsoft Edge for PDF conversion")
            cmd = exec.Command(edgePath, "--headless", "--disable-gpu", "--no-margins", "--print-to-pdf="+pdfPath, htmlPath)
            output, browserErr = cmd.CombinedOutput()
            if browserErr == nil {
                // Edge worked!
                fmt.Printf("PDF successfully generated with Edge: %s\n", pdfPath)
                goto PrintPDF
            } else {
                fmt.Printf("Edge failed: %v\n", browserErr)
            }
        }
    }
    
    // Try Chrome
    cmd = exec.Command("chrome", chromeArgs...)
    output, browserErr = cmd.CombinedOutput()
    if browserErr == nil {
        fmt.Printf("PDF successfully generated with Chrome: %s\n", pdfPath)
        goto PrintPDF
    }
    
    // Try Google Chrome
    cmd = exec.Command("google-chrome", chromeArgs...)
    output, browserErr = cmd.CombinedOutput()
    if browserErr == nil {
        fmt.Printf("PDF successfully generated with Google Chrome: %s\n", pdfPath)
        goto PrintPDF
    }
    
    // Try Chromium
    cmd = exec.Command("chromium-browser", chromeArgs...)
    output, browserErr = cmd.CombinedOutput()
    if browserErr == nil {
        fmt.Printf("PDF successfully generated with Chromium: %s\n", pdfPath)
        goto PrintPDF
    }
    
    // If we get here, all browsers failed
    return fmt.Errorf("error converting HTML to PDF: no compatible browser found\nLast error: %v\nOutput: %s", 
        browserErr, string(output))

PrintPDF:
    fmt.Printf("PDF generated: %s\n", pdfPath)

    // Print the PDF silently based on OS
    if runtime.GOOS == "windows" {
        // Windows: use PowerShell to print silently
        psCommand := fmt.Sprintf("Start-Process -FilePath \"%s\" -Verb Print -PassThru | %%{ sleep 2; $_.CloseMainWindow() }", pdfPath)
        if printerName != "" {
            // If printer name is specified, use it
            psCommand = fmt.Sprintf("Start-Process -FilePath \"%s\" -Verb PrintTo -ArgumentList '\"%s\"' -PassThru | %%{ sleep 2; $_.CloseMainWindow() }", pdfPath, printerName)
        }
        cmd = exec.Command("powershell", "-Command", psCommand)
        fmt.Printf("Printing PDF using PowerShell to printer: %s\n", printerName)
    } else if runtime.GOOS == "darwin" {
        // macOS: use lp command
        cmd = exec.Command("lp", "-d", printerName, pdfPath)
        fmt.Printf("Printing PDF using lp command on macOS to printer: %s\n", printerName)
    } else {
        // Linux: use lp command
        cmd = exec.Command("lp", "-d", printerName, pdfPath)
        fmt.Printf("Printing PDF using lp command on Linux to printer: %s\n", printerName)
    }

    output, err = cmd.CombinedOutput()
    if err != nil {
        return fmt.Errorf("error printing PDF: %v\nOutput: %s", err, string(output))
    }

    fmt.Printf("Successfully printed receipt\n")
    return nil
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
	}
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(resp)
}

// printReceiptHandler handles the receipt printing functionality
func printReceiptHandler(w http.ResponseWriter, r *http.Request, printerName string) {
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
    
    // Parse the JSON data with more flexible number handling
    var receipt ReceiptData
    d := json.NewDecoder(strings.NewReader(string(body)))
    d.UseNumber() // Use json.Number for numbers to avoid float64/int conversion issues
    if err := d.Decode(&receipt); err != nil {
        writeJSONError(w, http.StatusBadRequest, fmt.Errorf("error parsing JSON data: %v", err))
        return
    }
    
    // Validate receipt - skip validation for 'noSale' type
    if receipt.Type != "noSale" && receipt.TransactionID == "" {
        writeJSONError(w, http.StatusBadRequest, errors.New("transaction ID is required"))
        return
    }
    
    // Set default copies if not specified
    if receipt.Copies <= 0 {
        receipt.Copies = 1
    }
    
    // Print the requested number of copies
    successCount := 0
    for i := 0; i < receipt.Copies; i++ {
        fmt.Printf("Printing copy %d/%d\n", i+1, receipt.Copies)
        if err := printReceipt(receipt, printerName); err != nil {
            log.Printf("Print error (copy %d/%d): %v", i+1, receipt.Copies, err)
            continue
        }
        successCount++
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
	printerNameFlag := flag.String("printer", "Receipt1", "Printer name (default: Receipt1)")
	flag.Parse()
	
	readTimeout := time.Duration(*readTimeoutFlag) * time.Second
	
	fmt.Printf("Starting with scanner port: %s, serial port: %s, HTTP port: %d, read timeout: %d seconds\n", 
		*scannerPortFlag, *portFlag, *httpPortFlag, *readTimeoutFlag)
	fmt.Printf("Simple command: %v, Mac settings: %v\n", *useSimpleCommandFlag, *useMacSettingsFlag)
	fmt.Printf("Using printer: %s\n", *printerNameFlag)
	
	mux := http.NewServeMux()
	
	// Scanner endpoint
	mux.HandleFunc("/scanner/scan", func(w http.ResponseWriter, r *http.Request) {
		scannerHandler(w, r, *portFlag, *scannerPortFlag, *useSimpleCommandFlag, *useMacSettingsFlag, readTimeout)
	})
	
	// Receipt printing endpoint
	mux.HandleFunc("/print/receipt", func(w http.ResponseWriter, r *http.Request) {
		printReceiptHandler(w, r, *printerNameFlag)
	})
	
	log.Printf("Starting server on http://localhost:%d", *httpPortFlag)
	log.Printf("Scanner endpoint: http://localhost:%d/scanner/scan", *httpPortFlag)
	log.Printf("Receipt printer endpoint: http://localhost:%d/print/receipt", *httpPortFlag)
	
	if err := http.ListenAndServe(fmt.Sprintf(":%d", *httpPortFlag), corsMiddleware(mux)); err != nil {
		log.Fatal(err)
	}
}
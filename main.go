package main

import (
	"bytes"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"html/template"
	"io"
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
    
    {{if gt .PromoAmount 0}}
    <div style="display: flex; justify-content: space-between;">
        <span>Promo Discount:</span>
        <span>-${{printf "%.2f" .PromoAmount}}</span>
    </div>
    {{end}}

    <div style="display: flex; justify-content: space-between;">
        <span>Tax:</span>
        <span>${{printf "%.2f" .Tax}}</span>
    </div>
    
    <!-- Tax Breakdown - Only show for non-settlement transactions -->
    {{if .ShowTaxBreakdown}}
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
    {{end}}

    {{if gt .Tip 0}}
    <div style="display: flex; justify-content: space-between;">
        <span>Tip:</span>
        <span>${{printf "%.2f" .Tip}}</span>
    </div>
    {{end}}

    {{if gt .SettlementAmount 0}}
    <div style="display: flex; justify-content: space-between;">
        <span>Account Settlement:</span>
        <span>${{printf "%.2f" .SettlementAmount}}</span>
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
    
    <div style="margin-top: 10px;">
        <div style="font-weight: bold;">Payment Details</div>
        
        <div style="display: flex; justify-content: space-between;">
            <span>Payment Method:</span>
            <span>{{title .PaymentType}}</span>
        </div>
        
          {{if or (contains .PaymentType "credit") (contains .PaymentType "debit")}}

            <div style="display: flex; justify-content: space-between;">
              <span>Card:</span>
              <span style="font-weight: medium;">
                {{if index .CardDetails "cardBrand"}}
                  {{with index .CardDetails "cardBrand"}}
                    {{if isString .}}
                      {{title .}}
                    {{else}}
                      Card
                    {{end}}
                  {{end}}
                {{else}}
                  Card
                {{end}}
                {{if index .CardDetails "cardLast4"}}
                  {{with index .CardDetails "cardLast4"}}
                    {{if isString .}}
                      **** {{.}}
                    {{end}}
                  {{end}}
                {{end}}
              </span>
            </div>

            {{if index .CardDetails "authCode"}}
            <div style="display: flex; justify-content: space-between;">
              <span>Auth Code:</span>
              <span>
                {{index .CardDetails "authCode"}}
              </span>
            </div>
            {{end}}

            {{if .TerminalId}}
            <div style="display: flex; justify-content: space-between;">
              <span>Terminal ID:</span>
              <span>
                {{.TerminalId}}
              </span>
            </div>
            {{end}}

          {{end}}
    </div>
    
    {{if .AccountId}}
    <div style="margin-top: 10px;">
        <div style="font-weight: bold;">Account Information</div>
        
        <div style="display: flex; justify-content: space-between;">
            <span>Account ID:</span>
            <span>{{.AccountId}}</span>
        </div>
        
        {{if or .IsSettlement .HasCombinedTransaction}}
        <div style="display: flex; justify-content: space-between;">
            <span>Previous Balance:</span>
            <span>${{printf "%.2f" .AccountBalanceBefore}}</span>
        </div>
        
        <div style="display: flex; justify-content: space-between;">
            <span>New Balance:</span>
            <span>${{printf "%.2f" .AccountBalanceAfter}}</span>
        </div>
        {{end}}
    </div>
    {{end}}
    
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

// ensureAppDirectory creates and returns the application's dedicated directory
func ensureAppDirectory() (string, error) {
    var appDir string
    if runtime.GOOS == "windows" {
        // On Windows, ensure we have a backslash after the drive letter
        appDir = "C:\\GoScanRentalTide-main"
    } else {
        // On other systems, use standard path joining
        appDir = filepath.Join("/", "opt", "GoScanRentalTide-main")
    }
    
    // Create directories if they don't exist
    if err := os.MkdirAll(appDir, 0755); err != nil {
        return "", fmt.Errorf("failed to create application directory: %v", err)
    }
    
    // Create temp subdirectory
    tempDir := filepath.Join(appDir, "temp")
    if err := os.MkdirAll(tempDir, 0755); err != nil {
        return "", fmt.Errorf("failed to create temp directory: %v", err)
    }
    
    // Create logs subdirectory
    logsDir := filepath.Join(appDir, "logs")
    if err := os.MkdirAll(logsDir, 0755); err != nil {
        return "", fmt.Errorf("failed to create logs directory: %v", err)
    }
    
    return appDir, nil
}

// setupLogging configures logging to write to a file in our app directory
func setupLogging() (*os.File, error) {
    appDir, err := ensureAppDirectory()
    if err != nil {
        return nil, err
    }
    
    // Create log file with timestamp in name
    timestamp := time.Now().Format("2006-01-02")
    logPath := filepath.Join(appDir, "logs", fmt.Sprintf("goscantide-%s.log", timestamp))
    
    // Open log file for appending
    logFile, err := os.OpenFile(logPath, os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0666)
    if err != nil {
        return nil, fmt.Errorf("failed to open log file: %v", err)
    }
    
    // Configure logger to write to file and stdout
    log.SetOutput(io.MultiWriter(logFile, os.Stdout))
    log.SetFlags(log.Ldate | log.Ltime | log.Lshortfile)
    
    log.Printf("Logging initialized: %s", logPath)
    return logFile, nil
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

		// DOB: next 6 digits - check if year should be 19xx or 20xx
		dobYearShort := dateStr[6:8]
		dobYear := ""
		dobYearNum, _ := strconv.Atoi(dobYearShort)
		currentYear := time.Now().Year() % 100 // Get last two digits of current year
		
		// If the year is greater than the current year, it's likely from the previous century
		if dobYearNum > currentYear {
			dobYear = "19" + dobYearShort
		} else {
			dobYear = "20" + dobYearShort
		}
		
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

    // Get app directory
    appDir, err := ensureAppDirectory()
    if err != nil {
        return fmt.Errorf("error ensuring app directory: %v", err)
    }
    
    // Create temporary file paths in our app directory
    timestamp := time.Now().Format("20060102-150405")
    var htmlPath, pdfPath string
    
    if runtime.GOOS == "windows" {
        // Use proper Windows path format
        htmlPath = filepath.Join(appDir, "temp", fmt.Sprintf("receipt-%s.html", timestamp))
        pdfPath = filepath.Join(appDir, "temp", fmt.Sprintf("receipt-%s.pdf", timestamp))
        
        // Ensure paths are using Windows backslashes
        htmlPath = strings.ReplaceAll(htmlPath, "/", "\\")
        pdfPath = strings.ReplaceAll(pdfPath, "/", "\\")
        
        // Double-check to ensure the directory exists
        tempDir := filepath.Join(appDir, "temp")
        if err := os.MkdirAll(tempDir, 0755); err != nil {
            return fmt.Errorf("error ensuring temp directory exists: %v", err)
        }
        
        // Log the exact paths
        log.Printf("Windows file paths: HTML=%s, PDF=%s", htmlPath, pdfPath)
    } else {
        // Unix-style paths
        htmlPath = filepath.Join(appDir, "temp", fmt.Sprintf("receipt-%s.html", timestamp))
        pdfPath = filepath.Join(appDir, "temp", fmt.Sprintf("receipt-%s.pdf", timestamp))
    }
    
    // Write HTML to file
    log.Printf("Writing HTML to file: %s", htmlPath)
    err = ioutil.WriteFile(htmlPath, []byte(html), 0644)
    if err != nil {
        log.Printf("Error writing HTML file: %v", err)
        return fmt.Errorf("error writing HTML to file: %v", err)
    }
    
    // Verify the HTML file was created
    if fileInfo, err := os.Stat(htmlPath); os.IsNotExist(err) {
        log.Printf("HTML file not created at: %s", htmlPath)
        return fmt.Errorf("HTML file was not created at: %s", htmlPath)
    } else {
        log.Printf("HTML file created successfully: %s (size: %d bytes)", htmlPath, fileInfo.Size())
    }
    
    // Convert HTML to PDF using headless browser
    fmt.Printf("Converting HTML to PDF using browser: %s\n", htmlPath)
    log.Printf("Converting HTML to PDF: %s -> %s\n", htmlPath, pdfPath)
    
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
            log.Println("Using Microsoft Edge for PDF conversion")
            cmd = exec.Command(edgePath, "--headless", "--disable-gpu", "--no-margins", "--print-to-pdf="+pdfPath, htmlPath)
            output, browserErr = cmd.CombinedOutput()
            if browserErr == nil {
                // Edge worked!
                fmt.Printf("PDF successfully generated with Edge: %s\n", pdfPath)
                log.Printf("PDF successfully generated with Edge: %s\n", pdfPath)
                goto PrintPDF
            } else {
                fmt.Printf("Edge failed: %v\n", browserErr)
                log.Printf("Edge failed: %v\n%s", browserErr, string(output))
            }
        }
    }
    
    // Try Chrome
    cmd = exec.Command("chrome", chromeArgs...)
    output, browserErr = cmd.CombinedOutput()
    if browserErr == nil {
        fmt.Printf("PDF successfully generated with Chrome: %s\n", pdfPath)
        log.Printf("PDF successfully generated with Chrome: %s\n", pdfPath)
        goto PrintPDF
    } else {
        log.Printf("Chrome failed: %v\n%s", browserErr, string(output))
    }
    
    // Try Google Chrome
    cmd = exec.Command("google-chrome", chromeArgs...)
    output, browserErr = cmd.CombinedOutput()
    if browserErr == nil {
        fmt.Printf("PDF successfully generated with Google Chrome: %s\n", pdfPath)
        log.Printf("PDF successfully generated with Google Chrome: %s\n", pdfPath)
        goto PrintPDF
    } else {
        log.Printf("Google Chrome failed: %v\n%s", browserErr, string(output))
    }
    
    // Try Chromium
    cmd = exec.Command("chromium-browser", chromeArgs...)
    output, browserErr = cmd.CombinedOutput()
    if browserErr == nil {
        fmt.Printf("PDF successfully generated with Chromium: %s\n", pdfPath)
        log.Printf("PDF successfully generated with Chromium: %s\n", pdfPath)
        goto PrintPDF
    } else {
        log.Printf("Chromium failed: %v\n%s", browserErr, string(output))
    }
    
    // If we get here, all browsers failed
    return fmt.Errorf("error converting HTML to PDF: no compatible browser found\nLast error: %v\nOutput: %s", 
        browserErr, string(output))

PrintPDF:
    fmt.Printf("PDF generated: %s\n", pdfPath)
    log.Printf("PDF generated: %s\n", pdfPath)
    
    // Add a small delay to ensure the file is fully written and accessible
    time.Sleep(500 * time.Millisecond)
    
    // Verify the PDF file exists
    fileInfo, err := os.Stat(pdfPath)
    if err != nil {
        log.Printf("Warning - PDF file access issue: %v (will continue anyway)", err)
    } else {
        log.Printf("PDF file verified: %s (size: %d bytes)", pdfPath, fileInfo.Size())
    }

    // Print the PDF silently based on OS
    if runtime.GOOS == "windows" {
        // Log the file existence and size
        fileInfo, err := os.Stat(pdfPath)
        if err != nil {
            log.Printf("Error checking PDF file: %v", err)
        } else {
            log.Printf("PDF file exists at %s (size: %d bytes)", pdfPath, fileInfo.Size())
        }

        // For Windows, try several printing methods in order of reliability
        
        // Method 1: Print using ShellExecute with verb "print"
        log.Printf("Method 1: Using ShellExecute with 'print' verb...")
        shellCmd := exec.Command("cmd", "/c", "start", "", "/wait", "/b", "powershell", "-Command", 
            fmt.Sprintf("(New-Object -ComObject WScript.Shell).ShellExecute('%s', '', '', 'print', 1)", pdfPath))
        shellOutput, shellErr := shellCmd.CombinedOutput()
        
        if shellErr == nil {
            log.Printf("Successfully printed with ShellExecute")
            fmt.Printf("Successfully printed receipt\n")
            return nil  // Return nil to indicate success
        } else {
            log.Printf("ShellExecute printing error: %v\n%s", shellErr, string(shellOutput))
        }
        
        // Method 2: Use direct system command line printer
        log.Printf("Method 2: Using direct system print command...")
        
        sysCmd := exec.Command("cmd", "/c", "print", pdfPath)
        sysOutput, sysErr := sysCmd.CombinedOutput()
        
        if sysErr == nil {
            log.Printf("Successfully printed with system print command")
            fmt.Printf("Successfully printed receipt using system command\n")
            return nil
        } else {
            log.Printf("System print command error: %v\n%s", sysErr, string(sysOutput))
        }
        
        // Method 3: Try AcroRd32.exe if Adobe Reader is installed
        log.Printf("Method 3: Checking for Adobe Reader...")
        
        adobePaths := []string{
            "C:\\Program Files (x86)\\Adobe\\Acrobat Reader DC\\Reader\\AcroRd32.exe",
            "C:\\Program Files\\Adobe\\Acrobat Reader DC\\Reader\\AcroRd32.exe",
            "C:\\Program Files (x86)\\Adobe\\Reader\\AcroRd32.exe",
            "C:\\Program Files\\Adobe\\Reader\\AcroRd32.exe",
        }
        
        for _, adobePath := range adobePaths {
            if _, err := os.Stat(adobePath); err == nil {
                log.Printf("Found Adobe Reader at: %s", adobePath)
                
                // Print silently with Adobe Reader
                adobeCmd := exec.Command(adobePath, "/t", pdfPath, printerName)
                adobeOutput, adobeErr := adobeCmd.CombinedOutput()
                
                if adobeErr == nil {
                    log.Printf("Successfully printed with Adobe Reader")
                    fmt.Printf("Successfully printed receipt using Adobe Reader\n")
                    return nil
                } else {
                    log.Printf("Adobe Reader printing error: %v\n%s", adobeErr, string(adobeOutput))
                }
                
                break
            }
        }
        
        // Method 4: Try SumatraPDF if available
        log.Printf("Method 4: Checking for SumatraPDF...")
        
        sumatraPaths := []string{
            "C:\\Program Files\\SumatraPDF\\SumatraPDF.exe",
            "C:\\Program Files (x86)\\SumatraPDF\\SumatraPDF.exe",
        }
        
        for _, sumatraPath := range sumatraPaths {
            if _, err := os.Stat(sumatraPath); err == nil {
                log.Printf("Found SumatraPDF at: %s", sumatraPath)
                
                // Print silently with SumatraPDF
                var sumatraCmd *exec.Cmd
                
                if printerName != "" {
                    sumatraCmd = exec.Command(sumatraPath, "-print-to", printerName, "-silent", pdfPath)
                } else {
                    sumatraCmd = exec.Command(sumatraPath, "-print-to-default", "-silent", pdfPath)
                }
                
                sumatraOutput, sumatraErr := sumatraCmd.CombinedOutput()
                
                if sumatraErr == nil {
                    log.Printf("Successfully printed with SumatraPDF")
                    fmt.Printf("Successfully printed receipt using SumatraPDF\n")
                    return nil
                } else {
                    log.Printf("SumatraPDF printing error: %v\n%s", sumatraErr, string(sumatraOutput))
                }
                
                break
            }
        }
        
        // Method 5: Last resort - open the PDF for manual printing
        log.Printf("Method 5: Opening PDF for manual printing...")
        
        openCmd := exec.Command("cmd", "/c", "start", "", pdfPath)
        openErr := openCmd.Start()
        
        if openErr == nil {
            log.Printf("Opened PDF file for manual printing")
            return fmt.Errorf("automatic printing failed, opened PDF for manual printing at: %s", pdfPath)
        } else {
            log.Printf("Error opening PDF: %v", openErr)
            return fmt.Errorf("all printing methods failed. PDF saved at: %s", pdfPath)
        }
    } else if runtime.GOOS == "darwin" {
        // macOS: use lp command
        cmd = exec.Command("lp", "-d", printerName, pdfPath)
        fmt.Printf("Printing PDF using lp command on macOS to printer: %s\n", printerName)
        log.Printf("Printing PDF using lp command on macOS to printer: %s\n", printerName)
    } else {
        // Linux: use lp command
        cmd = exec.Command("lp", "-d", printerName, pdfPath)
        fmt.Printf("Printing PDF using lp command on Linux to printer: %s\n", printerName)
        log.Printf("Printing PDF using lp command on Linux to printer: %s\n", printerName)
    }

    // For macOS and Linux only, execute the command
    if runtime.GOOS == "darwin" || runtime.GOOS == "linux" {
        output, err := cmd.CombinedOutput()
        if err != nil {
            log.Printf("Printing error: %v\n%s", err, string(output))
            return fmt.Errorf("error printing PDF: %v\nOutput: %s", err, string(output))
        }
    }

    fmt.Printf("Successfully printed receipt\n")
    log.Printf("Successfully printed receipt\n")
    
    // We'll keep the files for debugging purposes
    // They're in our dedicated app directory, so they won't clutter the temp folder
    
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
    var lastError error
    
    for i := 0; i < receipt.Copies; i++ {
        fmt.Printf("Printing copy %d/%d\n", i+1, receipt.Copies)
        if err := printReceipt(receipt, printerName); err != nil {
            // If the error message contains "opened PDF for manual printing" or 
            // mentions ShellExecute or any indication of successful printing,
            // consider it a partial success
            if strings.Contains(err.Error(), "opened PDF for manual printing") || 
               strings.Contains(err.Error(), "ShellExecute") ||
               strings.Contains(err.Error(), "successfully printed") {
                successCount++
                log.Printf("Counted as success despite error: %v", err)
            } else {
                log.Printf("Print error (copy %d/%d): %v", i+1, receipt.Copies, err)
                lastError = err
            }
        } else {
            successCount++
        }
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
        var errMsg string
        if lastError != nil {
            errMsg = lastError.Error()
        } else {
            errMsg = "failed to print any copies"
        }
        writeJSONError(w, http.StatusInternalServerError, errors.New(errMsg))
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
	
	// Set up our application directory and logging
	logFile, err := setupLogging()
	if err != nil {
		fmt.Printf("Error setting up logging: %v\n", err)
		os.Exit(1)
	}
	defer logFile.Close()
	
	// Create app directory if it doesn't exist
	appDir, err := ensureAppDirectory()
	if err != nil {
		log.Fatalf("Error creating app directory: %v", err)
	}
	
	readTimeout := time.Duration(*readTimeoutFlag) * time.Second
	
	log.Printf("Application directory: %s", appDir)
	log.Printf("Starting with scanner port: %s, serial port: %s, HTTP port: %d, read timeout: %d seconds", 
		*scannerPortFlag, *portFlag, *httpPortFlag, *readTimeoutFlag)
	log.Printf("Simple command: %v, Mac settings: %v", *useSimpleCommandFlag, *useMacSettingsFlag)
	log.Printf("Using printer: %s", *printerNameFlag)
	
	mux := http.NewServeMux()
	
	// Scanner endpoint
	mux.HandleFunc("/scanner/scan", func(w http.ResponseWriter, r *http.Request) {
		scannerHandler(w, r, *portFlag, *scannerPortFlag, *useSimpleCommandFlag, *useMacSettingsFlag, readTimeout)
	})
	
	// Receipt printing endpoint
	mux.HandleFunc("/print/receipt", func(w http.ResponseWriter, r *http.Request) {
		printReceiptHandler(w, r, *printerNameFlag)
	})
	
	// Add a status endpoint
	mux.HandleFunc("/status", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(map[string]interface{}{
			"status": "ok",
			"version": "1.0.0",
			"appDir": appDir,
			"time": time.Now().Format(time.RFC3339),
		})
	})
	
	log.Printf("Starting server on http://localhost:%d", *httpPortFlag)
	log.Printf("Scanner endpoint: http://localhost:%d/scanner/scan", *httpPortFlag)
	log.Printf("Receipt printer endpoint: http://localhost:%d/print/receipt", *httpPortFlag)
	log.Printf("Status endpoint: http://localhost:%d/status", *httpPortFlag)
	
	if err := http.ListenAndServe(fmt.Sprintf(":%d", *httpPortFlag), corsMiddleware(mux)); err != nil {
		log.Fatal(err)
	}
}
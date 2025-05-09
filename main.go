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
	"strconv"
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
	Quantity float64 `json:"quantity"`
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
	Type               string        `json:"type,omitempty"`      // Added for 'noSale' type
	Timestamp          string        `json:"timestamp,omitempty"` // Added for timestamp
}

func parseBCLicenseData(raw string) LicenseData {
	fmt.Println("Parsing BC license data from raw input:")
	fmt.Println(raw)
	
	// Initialize license data
	license := LicenseData{
		RawData: raw,
		LicenseClass: "NA", // Default license class
	}
	
	// Remove any NAK (0x15) or control characters at the beginning
	raw = strings.TrimPrefix(raw, "\x15")
	
	// First, clean up the data
	raw = strings.ReplaceAll(raw, "\r", "")
	raw = strings.ReplaceAll(raw, "\n", "")
	
	// Split by carets (^)
	parts := strings.Split(raw, "^")
	
	// Extract info based on BC driver's license format
	if len(parts) >= 1 {
		// First part contains jurisdiction and city
		if strings.HasPrefix(parts[0], "%BC") {
			// Extract city
			cityPart := strings.TrimPrefix(parts[0], "%BC")
			license.City = strings.TrimSpace(cityPart)
			fmt.Println("Found city:", license.City)
		}
	}
	
	// Second part contains last name and first name
	if len(parts) >= 2 {
		nameParts := strings.Split(parts[1], ",")
		if len(nameParts) >= 2 {
			// Last name is before the comma
			license.LastName = strings.TrimSpace(strings.TrimPrefix(nameParts[0], "$"))
			fmt.Println("Found lastName:", license.LastName)
			
			// First name and possibly middle name after the comma
			fullNamePart := strings.TrimSpace(nameParts[1])
			// Remove the $ at the beginning if present
			fullNamePart = strings.TrimPrefix(fullNamePart, "$")
			
			// Check for middle name (split on space)
			firstNameParts := strings.Split(fullNamePart, " ")
			if len(firstNameParts) > 1 {
				license.FirstName = strings.TrimSpace(firstNameParts[0])
				license.MiddleName = strings.TrimSpace(strings.Join(firstNameParts[1:], " "))
			} else {
				license.FirstName = fullNamePart
			}
			fmt.Println("Found firstName:", license.FirstName)
			if license.MiddleName != "" {
				fmt.Println("Found middleName:", license.MiddleName)
			}
		}
	}
	
	// Third part contains address
	if len(parts) >= 3 {
		addressPart := parts[2]
		if strings.Contains(addressPart, "$") {
			// The address might have a $ separator for city/state
			addressParts := strings.Split(addressPart, "$")
			
			// The street address is the first part before the $
			license.Address = strings.TrimSpace(addressParts[0])
			fmt.Println("Found address:", license.Address)
			
			// If we have state/postal information
			if len(addressParts) > 1 {
				// Look for "BC" explicitly in the address part to extract state
				if strings.Contains(addressParts[1], "BC") {
					license.State = "BC"
					fmt.Println("Found state:", license.State)
					
					// Look for postal code pattern after BC (Canadian format)
					postalCodeMatch := regexp.MustCompile(`V\d[A-Z]\d`).FindString(addressParts[1])
					if postalCodeMatch != "" {
						license.Postal = postalCodeMatch
						fmt.Println("Found postal:", license.Postal)
					}
				}
			}
		} else {
			license.Address = strings.TrimSpace(addressPart)
			fmt.Println("Found address:", license.Address)
		}
	}
	
	// Extract license number - usually after the semicolon (;)
	// Based on the example, the license number is "02356646" (without the prefix)
	licenseNumberMatch := regexp.MustCompile(`;(\d+)=`).FindStringSubmatch(raw)
	if len(licenseNumberMatch) > 1 {
		// Extract the full number
		fullNumber := licenseNumberMatch[1]
		
		// For BC licenses, the actual license number is the last 8 digits
		if len(fullNumber) > 8 {
			license.LicenseNumber = fullNumber[len(fullNumber)-8:]
		} else {
			license.LicenseNumber = fullNumber
		}
		
		fmt.Println("Found licenseNumber:", license.LicenseNumber)
	}
	
	// Extract birth date from the BC license format
	// Based on your clarification, the format after "=" is:
	// DDMMYY (expiry) followed by YYMMDD (birth date)
	// Example: "271220051212" where:
	// - 271220: License expiry date (Dec 27, 2020)
	// - 051212: Birth date (Dec 12, 2005)
	dobMatch := regexp.MustCompile(`=(\d{12})=`).FindStringSubmatch(raw)
	if len(dobMatch) > 1 && len(dobMatch[1]) == 12 {
		dateStr := dobMatch[1]
		
		// First 6 digits are expiry date (not used currently)
		// expiryDay := dateStr[0:2]
		// expiryMonth := dateStr[2:4]
		// expiryYear := dateStr[4:6]
		
		// Last 6 digits are birth date (YYMMDD)
		birthYear := dateStr[6:8]
		birthMonth := dateStr[8:10]
		birthDay := dateStr[10:12]
		
		// Add century for birth year
		var fullBirthYear string
		birthYearNum, _ := strconv.Atoi(birthYear)
		currentYear := time.Now().Year() % 100
		if birthYearNum > currentYear {
			fullBirthYear = "19" + birthYear
		} else {
			fullBirthYear = "20" + birthYear
		}
		
		// Format birth date as YYYY/MM/DD
		license.Dob = fmt.Sprintf("%s/%s/%s", fullBirthYear, birthMonth, birthDay)
		fmt.Println("Found birth date:", license.Dob)
		
		// If needed, store expiry date too
		// fullExpiryYear := "20" + expiryYear
		// license.ExpiryDate = fmt.Sprintf("%s/%s/%s", fullExpiryYear, expiryMonth, expiryDay)
		// fmt.Println("Found expiry date:", license.ExpiryDate)
	}
	
	// Extract sex
	if strings.Contains(raw, "M ") {
		license.Sex = "M"
		fmt.Println("Found sex: M")
	} else if strings.Contains(raw, "F ") {
		license.Sex = "F"
		fmt.Println("Found sex: F")
	}
	
	// Extract height - usually in format like "M183" (in cm)
	heightMatch := regexp.MustCompile(`M(\d{3})`).FindStringSubmatch(raw)
	if len(heightMatch) > 1 {
		license.Height = heightMatch[1] + "cm" // Format as "183cm"
		fmt.Println("Found height:", license.Height)
	}
	
	// Extract physical attributes like eye color
	eyeColorMatch := regexp.MustCompile(`\b(BRN|HAZ|BLU|GRN)\b`).FindString(raw)
	if eyeColorMatch != "" {
		fmt.Println("Found eye color:", eyeColorMatch)
		// You can add this to a field if needed
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
	maxWaitTime := 15 * time.Second  // Maximum overall wait time
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

// generateESCPOSCommands creates raw ESC/POS commands for thermal printers
func generateESCPOSCommands(receipt ReceiptData) ([]byte, error) {
	var cmd bytes.Buffer
	
	// Initialize printer - make sure it's in a clean state
	cmd.Write([]byte{0x1B, 0x40}) // ESC @ - Initialize printer
	
	// Set character code page to PC437 (USA, Standard Europe) for consistent character rendering
	cmd.Write([]byte{0x1B, 0x74, 0x00}) // ESC t 0
	
	// For 80mm printer, use appropriate font sizing
	// Standard 80mm thermal printer can fit 42-48 characters per line at normal width
	
	// Center align
	cmd.Write([]byte{0x1B, 0x61, 0x01}) // ESC a 1 - Center alignment
	
	// Handle the special case for "No Sale" receipt
	if receipt.Type == "noSale" {
		// Set larger font for header
		cmd.Write([]byte{0x1D, 0x21, 0x11}) // GS ! 17 - Double width & height
		
		// Set bold text
		cmd.Write([]byte{0x1B, 0x45, 0x01}) // ESC E 1 (bold)
		cmd.WriteString("NO SALE\n\n")
		cmd.Write([]byte{0x1B, 0x45, 0x00}) // ESC E 0 (cancel bold)
		
		// Set normal size for timestamp
		cmd.Write([]byte{0x1D, 0x21, 0x00}) // GS ! 0 - Normal size
		
		// Add timestamp
		if receipt.Timestamp != "" {
			cmd.WriteString(receipt.Timestamp + "\n\n")
		} else {
			// Use current time if no timestamp provided
			currentTime := time.Now().Format("2006-01-02 15:04:05")
			cmd.WriteString(currentTime + "\n\n")
		}
		
		// Add location if available
		if receipt.Location != nil {
			var locationName string
			switch loc := receipt.Location.(type) {
			case string:
				locationName = loc
			case map[string]interface{}:
				if name, ok := loc["name"].(string); ok {
					locationName = name
				}
			}
			
			if locationName != "" && locationName != "noSale" {
				cmd.WriteString(locationName + "\n")
			}
		}
		
		// Extra space and cut paper
		cmd.WriteString("\n\n\n")
		cmd.Write([]byte{0x1D, 0x56, 0x41, 0x10}) // GS V A 16 (partial cut with feed)
		
		return cmd.Bytes(), nil
	}
	
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
	
	// Print header with large font
	cmd.Write([]byte{0x1D, 0x21, 0x11}) // GS ! 17 - Double width & height
	cmd.Write([]byte{0x1B, 0x45, 0x01}) // ESC E 1 (bold)
	cmd.WriteString(locationName + "\n")
	cmd.Write([]byte{0x1B, 0x45, 0x00}) // ESC E 0 (cancel bold)
	cmd.Write([]byte{0x1D, 0x21, 0x00}) // GS ! 0 - Normal size
	
	if receipt.CustomerName != "" {
		cmd.WriteString("Customer: " + receipt.CustomerName + "\n")
	}
	
	cmd.WriteString(receipt.Date + "\n\n")
	
	// Left align for the details
	cmd.Write([]byte{0x1B, 0x61, 0x00}) // ESC a 0 - Left alignment
	
	// Check for extremely long transaction IDs and truncate if necessary
	transactionID := receipt.TransactionID
	if len(transactionID) > 40 {
		// Truncate and add ellipsis
		transactionID = transactionID[:37] + "..."
	}
	
	// Print transaction info
	cmd.WriteString("Transaction ID: " + transactionID + "\n")
	cmd.WriteString("Payment: " + strings.Title(receipt.PaymentType) + "\n\n")
	
	// Print items section if there are items
	if len(receipt.Items) > 0 {
		// Print items header with larger font and bold
		cmd.Write([]byte{0x1D, 0x21, 0x01}) // GS ! 1 - Double width
		cmd.Write([]byte{0x1B, 0x45, 0x01}) // ESC E 1 (bold)
		cmd.WriteString("ITEMS\n")
		cmd.Write([]byte{0x1B, 0x45, 0x00}) // ESC E 0 (cancel bold)
		cmd.Write([]byte{0x1D, 0x21, 0x00}) // GS ! 0 - Normal size
		
		// Divider line - full width for 80mm (48 chars)
		cmd.Write([]byte{0x1B, 0x2D, 0x01}) // ESC - 1 (underline)
		cmd.WriteString("------------------------------------------------\n") // 48 dashes
		cmd.Write([]byte{0x1B, 0x2D, 0x00}) // ESC - 0 (cancel underline)
		
		for _, item := range receipt.Items {
			// Item name in bold
			cmd.Write([]byte{0x1B, 0x45, 0x01}) // ESC E 1 (bold)
			cmd.WriteString(item.Name + "\n")
			cmd.Write([]byte{0x1B, 0x45, 0x00}) // ESC E 0 (cancel bold)
			
			// Format quantity with appropriate precision
			var quantityStr string
			if item.Quantity == float64(int(item.Quantity)) {
				// If it's a whole number, display as integer
				quantityStr = fmt.Sprintf("%d", int(item.Quantity))
			} else {
				// Otherwise, display with up to 3 decimal places, trimming trailing zeros
				quantityStr = strings.TrimRight(strings.TrimRight(fmt.Sprintf("%.3f", item.Quantity), "0"), ".")
			}
			
			quantityPrice := fmt.Sprintf("%s x $%.2f", quantityStr, item.Price)
			// Calculate total price for the item
			itemTotal := fmt.Sprintf("$%.2f", item.Quantity*item.Price)
			cmd.WriteString(fmt.Sprintf("  %-34s %10s\n", quantityPrice, itemTotal))
			
			if item.SKU != "" {
				cmd.WriteString("  SKU: " + item.SKU + "\n")
			}
			cmd.WriteString("\n")
		}
	}
	
	// Divider line - full width for 80mm (48 chars)
	cmd.Write([]byte{0x1B, 0x2D, 0x01}) // ESC - 1 (underline)
	cmd.WriteString("------------------------------------------------\n") // 48 dashes
	cmd.Write([]byte{0x1B, 0x2D, 0x00}) // ESC - 0 (cancel underline)
	
	// Print totals with wider spacing for 80mm
	cmd.WriteString(fmt.Sprintf("%-34s $%.2f\n", "Subtotal:", receipt.Subtotal))
	
	if receipt.DiscountPercentage > 0 && receipt.DiscountAmount > 0 {
		cmd.WriteString(fmt.Sprintf("%-34s -$%.2f\n", fmt.Sprintf("Discount (%.0f%%):", receipt.DiscountPercentage), receipt.DiscountAmount))
	}
	
	cmd.WriteString(fmt.Sprintf("%-34s $%.2f\n", "Tax:", receipt.Tax))
	
	// Calculate GST and PST if tax is present
	if receipt.Tax > 0 {
		gst := receipt.Subtotal * 0.05
		pst := receipt.Subtotal * 0.07
		cmd.WriteString(fmt.Sprintf("  %-32s $%.2f\n", "GST (5%):", gst))
		cmd.WriteString(fmt.Sprintf("  %-32s $%.2f\n", "PST (7%):", pst))
	}
	
	if receipt.RefundAmount > 0 {
		cmd.WriteString(fmt.Sprintf("%-34s -$%.2f\n", "Refund:", receipt.RefundAmount))
	}
	
	if receipt.Tip > 0 {
		cmd.WriteString(fmt.Sprintf("%-34s $%.2f\n", "Tip:", receipt.Tip))
	}
	
	// Divider line - full width for 80mm (48 chars)
	cmd.Write([]byte{0x1B, 0x2D, 0x01}) // ESC - 1 (underline)
	cmd.WriteString("------------------------------------------------\n") // 48 dashes
	cmd.Write([]byte{0x1B, 0x2D, 0x00}) // ESC - 0 (cancel underline)
	
	// Print total in bold and larger font
	cmd.Write([]byte{0x1D, 0x21, 0x11}) // GS ! 17 - Double width & height
	cmd.Write([]byte{0x1B, 0x45, 0x01}) // ESC E 1 (bold)
	cmd.WriteString(fmt.Sprintf("TOTAL: $%.2f\n", receipt.Total))
	cmd.Write([]byte{0x1B, 0x45, 0x00}) // ESC E 0 (cancel bold)
	cmd.Write([]byte{0x1D, 0x21, 0x00}) // GS ! 0 - Normal size
	
	// Print cash details if applicable
	if receipt.PaymentType == "cash" && receipt.CashGiven > 0 {
		cmd.WriteString(fmt.Sprintf("%-34s $%.2f\n", "Cash:", receipt.CashGiven))
		cmd.WriteString(fmt.Sprintf("%-34s $%.2f\n", "Change:", receipt.ChangeDue))
	}
	
	// Divider line - full width for 80mm (48 chars)
	cmd.Write([]byte{0x1B, 0x2D, 0x01}) // ESC - 1 (underline)
	cmd.WriteString("------------------------------------------------\n") // 48 dashes
	cmd.Write([]byte{0x1B, 0x2D, 0x00}) // ESC - 0 (cancel underline)
	
	// Center align for footer
	cmd.Write([]byte{0x1B, 0x61, 0x01}) // ESC a 1 - Center alignment
	
	// Print footer
	cmd.WriteString("\nThank you for your purchase!\n")
	cmd.WriteString("Visit us again at " + locationName + "\n\n\n")
	
	// Cut paper
	cmd.Write([]byte{0x1D, 0x56, 0x41, 0x10}) // GS V A 16 (partial cut with feed)
	
	return cmd.Bytes(), nil
}

// generateHTMLReceipt creates a clean HTML receipt that will print nicely
func generateHTMLReceipt(receipt ReceiptData) (string, error) {
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

    // Start building HTML
    html := `<!DOCTYPE html>
<html>
<head>
    <meta charset="utf-8">
    <title>Receipt</title>
    <style>
        body {
            font-family: Arial, sans-serif;
            font-size: 10pt;
            margin: 0;
            padding: 0;
            width: 3in; /* Standard thermal receipt width */
        }
        .receipt {
            width: 100%;
            padding: 5px;
        }
        .header, .footer {
            text-align: center;
            margin-bottom: 10px;
        }
        .store-name {
            font-size: 14pt;
            font-weight: bold;
        }
        .divider {
            border-top: 1px dashed #000;
            margin: 10px 0;
        }
        .item {
            margin-bottom: 8px;
        }
        .item-name {
            font-weight: bold;
        }
        .item-details {
            padding-left: 10px;
        }
        .totals {
            margin-top: 10px;
        }
        .total-line {
            display: flex;
            justify-content: space-between;
        }
        .grand-total {
            font-weight: bold;
            font-size: 12pt;
            margin-top: 10px;
            text-align: center;
            padding: 5px;
            background-color: #f5f5f5;
        }
        @media print {
            @page {
                size: 3in auto;
                margin: 0mm;
            }
            body {
                width: 100%;
            }
        }
    </style>
</head>
<body>
    <div class="receipt">
        <div class="header">
            <div class="store-name">` + locationName + `</div>`

    if receipt.CustomerName != "" {
        html += `
            <div>Customer: ` + receipt.CustomerName + `</div>`
    }

    html += `
            <div>` + receipt.Date + `</div>
        </div>
        
        <div class="divider"></div>
        
        <div>Transaction ID: ` + receipt.TransactionID + `</div>
        <div>Payment: ` + strings.Title(receipt.PaymentType) + `</div>
        
        <div class="divider"></div>
        
        <div style="font-weight: bold;">ITEMS</div>`

    // Add items
    for _, item := range receipt.Items {
        // Format quantity
        var quantityStr string
        if item.Quantity == float64(int(item.Quantity)) {
            quantityStr = fmt.Sprintf("%d", int(item.Quantity))
        } else {
            quantityStr = strings.TrimRight(strings.TrimRight(fmt.Sprintf("%.3f", item.Quantity), "0"), ".")
        }

        html += `
        <div class="item">
            <div class="item-name">` + item.Name + `</div>
            <div class="item-details">
                <div class="total-line">
                    <span>` + quantityStr + ` x $` + fmt.Sprintf("%.2f", item.Price) + `</span>
                    <span>$` + fmt.Sprintf("%.2f", item.Quantity*item.Price) + `</span>
                </div>`

        if item.SKU != "" {
            html += `
                <div>SKU: ` + item.SKU + `</div>`
        }

        html += `
            </div>
        </div>`
    }

    html += `
        <div class="divider"></div>
        
        <div class="totals">
            <div class="total-line">
                <span>Subtotal:</span>
                <span>$` + fmt.Sprintf("%.2f", receipt.Subtotal) + `</span>
            </div>`

    if receipt.DiscountPercentage > 0 && receipt.DiscountAmount > 0 {
        html += `
            <div class="total-line">
                <span>Discount (` + fmt.Sprintf("%.0f", receipt.DiscountPercentage) + `%):</span>
                <span>-$` + fmt.Sprintf("%.2f", receipt.DiscountAmount) + `</span>
            </div>`
    }

    html += `
            <div class="total-line">
                <span>Tax:</span>
                <span>$` + fmt.Sprintf("%.2f", receipt.Tax) + `</span>
            </div>`

    // GST and PST
    if receipt.Tax > 0 {
        gst := receipt.Subtotal * 0.05
        pst := receipt.Subtotal * 0.07
        html += `
            <div style="padding-left: 10px;">
                <div>GST (5%): $` + fmt.Sprintf("%.2f", gst) + `</div>
                <div>PST (7%): $` + fmt.Sprintf("%.2f", pst) + `</div>
            </div>`
    }

    if receipt.RefundAmount > 0 {
        html += `
            <div class="total-line">
                <span>Refund:</span>
                <span>-$` + fmt.Sprintf("%.2f", receipt.RefundAmount) + `</span>
            </div>`
    }

    if receipt.Tip > 0 {
        html += `
            <div class="total-line">
                <span>Tip:</span>
                <span>$` + fmt.Sprintf("%.2f", receipt.Tip) + `</span>
            </div>`
    }

    html += `
        </div>
        
        <div class="grand-total">
            <div class="total-line">
                <span>TOTAL:</span>
                <span>$` + fmt.Sprintf("%.2f", receipt.Total) + `</span>
            </div>
        </div>`

    // Cash payment details
    if receipt.PaymentType == "cash" && receipt.CashGiven > 0 {
        html += `
        <div style="margin-top: 10px; background-color: #f8f8f8; padding: 5px;">
            <div class="total-line">
                <span>Cash:</span>
                <span>$` + fmt.Sprintf("%.2f", receipt.CashGiven) + `</span>
            </div>
            <div class="total-line">
                <span>Change:</span>
                <span>$` + fmt.Sprintf("%.2f", receipt.ChangeDue) + `</span>
            </div>
        </div>`
    }

    html += `
        <div class="divider"></div>
        
        <div class="footer">
            <div>Thank you for your purchase!</div>
            <div>Visit us again at ` + locationName + `</div>
        </div>
    </div>
</body>
</html>`

    return html, nil
}

// printHTMLReceiptHandler creates and prints a receipt using HTML
func printHTMLReceiptHandler(w http.ResponseWriter, r *http.Request, printerName string) {
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
    
    // Set default copies if not specified
    if receipt.Copies <= 0 {
        receipt.Copies = 1
    }
    
    // Generate HTML receipt
    htmlContent, err := generateHTMLReceipt(receipt)
    if err != nil {
        writeJSONError(w, http.StatusInternalServerError, errors.New("error generating HTML receipt"))
        return
    }
    
    // Create a temporary HTML file
    tempFile, err := ioutil.TempFile("", "receipt-*.html")
    if err != nil {
        writeJSONError(w, http.StatusInternalServerError, errors.New("error creating temporary file"))
        return
    }
    
    tempFilePath := tempFile.Name()
    defer os.Remove(tempFilePath)
    
    // Write the HTML content to the file
    if _, err := tempFile.WriteString(htmlContent); err != nil {
        writeJSONError(w, http.StatusInternalServerError, errors.New("error writing to temporary file"))
        return
    }
    
    // Close the file
    if err := tempFile.Close(); err != nil {
        writeJSONError(w, http.StatusInternalServerError, errors.New("error closing temporary file"))
        return
    }
    
    // Print the HTML file
    successCount := 0
    for i := 0; i < receipt.Copies; i++ {
        var cmd *exec.Cmd
        
        if runtime.GOOS == "windows" {
            // On Windows, use the default browser to print silently
            cmd = exec.Command("powershell", "-Command", 
                fmt.Sprintf("Start-Process \"$env:SystemRoot\\System32\\rundll32.exe\" -ArgumentList \"mshtml.dll,PrintHTML '%s'\" -WindowStyle Hidden", tempFilePath))
        } else if runtime.GOOS == "darwin" {
            // On macOS
            cmd = exec.Command("lp", "-d", printerName, tempFilePath)
        } else {
            // On Linux
            cmd = exec.Command("lp", "-d", printerName, tempFilePath)
        }
        
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
	
	// Receipt printing endpoint - pass the printer name to the handler
	mux.HandleFunc("/print/receipt", func(w http.ResponseWriter, r *http.Request) {
		printHTMLReceiptHandler(w, r, *printerNameFlag)
	})
	
	log.Printf("Starting server on http://localhost:%d", *httpPortFlag)
	log.Printf("Scanner endpoint: http://localhost:%d/scanner/scan", *httpPortFlag)
	log.Printf("Receipt printer endpoint: http://localhost:%d/print/receipt", *httpPortFlag)
	log.Printf("HTML receipt printer endpoint: http://localhost:%d/print/html-receipt", *httpPortFlag)
	
	if err := http.ListenAndServe(fmt.Sprintf(":%d", *httpPortFlag), corsMiddleware(mux)); err != nil {
		log.Fatal(err)
	}
}
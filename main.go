package main

import (
	"encoding/hex"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"log"
	"net/http"
	"regexp"
	"runtime"
	"strconv"
	"strings"
	"time"

	"go.bug.st/serial"
)

// SerialResponse represents the response sent to the frontend
type SerialResponse struct {
	Status    string `json:"status"`
	RawData   string `json:"rawData"`
	RawHex    string `json:"rawHex"`
	Message   string `json:"message,omitempty"`
	Timestamp string `json:"timestamp"`
	
	// Parsed license data - populated if parsing is successful
	ParsedData *LicenseData `json:"parsedData,omitempty"`
}

// LicenseData represents the structured data from a parsed license
type LicenseData struct {
	FirstName     string `json:"firstName"`
	MiddleName    string `json:"middleName,omitempty"`
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
	DOB           string `json:"dob"`
	Weight        string `json:"weight,omitempty"`
	HairColor     string `json:"hairColor,omitempty"`
	EyeColor      string `json:"eyeColor,omitempty"`
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

func writeJSONError(w http.ResponseWriter, status int, err error) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	json.NewEncoder(w).Encode(map[string]string{
		"status":  "error",
		"message": err.Error(),
	})
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
	
	// Always use 1200 baud for COM4
	if strings.ToUpper(portName) == "COM4" {
		mode = &serial.Mode{
			BaudRate:   1200,
			DataBits: 8,
			Parity:   serial.NoParity,
			StopBits: serial.OneStopBit,
		}
		fmt.Println("Using COM4 settings: BaudRate=1200, DataBits=8")
	} else if useMacSettings {
		// Use settings from the Mac version for other ports
		mode = &serial.Mode{
			BaudRate: 9600,
			DataBits: 8,
			Parity:   serial.NoParity,
			StopBits: serial.OneStopBit,
		}
		fmt.Println("Using Mac settings: BaudRate=9600, DataBits=8")
	} else {
		// Default Windows settings for other ports
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

	var responseBuffer strings.Builder
	maxWaitTime := 3 * time.Second  // Maximum overall wait time (reduced from 5)
	maxDataWaitTime := 1 * time.Second // Maximum wait time after receiving data (reduced from 3)
	deadline := time.Now().Add(maxWaitTime)
	tmp := make([]byte, 128)

	fmt.Printf("Waiting for response... (timeout: %v, max wait: %v, max data wait: %v)\n", 
		readTimeout, maxWaitTime, maxDataWaitTime)
	fmt.Println("PLEASE SCAN YOUR LICENSE NOW - You have 5 seconds") // Reduced from 10
	
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
		
		// Add the received bytes to our response buffer
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
	fmt.Printf("Raw response (hex): %s\n", hex.EncodeToString([]byte(result)))
	fmt.Printf("Raw response (string): %q\n", result)
	fmt.Println("===== END RESPONSE =====")
	
	return result, nil
}

// parseCanadianLicense parses Canadian driver's license data
func parseCanadianLicense(raw string) *LicenseData {
	log.Println("Parsing Canadian license data")
	
	// Create a new license data struct
	license := &LicenseData{
		LicenseClass: "NA", // Default license class
	}
	
	// Remove any NAK (0x15) or control characters from the beginning
	raw = strings.Replace(raw, "\x15", "", 1)
	
	// Clean up the data
	raw = strings.Replace(raw, "\r", "", -1)
	raw = strings.Replace(raw, "\n", "", -1)
	
	// Extract license number - looking for pattern: ;(\d+)=
	licenseNumberMatch := regexp.MustCompile(`;(\d+)=`).FindStringSubmatch(raw)
	if len(licenseNumberMatch) > 1 {
		fullLicenseNumber := licenseNumberMatch[1]
		
		// BC pattern is often that the last 7 digits are the actual license number
		if len(fullLicenseNumber) > 7 {
			license.LicenseNumber = fullLicenseNumber[len(fullLicenseNumber)-7:]
		} else {
			license.LicenseNumber = fullLicenseNumber
		}
		
		log.Println("Found license number:", license.LicenseNumber)
	}
	
	// Determine province format
	province := "UNKNOWN"
	
	// Check for province identifiers in the data
	if strings.Contains(raw, "%BC") {
		province = "BC"
	} else if strings.Contains(raw, "%AB") {
		province = "AB"
	} else if strings.Contains(raw, "%ON") {
		province = "ON"
	} else if strings.Contains(raw, "%QC") {
		province = "QC"
	} else if strings.Contains(raw, "%MB") {
		province = "MB"
	} else if strings.Contains(raw, "%SK") {
		province = "SK"
	} else if strings.Contains(raw, "%NS") {
		province = "NS"
	} else if strings.Contains(raw, "%NB") {
		province = "NB"
	} else if strings.Contains(raw, "%PE") {
		province = "PE"
	} else if strings.Contains(raw, "%NL") {
		province = "NL"
	} else if strings.Contains(raw, "%YT") {
		province = "YT"
	} else if strings.Contains(raw, "%NT") {
		province = "NT"
	} else if strings.Contains(raw, "%NU") {
		province = "NU"
	}
	
	license.State = province
	log.Printf("Detected province: %s", province)
	
	// Split by carets (^) - common in many Canadian DL formats
	parts := strings.Split(raw, "^")
	
	// Extract city from first part (after %BC)
	if len(parts) >= 1 && strings.Contains(parts[0], "%BC") {
		cityPart := strings.Replace(parts[0], "%BC", "", 1)
		license.City = strings.TrimSpace(cityPart)
		log.Println("Found city:", license.City)
	}
	
	// Extract name from second part
	if len(parts) >= 2 {
		nameParts := strings.Split(parts[1], ",")
		if len(nameParts) >= 2 {
			// Last name is before the comma
			license.LastName = strings.TrimSpace(strings.Replace(nameParts[0], "$", "", 1))
			
			// First name and middle name after the comma
			fullNamePart := strings.TrimSpace(nameParts[1])
			// Remove the $ at the beginning if present
			fullNamePart = strings.Replace(fullNamePart, "$", "", 1)
			
			// Check for middle name (split on space)
			firstNameParts := strings.Split(fullNamePart, " ")
			if len(firstNameParts) > 1 {
				license.FirstName = strings.TrimSpace(firstNameParts[0])
				license.MiddleName = strings.TrimSpace(strings.Join(firstNameParts[1:], " "))
			} else {
				license.FirstName = fullNamePart
			}
			
			log.Printf("Found name: %s %s %s", license.FirstName, license.MiddleName, license.LastName)
		}
	}
	
	// Extract address, province, postal code from third part
	if len(parts) >= 3 {
		addressPart := parts[2]
		
		// Check if the address has a $ separator
		if strings.Contains(addressPart, "$") {
			addressParts := strings.Split(addressPart, "$")
			
			// First part is the street address
			if len(addressParts) >= 1 {
				license.Address = strings.TrimSpace(addressParts[0])
				log.Println("Found address:", license.Address)
			}
			
			// Second part might contain city, province, postal code
			if len(addressParts) >= 2 {
				addressLine2 := strings.TrimSpace(addressParts[1])
				
				// Extract postal code - Look for pattern like V1W 5B8 or V1W5B8
				postalMatch := regexp.MustCompile(`([A-Z]\d[A-Z])\s*(\d[A-Z]\d)`).FindStringSubmatch(addressLine2)
				if len(postalMatch) > 2 {
					license.Postal = postalMatch[1] + postalMatch[2]
					log.Println("Found postal code from address line 2:", license.Postal)
				}
				
				// If we still don't have a postal code, look elsewhere in the raw data
				if license.Postal == "" {
					// Try other patterns
					bcPostalMatch := regexp.MustCompile(`\b(V\d[A-Z]\d[A-Z]\d)\b`).FindStringSubmatch(raw)
					if len(bcPostalMatch) > 1 {
						license.Postal = bcPostalMatch[1]
						log.Println("Found BC postal code:", license.Postal)
					}
				}
			}
		} else {
			// If no $ separator, just use the whole part as address
			license.Address = strings.TrimSpace(addressPart)
			log.Println("Found address (no separator):", license.Address)
		}
	}
	
	// Extract dates - Look for pattern like =DDMMYYYYMMDD=
	dateMatch := regexp.MustCompile(`=(\d{12})=`).FindStringSubmatch(raw)
	if len(dateMatch) > 1 && len(dateMatch[1]) == 12 {
		dateStr := dateMatch[1]
		
		// First 6 digits are expiry date (DDMMYY)
		expiryDay := dateStr[0:2]
		expiryMonth := dateStr[2:4]
		expiryYear := dateStr[4:6]
		
		// Last 6 digits are birth date (YYMMDD)
		birthYear := dateStr[6:8]
		birthMonth := dateStr[8:10]
		birthDay := dateStr[10:12]
		
		// Add century for birth year
		fullBirthYear := ""
		birthYearNum, _ := strconv.Atoi(birthYear)
		currentYear := time.Now().Year() % 100
		if birthYearNum > currentYear {
			fullBirthYear = "19" + birthYear
		} else {
			fullBirthYear = "20" + birthYear
		}
		
		// Format birth date
		license.DOB = fmt.Sprintf("%s%s%s", fullBirthYear, birthMonth, birthDay)
		log.Println("Found DOB:", license.DOB)
		
		// Format expiry date
		license.ExpiryDate = fmt.Sprintf("20%s-%s-%s", expiryYear, expiryMonth, expiryDay)
		log.Println("Found expiry date:", license.ExpiryDate)
		
		// Try to find issue date by looking for a pattern in the raw data
		issueMatch := regexp.MustCompile(`(\d{4})-(\d{2})-(\d{2})`).FindStringSubmatch(raw)
		if len(issueMatch) > 3 {
			issueYear := issueMatch[1]
			issueMonth := issueMatch[2]
			issueDay := issueMatch[3]
			license.IssueDate = fmt.Sprintf("%s-%s-%s", issueYear, issueMonth, issueDay)
			log.Println("Found issue date from pattern:", license.IssueDate)
		} else if license.ExpiryDate != "" {
			// If we couldn't find an issue date pattern, calculate it from expiry date
			// (typically 5 years earlier for standard licenses)
			expiryParts := strings.Split(license.ExpiryDate, "-")
			if len(expiryParts) == 3 {
				expiryYearNum, _ := strconv.Atoi(expiryParts[0])
				issueYear := expiryYearNum - 5
				license.IssueDate = fmt.Sprintf("%d-%s-%s", issueYear, expiryParts[1], expiryParts[2])
				log.Println("Calculated issue date:", license.IssueDate)
			}
		}
	}
	
	// Extract physical details - look for pattern after the main data
	// Pattern often includes height, weight, eye color
	// Example: F168 57BLOBLU
	physicalMatch := regexp.MustCompile(`([MF])(\d{3})\s+(\d{2})([A-Z]{3})([A-Z]{3})`).FindStringSubmatch(raw)
	if len(physicalMatch) > 5 {
		license.Sex = physicalMatch[1]    // M or F
		license.Height = physicalMatch[2] // Height in cm
		license.Weight = physicalMatch[3] // Weight in kg
		license.HairColor = physicalMatch[4] // Hair color code
		license.EyeColor = physicalMatch[5]  // Eye color code
		
		log.Printf("Found physical details - Sex: %s, Height: %s, Weight: %s, Hair: %s, Eyes: %s", 
			license.Sex, license.Height, license.Weight, license.HairColor, license.EyeColor)
	} else {
		// Try alternative pattern for just sex and height
		altPhysicalMatch := regexp.MustCompile(`([MF])(\d{3})`).FindStringSubmatch(raw)
		if len(altPhysicalMatch) > 2 {
			license.Sex = altPhysicalMatch[1]
			license.Height = altPhysicalMatch[2]
			log.Printf("Found alternative physical details - Sex: %s, Height: %s", license.Sex, license.Height)
		}
	}
	
	// Extract license class - common pattern is a digit or letter preceded by keyword
	classMatch := regexp.MustCompile(`(?i)Class:\s*([A-Z0-9]+)`).FindStringSubmatch(raw)
	if len(classMatch) > 1 {
		license.LicenseClass = strings.TrimSpace(classMatch[1])
		log.Println("Found license class:", license.LicenseClass)
	} else {
		// Try to find a standalone digit or letter that could be the class
		// Common classes are 5, 7, G, M, etc.
		simpleClassMatch := regexp.MustCompile(`\b([1-7ABCDEFGM])\b`).FindStringSubmatch(raw)
		if len(simpleClassMatch) > 1 {
			license.LicenseClass = simpleClassMatch[1]
			log.Println("Found simple license class:", license.LicenseClass)
		}
	}
	
	return license
}

// parseAAMVALicense parses AAMVA-standard driver's license data
func parseAAMVALicense(raw string) *LicenseData {
	log.Println("Parsing AAMVA license data")
	
	// Create a new license data struct
	license := &LicenseData{
		LicenseClass: "NA", // Default license class
	}
	
	// Remove any NAK (0x15) character from the beginning
	raw = strings.Replace(raw, "\x15", "", 1)
	
	// Split into lines
	lines := strings.Split(raw, "\n")
	
	// Process each line based on AAMVA standard
	for _, line := range lines {
		line = strings.TrimSpace(line)
		if line == "" {
			continue
		}
		
		// Extract field based on prefix
		if len(line) >= 3 {
			prefix := line[0:3]
			value := strings.TrimSpace(line[3:])
			
			switch prefix {
			case "DCS":
				license.LastName = value
			case "DAC":
				license.FirstName = value
			case "DAD":
				license.MiddleName = value
			case "DBA":
				if len(value) >= 8 {
					year := value[0:4]
					month := value[4:6]
					day := value[6:8]
					license.ExpiryDate = fmt.Sprintf("%s-%s-%s", year, month, day)
				}
			case "DBD":
				if len(value) >= 8 {
					year := value[0:4]
					month := value[4:6]
					day := value[6:8]
					license.IssueDate = fmt.Sprintf("%s-%s-%s", year, month, day)
				}
			case "DBB":
				if len(value) >= 8 {
					year := value[0:4]
					month := value[4:6]
					day := value[6:8]
					license.DOB = fmt.Sprintf("%s%s%s", year, month, day)
				}
			case "DBC":
				if value == "1" {
					license.Sex = "M"
				} else if value == "2" {
					license.Sex = "F"
				} else {
					license.Sex = value
				}
			case "DAU":
				license.Height = strings.Replace(value, " ", "", -1)
			case "DAG":
				license.Address = value
			case "DAI":
				license.City = value
			case "DAJ":
				license.State = value
			case "DAK":
				license.Postal = value
			case "DAQ":
				if len(value) == 15 {
					license.LicenseNumber = fmt.Sprintf("%s-%s-%s", 
						value[0:5], value[5:10], value[10:15])
				} else {
					license.LicenseNumber = value
				}
			}
		}
		
		// Check for license class
		if strings.Contains(line, "DCAG") {
			classMatch := regexp.MustCompile(`DCAG(\w+)`).FindStringSubmatch(line)
			if len(classMatch) > 1 {
				license.LicenseClass = classMatch[1]
			}
		}
	}
	
	return license
}

// parseLicenseData parses the raw data into structured license data
func parseLicenseData(raw string) *LicenseData {
	// Remove any NAK (0x15) character from the beginning for format detection
	cleanRaw := strings.Replace(raw, "\x15", "", 1)
	
	var license *LicenseData
	
	// Determine the format of the license data
	if strings.Contains(cleanRaw, "%BC") || 
	   strings.Contains(cleanRaw, "%AB") || 
	   strings.Contains(cleanRaw, "%ON") || 
	   strings.Contains(cleanRaw, "%QC") || 
	   strings.Contains(cleanRaw, "%MB") || 
	   strings.Contains(cleanRaw, "%SK") || 
	   strings.Contains(cleanRaw, "%NS") || 
	   strings.Contains(cleanRaw, "%NB") || 
	   strings.Contains(cleanRaw, "%PE") || 
	   strings.Contains(cleanRaw, "%NL") || 
	   strings.Contains(cleanRaw, "%YT") || 
	   strings.Contains(cleanRaw, "%NT") || 
	   strings.Contains(cleanRaw, "%NU") {
		// This is a Canadian driver's license
		license = parseCanadianLicense(raw)
	} else if strings.Contains(cleanRaw, "ANSI ") {
		// This is an AAMVA format license
		license = parseAAMVALicense(raw)
	} else if strings.Contains(cleanRaw, "DCS") || strings.Contains(cleanRaw, "DAQ") {
		// This is likely an AAMVA format license
		license = parseAAMVALicense(raw)
	} else {
		// Try Canadian format first
		license = parseCanadianLicense(raw)
		
		// If we couldn't extract basic info, try AAMVA as a fallback
		if license.FirstName == "" && license.LastName == "" && license.LicenseNumber == "" {
			license = parseAAMVALicense(raw)
		}
	}
	
	// Post-process data for consistency
	if license.Height != "" && !strings.Contains(license.Height, "cm") {
		license.Height = license.Height + "cm"
	}
	
	return license
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

	// Create the response object with raw data
	response := SerialResponse{
		Status:    "success",
		RawData:   result,
		RawHex:    hex.EncodeToString([]byte(result)),
		Timestamp: time.Now().Format(time.RFC3339),
	}
	
	// Try to parse the license data
	parsedData := parseLicenseData(result)
	if parsedData != nil {
		// Only include parsed data if we successfully extracted some fields
		if parsedData.FirstName != "" || parsedData.LastName != "" || 
		   parsedData.Address != "" || parsedData.LicenseNumber != "" {
			response.ParsedData = parsedData
			fmt.Println("Successfully parsed license data")
		} else {
			fmt.Println("Failed to extract meaningful license data")
		}
	}

	// Send the response as JSON
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(response)
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
	
	// Scanner endpoint - now just passes raw data
	mux.HandleFunc("/scanner/scan", func(w http.ResponseWriter, r *http.Request) {
		scannerHandler(w, r, *portFlag, *scannerPortFlag, *useSimpleCommandFlag, *useMacSettingsFlag, readTimeout)
	})
	
	log.Printf("Starting server on http://localhost:%d", *httpPortFlag)
	log.Printf("Scanner endpoint: http://localhost:%d/scanner/scan", *httpPortFlag)
	
	if err := http.ListenAndServe(fmt.Sprintf(":%d", *httpPortFlag), corsMiddleware(mux)); err != nil {
		log.Fatal(err)
	}
}
package main

import (
	"encoding/json"
	"fmt"
	"log"
	"net"
	"net/http"
	"os"
	"strconv"
	"strings"
	"time"
)

// Configuration
type Config struct {
	Port        int    `json:"port"`
	PrinterIP   string `json:"printer_ip"`
	PrinterPort int    `json:"printer_port"`
}

// Receipt item structure
type ReceiptItem struct {
	Name     string  `json:"name"`
	Quantity int     `json:"quantity"`
	Price    float64 `json:"price"`
	SKU      string  `json:"sku"`
}

// Receipt data structure matching your React frontend
type ReceiptData struct {
	TransactionID    string        `json:"transactionId"`
	Items           []ReceiptItem `json:"items"`
	Subtotal        float64       `json:"subtotal"`
	Tax             float64       `json:"tax"`
	Total           float64       `json:"total"`
	Tip             float64       `json:"tip"`
	PaymentType     string        `json:"paymentType"`
	CustomerName    string        `json:"customerName"`
	Date            string        `json:"date"`
	Location        string        `json:"location"`
	Copies          int           `json:"copies"`
	CashGiven       float64       `json:"cashGiven"`
	ChangeDue       float64       `json:"changeDue"`
	DiscountAmount  float64       `json:"discountAmount"`
	RefundAmount    float64       `json:"refundAmount"`
}

// Response structure
type PrintResponse struct {
	Success bool   `json:"success"`
	Message string `json:"message"`
}

// Global configuration
var config Config

// CORS middleware
func enableCORS(w http.ResponseWriter) {
	w.Header().Set("Access-Control-Allow-Origin", "*")
	w.Header().Set("Access-Control-Allow-Methods", "GET, POST, OPTIONS")
	w.Header().Set("Access-Control-Allow-Headers", "Content-Type, Authorization")
}

// Log with timestamp
func logMessage(message string) {
	timestamp := time.Now().Format("2006-01-02 15:04:05")
	log.Printf("%s - %s", timestamp, message)
}

// Send data to printer via TCP
func sendToPrinter(data string, copies int) error {
	for i := 1; i <= copies; i++ {
		// Connect to printer
		conn, err := net.DialTimeout("tcp", fmt.Sprintf("%s:%d", config.PrinterIP, config.PrinterPort), 5*time.Second)
		if err != nil {
			return fmt.Errorf("failed to connect to printer: %v", err)
		}
		defer conn.Close()

		// Set write timeout
		conn.SetWriteDeadline(time.Now().Add(10 * time.Second))

		// Send data
		_, err = conn.Write([]byte(data))
		if err != nil {
			return fmt.Errorf("failed to send data to printer: %v", err)
		}

		logMessage(fmt.Sprintf("✓ Copy %d sent to printer successfully", i))

		// Small delay between copies
		if i < copies {
			time.Sleep(1 * time.Second)
		}
	}
	return nil
}

// Format receipt for thermal printer (ESC/POS commands)
func formatReceipt(receipt ReceiptData) string {
	var builder strings.Builder

	// ESC/POS commands
	ESC := string(rune(27))
	GS := string(rune(29))

	// Reset printer
	builder.WriteString(ESC + "@")

	// Header - centered
	builder.WriteString(ESC + "a" + string(rune(1))) // Center alignment
	builder.WriteString("          RECEIPT          \n")
	builder.WriteString(ESC + "a" + string(rune(0))) // Left alignment

	builder.WriteString("================================\n")

	// Store info
	location := receipt.Location
	if location == "" {
		location = "Store"
	}
	builder.WriteString(fmt.Sprintf("Location: %s\n", location))

	date := receipt.Date
	if date == "" {
		date = time.Now().Format("2006-01-02 15:04:05")
	}
	builder.WriteString(fmt.Sprintf("Date: %s\n", date))

	builder.WriteString(fmt.Sprintf("Transaction: %s\n", receipt.TransactionID))

	if receipt.CustomerName != "" {
		builder.WriteString(fmt.Sprintf("Customer: %s\n", receipt.CustomerName))
	}

	builder.WriteString("================================\n\n")

	// Items
	builder.WriteString("Items:\n")
	for _, item := range receipt.Items {
		itemTotal := float64(item.Quantity) * item.Price
		builder.WriteString(fmt.Sprintf("%s\n", item.Name))
		builder.WriteString(fmt.Sprintf("  %d x $%.2f = $%.2f\n", item.Quantity, item.Price, itemTotal))
		if item.SKU != "" {
			builder.WriteString(fmt.Sprintf("  SKU: %s\n", item.SKU))
		}
	}

	builder.WriteString("\n================================\n")

	// Totals
	builder.WriteString(fmt.Sprintf("Subtotal: $%.2f\n", receipt.Subtotal))

	if receipt.DiscountAmount > 0 {
		builder.WriteString(fmt.Sprintf("Discount: -$%.2f\n", receipt.DiscountAmount))
	}

	builder.WriteString(fmt.Sprintf("Tax: $%.2f\n", receipt.Tax))

	if receipt.Tip > 0 {
		builder.WriteString(fmt.Sprintf("Tip: $%.2f\n", receipt.Tip))
	}

	// Bold total
	builder.WriteString(ESC + "E" + string(rune(1))) // Bold on
	builder.WriteString(fmt.Sprintf("Total: $%.2f", receipt.Total))
	builder.WriteString(ESC + "E" + string(rune(0))) // Bold off
	builder.WriteString("\n")

	builder.WriteString(fmt.Sprintf("Payment: %s\n", receipt.PaymentType))

	// Cash details if applicable
	if receipt.PaymentType == "cash" && receipt.CashGiven > 0 {
		builder.WriteString(fmt.Sprintf("Cash: $%.2f\n", receipt.CashGiven))
		builder.WriteString(fmt.Sprintf("Change: $%.2f\n", receipt.ChangeDue))
	}

	// Refund if applicable
	if receipt.RefundAmount > 0 {
		builder.WriteString(fmt.Sprintf("Refund: $%.2f\n", receipt.RefundAmount))
	}

	builder.WriteString("================================\n\n")

	// Footer - centered
	builder.WriteString(ESC + "a" + string(rune(1))) // Center alignment
	builder.WriteString("Thank you for your purchase!\n")
	builder.WriteString(ESC + "a" + string(rune(0))) // Left alignment

	// Line feeds and cut
	builder.WriteString(ESC + string(rune(100)) + string(rune(3))) // Feed 3 lines
	builder.WriteString("\n\n\n")
	builder.WriteString(GS + string(rune(86)) + string(rune(66)) + string(rune(0))) // Full cut

	return builder.String()
}

// Test printer connection
func testPrinter() error {
	logMessage("Testing printer connection...")
	fmt.Printf("Printer: %s:%d\n", config.PrinterIP, config.PrinterPort)

	// Test connection
	conn, err := net.DialTimeout("tcp", fmt.Sprintf("%s:%d", config.PrinterIP, config.PrinterPort), 5*time.Second)
	if err != nil {
		return fmt.Errorf("cannot reach printer: %v", err)
	}
	defer conn.Close()

	fmt.Println("✅ Printer is reachable")

	// Send test print
	fmt.Println("Sending test print...")
	testReceipt := string(rune(27)) + "@\n" +
		string(rune(27)) + "a" + string(rune(1)) + "TEST PRINT" + string(rune(27)) + "a" + string(rune(0)) + "\n" +
		"================================\n" +
		"Date: " + time.Now().Format("2006-01-02 15:04:05") + "\n" +
		"Test from Go print server\n" +
		"================================\n" +
		string(rune(27)) + string(rune(100)) + string(rune(3)) + "\n" +
		string(rune(29)) + string(rune(86)) + string(rune(66)) + string(rune(0))

	conn.SetWriteDeadline(time.Now().Add(5 * time.Second))
	_, err = conn.Write([]byte(testReceipt))
	if err != nil {
		return fmt.Errorf("failed to send test print: %v", err)
	}

	fmt.Println("✅ Test print sent successfully")
	return nil
}

// Handle print receipt endpoint
func handlePrintReceipt(w http.ResponseWriter, r *http.Request) {
	enableCORS(w)

	if r.Method == "OPTIONS" {
		w.WriteHeader(http.StatusOK)
		return
	}

	if r.Method != "POST" {
		w.WriteHeader(http.StatusMethodNotAllowed)
		json.NewEncoder(w).Encode(PrintResponse{
			Success: false,
			Message: "Method not allowed",
		})
		return
	}

	// Parse JSON body
	var receipt ReceiptData
	err := json.NewDecoder(r.Body).Decode(&receipt)
	if err != nil {
		logMessage(fmt.Sprintf("❌ Error parsing JSON: %v", err))
		w.WriteHeader(http.StatusBadRequest)
		json.NewEncoder(w).Encode(PrintResponse{
			Success: false,
			Message: "Invalid JSON data",
		})
		return
	}

	logMessage("📄 Received print request")

	// Set default copies if not specified
	if receipt.Copies <= 0 {
		receipt.Copies = 1
	}

	// Format receipt
	formattedReceipt := formatReceipt(receipt)

	// Send to printer
	err = sendToPrinter(formattedReceipt, receipt.Copies)
	if err != nil {
		logMessage(fmt.Sprintf("❌ Print job failed: %v", err))
		w.WriteHeader(http.StatusInternalServerError)
		json.NewEncoder(w).Encode(PrintResponse{
			Success: false,
			Message: fmt.Sprintf("Failed to print receipt: %v", err),
		})
		return
	}

	logMessage("✅ Print job completed successfully")
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(PrintResponse{
		Success: true,
		Message: fmt.Sprintf("Receipt printed successfully (%d %s)", receipt.Copies, 
			map[bool]string{true: "copy", false: "copies"}[receipt.Copies == 1]),
	})
}

// Handle health check
func handleHealth(w http.ResponseWriter, r *http.Request) {
	enableCORS(w)
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(map[string]string{
		"status": "ok",
		"printer": fmt.Sprintf("%s:%d", config.PrinterIP, config.PrinterPort),
	})
}

// Show usage information
func showUsage() {
	fmt.Println("Receipt Print Server for Go")
	fmt.Println("Usage: go run main.go [options]")
	fmt.Println("")
	fmt.Println("Options:")
	fmt.Println("  -port PORT            Set server port (default: 3600)")
	fmt.Println("  -printer-ip IP        Set printer IP address (default: 192.168.1.100)")
	fmt.Println("  -printer-port PORT    Set printer port (default: 9100)")
	fmt.Println("  -test                 Test printer connection")
	fmt.Println("  -help                 Show this help message")
	fmt.Println("")
	fmt.Println("Examples:")
	fmt.Println("  go run main.go                                      # Start with default settings")
	fmt.Println("  go run main.go -port 8080 -printer-ip 192.168.1.50 # Custom port and printer IP")
	fmt.Println("  go run main.go -test                               # Test printer connection")
}

func main() {
	// Default configuration
	config = Config{
		Port:        3600,
		PrinterIP:   "192.168.1.100",
		PrinterPort: 9100,
	}

	// Parse command line arguments
	args := os.Args[1:]
	for i := 0; i < len(args); i++ {
		switch args[i] {
		case "-port":
			if i+1 < len(args) {
				port, err := strconv.Atoi(args[i+1])
				if err != nil {
					fmt.Printf("Invalid port: %s\n", args[i+1])
					os.Exit(1)
				}
				config.Port = port
				i++
			}
		case "-printer-ip":
			if i+1 < len(args) {
				config.PrinterIP = args[i+1]
				i++
			}
		case "-printer-port":
			if i+1 < len(args) {
				port, err := strconv.Atoi(args[i+1])
				if err != nil {
					fmt.Printf("Invalid printer port: %s\n", args[i+1])
					os.Exit(1)
				}
				config.PrinterPort = port
				i++
			}
		case "-test":
			if err := testPrinter(); err != nil {
				fmt.Printf("❌ Printer test failed: %v\n", err)
				os.Exit(1)
			}
			return
		case "-help":
			showUsage()
			return
		default:
			fmt.Printf("Unknown option: %s\n", args[i])
			showUsage()
			os.Exit(1)
		}
	}

	logMessage(fmt.Sprintf("🚀 Starting receipt print server on port %d", config.Port))
	logMessage(fmt.Sprintf("🖨️  Printer configured: %s:%d", config.PrinterIP, config.PrinterPort))

	fmt.Printf("Receipt Print Server Starting...\n")
	fmt.Printf("Listening on: http://localhost:%d\n", config.Port)
	fmt.Printf("Printer: %s:%d\n", config.PrinterIP, config.PrinterPort)
	fmt.Printf("Press Ctrl+C to stop\n\n")

	// Test printer connectivity
	conn, err := net.DialTimeout("tcp", fmt.Sprintf("%s:%d", config.PrinterIP, config.PrinterPort), 2*time.Second)
	if err != nil {
		logMessage(fmt.Sprintf("⚠️  Warning: Cannot reach printer at %s:%d", config.PrinterIP, config.PrinterPort))
	} else {
		conn.Close()
		logMessage("✅ Printer connection test successful")
	}

	// Setup routes
	http.HandleFunc("/print/receipt", handlePrintReceipt)
	http.HandleFunc("/health", handleHealth)

	// Start server
	address := fmt.Sprintf(":%d", config.Port)
	logMessage(fmt.Sprintf("Server listening on %s", address))

	if err := http.ListenAndServe(address, nil); err != nil {
		log.Fatal("Server failed to start:", err)
	}
}
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
	TransactionID           string        `json:"transactionId"`
	Items                  []ReceiptItem `json:"items"`
	Subtotal               float64       `json:"subtotal"`
	Tax                    float64       `json:"tax"`
	Total                  float64       `json:"total"`
	Tip                    float64       `json:"tip"`
	PaymentType            string        `json:"paymentType"`
	CustomerName           string        `json:"customerName"`
	Date                   string        `json:"date"`
	Location               string        `json:"location"`
	Copies                 int           `json:"copies"`
	CashGiven              float64       `json:"cashGiven"`
	ChangeDue              float64       `json:"changeDue"`
	DiscountAmount         float64       `json:"discountAmount"`
	DiscountPercentage     float64       `json:"discountPercentage"`
	PromoAmount            float64       `json:"promoAmount"`
	RefundAmount           float64       `json:"refundAmount"`
	TerminalId             string        `json:"terminalId"`
	AccountId              string        `json:"accountId"`
	AccountName            string        `json:"accountName"`
	AccountBalanceBefore   float64       `json:"accountBalanceBefore"`
	AccountBalanceAfter    float64       `json:"accountBalanceAfter"`
	SettlementAmount       float64       `json:"settlementAmount"`
	IsSettlement           bool          `json:"isSettlement"`
	IsRetail               bool          `json:"isRetail"`
	HasCombinedTransaction bool          `json:"hasCombinedTransaction"`
	SkipTaxCalculation     bool          `json:"skipTaxCalculation"`
	HasNoTax               bool          `json:"hasNoTax"`
	LogoUrl                string        `json:"logoUrl"`
	CardDetails            CardDetails   `json:"cardDetails"`
}

// Card details structure
type CardDetails struct {
	CardBrand string `json:"cardBrand"`
	CardLast4 string `json:"cardLast4"`
	AuthCode  string `json:"authCode"`
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
		// Try to resolve printer name to IP if it's not an IP address
		printerAddress := config.PrinterIP
		if !strings.Contains(printerAddress, ".") {
			// Try to resolve hostname/printer name
			ips, err := net.LookupIP(printerAddress)
			if err != nil {
				return fmt.Errorf("failed to resolve printer name '%s': %v", printerAddress, err)
			}
			if len(ips) > 0 {
				printerAddress = ips[0].String()
				logMessage(fmt.Sprintf("Resolved %s to %s", config.PrinterIP, printerAddress))
			}
		}
		
		// Connect to printer
		conn, err := net.DialTimeout("tcp", fmt.Sprintf("%s:%d", printerAddress, config.PrinterPort), 5*time.Second)
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

	// Header - centered and bold
	builder.WriteString(ESC + "a" + string(rune(1))) // Center alignment
	builder.WriteString(ESC + "E" + string(rune(1))) // Bold on
	
	location := receipt.Location
	if location == "" {
		location = "Store"
	}
	builder.WriteString(fmt.Sprintf("%s\n", location))
	builder.WriteString(ESC + "E" + string(rune(0))) // Bold off

	// Date - centered
	date := receipt.Date
	if date == "" {
		date = time.Now().Format("2006-01-02 15:04:05")
	}
	// Remove seconds from date for cleaner look
	if len(date) > 16 {
		date = date[:16]
	}
	builder.WriteString(fmt.Sprintf("%s\n", date))

	// Customer name if present
	if receipt.CustomerName != "" {
		builder.WriteString(fmt.Sprintf("Customer: %s\n", receipt.CustomerName))
	}

	builder.WriteString(ESC + "a" + string(rune(0))) // Left alignment
	builder.WriteString("================================\n")

	// Transaction type indicators (matching React component)
	if receipt.IsSettlement {
		builder.WriteString("✓ Account Settlement Transaction\n")
	} else if receipt.IsRetail {
		builder.WriteString("✓ Retail Transaction\n")
	} else if receipt.HasCombinedTransaction {
		builder.WriteString("✓ Combined Retail & Settlement\n")
	}

	// Items section
	builder.WriteString("\nITEMS\n")
	for _, item := range receipt.Items {
		itemTotal := float64(item.Quantity) * item.Price
		
		// Item name (bold)
		builder.WriteString(ESC + "E" + string(rune(1)))
		builder.WriteString(fmt.Sprintf("%s\n", item.Name))
		builder.WriteString(ESC + "E" + string(rune(0)))
		
		// Quantity and price details with right alignment
		builder.WriteString(fmt.Sprintf("  %d x $%.2f", item.Quantity, item.Price))
		
		// Right-align the total
		totalStr := fmt.Sprintf("$%.2f", itemTotal)
		padding := 32 - len(fmt.Sprintf("  %d x $%.2f", item.Quantity, item.Price)) - len(totalStr)
		if padding > 0 {
			builder.WriteString(strings.Repeat(" ", padding))
		}
		builder.WriteString(totalStr + "\n")
		
		// SKU on separate line
		if item.SKU != "" {
			builder.WriteString(fmt.Sprintf("  SKU: %s\n", item.SKU))
		}
		builder.WriteString("\n")
	}

	builder.WriteString("================================\n")

	// Totals section with right alignment
	builder.WriteString(formatReceiptLine("Subtotal:", fmt.Sprintf("$%.2f", receipt.Subtotal)))

	// Discounts
	if receipt.DiscountAmount > 0 {
		discountText := "Discount:"
		if receipt.DiscountPercentage > 0 {
			discountText = fmt.Sprintf("Discount (%.0f%%):", receipt.DiscountPercentage)
		}
		builder.WriteString(formatReceiptLine(discountText, fmt.Sprintf("-$%.2f", receipt.DiscountAmount)))
	}

	if receipt.PromoAmount > 0 {
		builder.WriteString(formatReceiptLine("Promo Discount:", fmt.Sprintf("-$%.2f", receipt.PromoAmount)))
	}

	builder.WriteString(formatReceiptLine("Tax:", fmt.Sprintf("$%.2f", receipt.Tax)))

	// Tax breakdown (matching React component logic)
	if !receipt.IsSettlement && !receipt.SkipTaxCalculation && !receipt.HasNoTax {
		gst := receipt.Subtotal * 0.05
		pst := receipt.Subtotal * 0.07
		builder.WriteString(fmt.Sprintf("  GST (5%%): $%.2f\n", gst))
		builder.WriteString(fmt.Sprintf("  PST (7%%): $%.2f\n", pst))
	}

	if receipt.Tip > 0 {
		builder.WriteString(formatReceiptLine("Tip:", fmt.Sprintf("$%.2f", receipt.Tip)))
	}

	if receipt.SettlementAmount > 0 {
		builder.WriteString(formatReceiptLine("Account Settlement:", fmt.Sprintf("$%.2f", receipt.SettlementAmount)))
	}

	// Total (bold and highlighted)
	builder.WriteString("\n")
	builder.WriteString(ESC + "E" + string(rune(1))) // Bold on
	builder.WriteString(formatReceiptLine("TOTAL:", fmt.Sprintf("$%.2f", receipt.Total)))
	builder.WriteString(ESC + "E" + string(rune(0))) // Bold off

	builder.WriteString("================================\n")

	// Payment Details section
	builder.WriteString("\nPayment Details\n")
	
	// Payment method with emoji
	paymentEmoji := getPaymentEmoji(receipt.PaymentType)
	paymentDisplay := formatPaymentType(receipt.PaymentType, receipt.IsSettlement, receipt.HasCombinedTransaction)
	builder.WriteString(formatReceiptLine("Payment Method:", fmt.Sprintf("%s %s", paymentEmoji, paymentDisplay)))

	// Card details if applicable
	if strings.Contains(receipt.PaymentType, "credit") || strings.Contains(receipt.PaymentType, "debit") {
		if receipt.CardDetails.CardBrand != "" || receipt.CardDetails.CardLast4 != "" {
			cardText := "Card"
			if receipt.CardDetails.CardBrand != "" {
				cardText = strings.Title(receipt.CardDetails.CardBrand)
			}
			if receipt.CardDetails.CardLast4 != "" {
				cardText += fmt.Sprintf(" ****%s", receipt.CardDetails.CardLast4)
			}
			builder.WriteString(formatReceiptLine("Card:", cardText))
		}

		if receipt.CardDetails.AuthCode != "" {
			builder.WriteString(formatReceiptLine("Auth Code:", receipt.CardDetails.AuthCode))
		}

		if receipt.TerminalId != "" {
			builder.WriteString(formatReceiptLine("Terminal ID:", receipt.TerminalId))
		}
	}

	// Cash details
	if receipt.PaymentType == "cash" && receipt.CashGiven > 0 {
		builder.WriteString("\n")
		builder.WriteString(formatReceiptLine("Cash:", fmt.Sprintf("$%.2f", receipt.CashGiven)))
		builder.WriteString(formatReceiptLine("Change:", fmt.Sprintf("$%.2f", receipt.ChangeDue)))
	}

	// Account information
	if receipt.AccountId != "" {
		builder.WriteString("\nAccount Information\n")
		builder.WriteString(formatReceiptLine("Account ID:", receipt.AccountId))
		if receipt.AccountName != "" {
			builder.WriteString(formatReceiptLine("Account Name:", receipt.AccountName))
		}

		if receipt.IsSettlement || receipt.HasCombinedTransaction {
			builder.WriteString(formatReceiptLine("Previous Balance:", fmt.Sprintf("$%.2f", receipt.AccountBalanceBefore)))
			
			balanceText := fmt.Sprintf("$%.2f", receipt.AccountBalanceAfter)
			if receipt.AccountBalanceAfter == 0 {
				balanceText += " (Fully Settled)"
			}
			builder.WriteString(formatReceiptLine("New Balance:", balanceText))
		}
	}

	// Refund if applicable
	if receipt.RefundAmount > 0 {
		builder.WriteString("\n")
		builder.WriteString(formatReceiptLine("Refund:", fmt.Sprintf("$%.2f", receipt.RefundAmount)))
	}

	builder.WriteString("================================\n")

	// Footer - centered
	builder.WriteString(ESC + "a" + string(rune(1))) // Center alignment
	builder.WriteString("\nThank you for your purchase!\n")
	builder.WriteString(fmt.Sprintf("Visit us again at %s\n", location))
	builder.WriteString(ESC + "a" + string(rune(0))) // Left alignment

	// Transaction ID as barcode simulation
	builder.WriteString("\n")
	builder.WriteString(ESC + "a" + string(rune(1))) // Center alignment
	builder.WriteString(fmt.Sprintf("Transaction: %s\n", receipt.TransactionID))
	builder.WriteString(ESC + "a" + string(rune(0))) // Left alignment

	// Line feeds and cut
	builder.WriteString(ESC + string(rune(100)) + string(rune(3))) // Feed 3 lines
	builder.WriteString("\n\n\n")
	builder.WriteString(GS + string(rune(86)) + string(rune(66)) + string(rune(0))) // Full cut

	return builder.String()
}

// Helper function to format receipt lines with proper spacing
func formatReceiptLine(label, value string) string {
	totalWidth := 32 // Standard thermal printer width
	padding := totalWidth - len(label) - len(value)
	if padding < 1 {
		padding = 1
	}
	return label + strings.Repeat(" ", padding) + value + "\n"
}

// Helper function to get payment emoji
func getPaymentEmoji(paymentType string) string {
	paymentEmojis := map[string]string{
		"cash":    "💵",
		"credit":  "💳",
		"debit":   "💳",
		"account": "📒",
		"cheque":  "🧾",
	}
	
	// Extract base payment type (remove -settlement suffix)
	baseType := strings.Split(paymentType, "-")[0]
	if emoji, exists := paymentEmojis[baseType]; exists {
		return emoji
	}
	return "💰"
}

// Helper function to format payment type display
func formatPaymentType(paymentType string, isSettlement, hasCombinedTransaction bool) string {
	baseType := strings.Split(paymentType, "-")[0]
	displayType := strings.Title(baseType)
	
	if hasCombinedTransaction {
		return displayType + " (Combined Transaction)"
	} else if isSettlement {
		return displayType + " (Account Settlement)"
	}
	return displayType
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
		PrinterIP:   "ESDPRT001",  // Can be IP address or printer name
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
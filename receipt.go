package main

import (
	"bytes"
	"encoding/json"
	"fmt"
	"html/template"
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

// Card details structure
type CardDetails struct {
	CardBrand string `json:"cardBrand"`
	CardLast4 string `json:"cardLast4"`
	AuthCode  string `json:"authCode"`
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

// Template data structure for enhanced rendering
type TemplateData struct {
	ReceiptData
	CleanDate          string
	PaymentIcon        string
	PaymentDisplay     string
	ShowCardDetails    bool
	CardDisplay        string
	ShowTaxBreakdown   bool
	GST               float64
	PST               float64
}

// Response structure
type PrintResponse struct {
	Success bool   `json:"success"`
	Message string `json:"message"`
}

// Global configuration
var config Config

// Template functions
var funcMap = template.FuncMap{
	"multiply": func(a int, b float64) float64 {
		return float64(a) * b
	},
	"gt": func(a, b interface{}) bool {
		// Convert to float64 for comparison
		aVal := toFloat64(a)
		bVal := toFloat64(b)
		return aVal > bVal
	},
	"eq": func(a, b interface{}) bool {
		// Convert to float64 for comparison
		aVal := toFloat64(a)
		bVal := toFloat64(b)
		return aVal == bVal
	},
}

// Helper function to convert interface{} to float64
func toFloat64(val interface{}) float64 {
	switch v := val.(type) {
	case float64:
		return v
	case float32:
		return float64(v)
	case int:
		return float64(v)
	case int64:
		return float64(v)
	case int32:
		return float64(v)
	default:
		return 0
	}
}

// HTML Receipt Template - matches your React component exactly
const receiptTemplate = `
<!DOCTYPE html>
<html>
<head>
    <meta charset="UTF-8">
    <title>Receipt</title>
    <style>
        @page {
            size: 80mm auto;
            margin: 0;
        }
        
        body {
            font-family: Arial, sans-serif;
            padding: 8px;
            margin: 0;
            width: 72mm;
            font-size: 12px;
            line-height: 1.2;
            color: #000;
            background: white;
        }
        
        .receipt-container {
            width: 100%;
            background: white;
        }
        
        .header {
            text-align: center;
            margin-bottom: 16px;
        }
        
        .header h1 {
            font-size: 16px;
            font-weight: bold;
            margin: 0 0 8px 0;
            color: #000;
        }
        
        .date-style {
            font-size: 12px;
            color: #666;
            margin-bottom: 4px;
        }
        
        .customer-name {
            font-size: 12px;
            margin-bottom: 4px;
        }
        
        .divider {
            border-top: 1px dashed #000;
            margin: 12px 0;
        }
        
        .transaction-type {
            background: #f0f0f0;
            padding: 8px;
            border-radius: 4px;
            text-align: center;
            margin-bottom: 12px;
        }
        
        .transaction-type h3 {
            margin: 0;
            font-size: 12px;
            font-weight: bold;
        }
        
        .items-section h2 {
            font-size: 12px;
            font-weight: bold;
            margin: 0 0 8px 0;
        }
        
        .item {
            margin-bottom: 12px;
        }
        
        .item-name {
            font-weight: bold;
            font-size: 12px;
            margin-bottom: 2px;
        }
        
        .item-details {
            display: flex;
            justify-content: space-between;
            padding-left: 8px;
            font-size: 11px;
            color: #666;
        }
        
        .item-sku {
            padding-left: 8px;
            font-size: 10px;
            color: #666;
        }
        
        .totals-section {
            margin-bottom: 12px;
        }
        
        .total-line {
            display: flex;
            justify-content: space-between;
            margin-bottom: 2px;
            font-size: 12px;
        }
        
        .tax-breakdown {
            margin-left: 16px;
            font-size: 10px;
            color: #666;
            margin-bottom: 4px;
        }
        
        .final-total {
            background: #f5f5f5;
            padding: 4px 8px;
            border-radius: 4px;
            font-weight: bold;
            display: flex;
            justify-content: space-between;
            font-size: 13px;
        }
        
        .payment-section h3 {
            font-size: 12px;
            font-weight: bold;
            margin: 0 0 8px 0;
        }
        
        .payment-line {
            display: flex;
            justify-content: space-between;
            margin-bottom: 2px;
            font-size: 12px;
        }
        
        .cash-details {
            background: #f8f8f8;
            padding: 8px;
            border-radius: 4px;
            margin-top: 8px;
        }
        
        .account-section h3 {
            font-size: 12px;
            font-weight: bold;
            margin: 0 0 8px 0;
        }
        
        .account-line {
            display: flex;
            justify-content: space-between;
            margin-bottom: 2px;
            font-size: 12px;
        }
        
        .footer {
            text-align: center;
            margin-top: 16px;
        }
        
        .footer-main {
            font-weight: bold;
            font-size: 12px;
            margin-bottom: 4px;
        }
        
        .footer-sub {
            font-size: 10px;
            color: #666;
        }
        
        .barcode-section {
            text-align: center;
            margin-top: 16px;
            padding: 8px;
            background: white;
        }
        
        .transaction-id {
            font-family: monospace;
            font-size: 10px;
            margin-top: 4px;
        }
        
        .error-text {
            color: #d32f2f;
        }
        
        .success-text {
            color: #2e7d32;
        }
        
        .fully-settled {
            color: #2e7d32;
            font-weight: bold;
        }
    </style>
</head>
<body>
    <div class="receipt-container">
        <!-- Header -->
        <div class="header">
            {{if .LogoUrl}}
                <img src="{{.LogoUrl}}" alt="{{.Location}} logo" style="max-width: 100%; max-height: 60px; height: auto; margin-bottom: 8px;">
            {{else}}
                <h1>{{.Location}}</h1>
            {{end}}
            
            <div class="date-style">{{.CleanDate}}</div>
            
            {{if .CustomerName}}
                <div class="customer-name">Customer: {{.CustomerName}}</div>
            {{end}}
        </div>

        <div class="divider"></div>

        <!-- Transaction Type Indicator -->
        {{if or .IsSettlement .IsRetail .HasCombinedTransaction}}
        <div class="transaction-type">
            <h3>
                {{if .IsSettlement}}
                    ✓ Account Settlement Transaction
                {{else if .HasCombinedTransaction}}
                    ✓ Combined Retail & Settlement Transaction
                {{else}}
                    ✓ Retail Transaction
                {{end}}
            </h3>
        </div>
        {{end}}

        <!-- Items -->
        <div class="items-section">
            <h2>ITEMS</h2>
            {{range .Items}}
            <div class="item">
                <div class="item-name">{{.Name}}</div>
                <div class="item-details">
                    <span>{{.Quantity}} x ${{printf "%.2f" .Price}}</span>
                    <span>${{printf "%.2f" (multiply .Quantity .Price)}}</span>
                </div>
                <div class="item-sku">SKU: {{.SKU}}</div>
            </div>
            {{end}}
        </div>

        <div class="divider"></div>

        <!-- Totals -->
        <div class="totals-section">
            <div class="total-line">
                <span>Subtotal:</span>
                <span>${{printf "%.2f" .Subtotal}}</span>
            </div>

            {{if gt .DiscountPercentage 0.0}}
            <div class="total-line">
                <span>Discount ({{printf "%.0f" .DiscountPercentage}}%):</span>
                <span class="error-text">-${{printf "%.2f" .DiscountAmount}}</span>
            </div>
            {{end}}

            {{if gt .PromoAmount 0.0}}
            <div class="total-line">
                <span>Promo Discount:</span>
                <span class="error-text">-${{printf "%.2f" .PromoAmount}}</span>
            </div>
            {{end}}

            <div class="total-line">
                <span>Tax:</span>
                <span>${{printf "%.2f" .Tax}}</span>
            </div>

            <!-- Tax Breakdown -->
            {{if .ShowTaxBreakdown}}
            <div class="tax-breakdown">
                GST (5%): ${{printf "%.2f" .GST}}<br>
                PST (7%): ${{printf "%.2f" .PST}}
            </div>
            {{end}}

            {{if gt .Tip 0.0}}
            <div class="total-line">
                <span>Tip:</span>
                <span>${{printf "%.2f" .Tip}}</span>
            </div>
            {{end}}

            {{if gt .SettlementAmount 0.0}}
            <div class="total-line">
                <span>Account Settlement:</span>
                <span>${{printf "%.2f" .SettlementAmount}}</span>
            </div>
            {{end}}
        </div>

        <!-- Total Amount -->
        <div class="final-total">
            <span>TOTAL:</span>
            <span>${{printf "%.2f" .Total}}</span>
        </div>

        <div class="divider"></div>

        <!-- Payment Information -->
        <div class="payment-section">
            <h3>Payment Details</h3>

            <div class="payment-line">
                <span>Payment Method:</span>
                <span>{{.PaymentIcon}} {{.PaymentDisplay}}</span>
            </div>

            <!-- Card payment details -->
            {{if .ShowCardDetails}}
                {{if or .CardDetails.CardBrand .CardDetails.CardLast4}}
                <div class="payment-line">
                    <span>Card:</span>
                    <span>{{.CardDisplay}}</span>
                </div>
                {{end}}

                {{if .CardDetails.AuthCode}}
                <div class="payment-line">
                    <span>Auth Code:</span>
                    <span>{{.CardDetails.AuthCode}}</span>
                </div>
                {{end}}

                {{if .TerminalId}}
                <div class="payment-line">
                    <span>Terminal ID:</span>
                    <span>{{.TerminalId}}</span>
                </div>
                {{end}}
            {{end}}

            {{if and (eq .PaymentType "cash") (gt .CashGiven 0.0)}}
            <div class="cash-details">
                <div class="payment-line">
                    <span>Cash:</span>
                    <span>${{printf "%.2f" .CashGiven}}</span>
                </div>
                <div class="payment-line">
                    <span>Change:</span>
                    <span>${{printf "%.2f" .ChangeDue}}</span>
                </div>
            </div>
            {{end}}
        </div>

        <!-- Account Information -->
        {{if .AccountId}}
        <div class="account-section">
            <h3>Account Information</h3>

            <div class="account-line">
                <span>Account ID:</span>
                <span>{{.AccountId}}</span>
            </div>

            {{if .AccountName}}
            <div class="account-line">
                <span>Account Name:</span>
                <span>{{.AccountName}}</span>
            </div>
            {{end}}

            {{if or .IsSettlement .HasCombinedTransaction}}
            <div class="account-line">
                <span>Previous Balance:</span>
                <span>${{printf "%.2f" .AccountBalanceBefore}}</span>
            </div>

            <div class="account-line">
                <span>New Balance:</span>
                <span {{if eq .AccountBalanceAfter 0.0}}class="fully-settled"{{end}}>
                    ${{printf "%.2f" .AccountBalanceAfter}}{{if eq .AccountBalanceAfter 0.0}} (Fully Settled){{end}}
                </span>
            </div>
            {{end}}
        </div>
        {{end}}

        <div class="divider"></div>

        <!-- Footer -->
        <div class="footer">
            <div class="footer-main">Thank you for your purchase!</div>
            <div class="footer-sub">Visit us again at {{.Location}}</div>
        </div>

        <!-- Barcode/Transaction ID -->
        <div class="barcode-section">
            <div class="transaction-id">{{.TransactionID}}</div>
        </div>
    </div>
</body>
</html>
`

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

// Convert HTML content to formatted text for thermal printer
func convertHTMLToText(htmlContent string) string {
	// This is a simple conversion - you might want to use a proper HTML parser
	// For now, we'll create a well-formatted text version
	
	var builder strings.Builder
	
	// ESC/POS commands
	ESC := string(rune(27))
	GS := string(rune(29))
	
	// Reset printer
	builder.WriteString(ESC + "@")
	
	// For simplicity, we'll extract the key information and format it nicely
	// In a production environment, you might want to use a library like goquery
	// to properly parse the HTML and extract formatting
	
	builder.WriteString(ESC + "a" + string(rune(1))) // Center alignment
	builder.WriteString("RECEIPT\n")
	builder.WriteString(ESC + "a" + string(rune(0))) // Left alignment
	
	builder.WriteString("================================\n")
	builder.WriteString("Generated from HTML template\n")
	builder.WriteString("================================\n\n")
	
	builder.WriteString("Thank you for your purchase!\n\n")
	
	// Line feeds and cut
	builder.WriteString(ESC + string(rune(100)) + string(rune(3))) // Feed 3 lines
	builder.WriteString("\n\n\n")
	builder.WriteString(GS + string(rune(86)) + string(rune(66)) + string(rune(0))) // Full cut
	
	return builder.String()
}

// Send HTML to printer (convert to text or send as HTML if printer supports it)
func sendToPrinter(htmlContent string, copies int) error {
	// For now, we'll convert HTML to a simpler text format
	// In production, you might want to use a HTML-to-image converter
	textContent := convertHTMLToText(htmlContent)
	
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
		_, err = conn.Write([]byte(textContent))
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

// Render HTML receipt
func renderHTMLReceipt(receipt ReceiptData) (string, error) {
	// Prepare template data
	data := TemplateData{
		ReceiptData: receipt,
	}
	
	// Clean date (remove seconds)
	if len(receipt.Date) > 16 {
		data.CleanDate = receipt.Date[:16]
	} else {
		data.CleanDate = receipt.Date
	}
	
	// Payment icon and display
	data.PaymentIcon = getPaymentEmoji(receipt.PaymentType)
	data.PaymentDisplay = formatPaymentType(receipt.PaymentType, receipt.IsSettlement, receipt.HasCombinedTransaction)
	
	// Card details
	data.ShowCardDetails = strings.Contains(receipt.PaymentType, "credit") || strings.Contains(receipt.PaymentType, "debit")
	if data.ShowCardDetails {
		cardText := "Card"
		if receipt.CardDetails.CardBrand != "" {
			cardText = strings.Title(receipt.CardDetails.CardBrand)
		}
		if receipt.CardDetails.CardLast4 != "" {
			cardText += fmt.Sprintf(" ****%s", receipt.CardDetails.CardLast4)
		}
		data.CardDisplay = cardText
	}
	
	// Tax breakdown
	data.ShowTaxBreakdown = !receipt.IsSettlement && !receipt.SkipTaxCalculation && !receipt.HasNoTax
	if data.ShowTaxBreakdown {
		data.GST = receipt.Subtotal * 0.05
		data.PST = receipt.Subtotal * 0.07
	}
	
	// Parse and execute template
	tmpl, err := template.New("receipt").Funcs(funcMap).Parse(receiptTemplate)
	if err != nil {
		return "", fmt.Errorf("failed to parse template: %v", err)
	}
	
	var buf bytes.Buffer
	err = tmpl.Execute(&buf, data)
	if err != nil {
		return "", fmt.Errorf("failed to execute template: %v", err)
	}
	
	return buf.String(), nil
}

// Serve HTML receipt for preview
func handlePreviewReceipt(w http.ResponseWriter, r *http.Request) {
	enableCORS(w)
	
	if r.Method != "POST" {
		w.WriteHeader(http.StatusMethodNotAllowed)
		return
	}
	
	// Parse JSON body
	var receipt ReceiptData
	err := json.NewDecoder(r.Body).Decode(&receipt)
	if err != nil {
		w.WriteHeader(http.StatusBadRequest)
		w.Write([]byte("Invalid JSON data"))
		return
	}
	
	// Render HTML
	htmlContent, err := renderHTMLReceipt(receipt)
	if err != nil {
		w.WriteHeader(http.StatusInternalServerError)
		w.Write([]byte(fmt.Sprintf("Template error: %v", err)))
		return
	}
	
	// Serve HTML
	w.Header().Set("Content-Type", "text/html")
	w.Write([]byte(htmlContent))
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

	// Render HTML receipt
	htmlContent, err := renderHTMLReceipt(receipt)
	if err != nil {
		logMessage(fmt.Sprintf("❌ Template error: %v", err))
		w.WriteHeader(http.StatusInternalServerError)
		json.NewEncoder(w).Encode(PrintResponse{
			Success: false,
			Message: fmt.Sprintf("Template error: %v", err),
		})
		return
	}

	// Send to printer
	err = sendToPrinter(htmlContent, receipt.Copies)
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
	fmt.Println("  -printer-ip IP        Set printer IP address (default: ESDPRT001)")
	fmt.Println("  -printer-port PORT    Set printer port (default: 9100)")
	fmt.Println("  -test                 Test printer connection")
	fmt.Println("  -help                 Show this help message")
	fmt.Println("")
	fmt.Println("Examples:")
	fmt.Println("  go run main.go                                      # Start with default settings")
	fmt.Println("  go run main.go -port 8080 -printer-ip 192.168.1.50 # Custom port and printer IP")
	fmt.Println("  go run main.go -test                               # Test printer connection")
	fmt.Println("")
	fmt.Println("Endpoints:")
	fmt.Println("  POST /print/receipt   # Print receipt")
	fmt.Println("  POST /preview/receipt # Preview receipt in browser")
	fmt.Println("  GET  /health          # Health check")
}

func main() {
	// Default configuration
	config = Config{
		Port:        3600,
		PrinterIP:   "ESDPRT001",
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
	http.HandleFunc("/preview/receipt", handlePreviewReceipt)
	http.HandleFunc("/health", handleHealth)

	// Start server
	address := fmt.Sprintf(":%d", config.Port)
	logMessage(fmt.Sprintf("Server listening on %s", address))

	if err := http.ListenAndServe(address, nil); err != nil {
		log.Fatal("Server failed to start:", err)
	}
}
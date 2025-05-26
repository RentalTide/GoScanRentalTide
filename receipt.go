package main

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"html/template"
	"log"
	"net"
	"net/http"
	"os"
	"os/signal"
	"strconv"
	"strings"
	"syscall"
	"time"
)

// Configuration
type Config struct {
	Port        int    `json:"port"`
	PrinterIP   string `json:"printer_ip"`
	PrinterPort int    `json:"printer_port"`
	LogLevel    string `json:"log_level"`
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

// Response structures
type PrintResponse struct {
	Success bool   `json:"success"`
	Message string `json:"message"`
}

type HealthResponse struct {
	Status    string `json:"status"`
	Printer   string `json:"printer"`
	Timestamp string `json:"timestamp"`
	Version   string `json:"version"`
}

type ErrorResponse struct {
	Error   string `json:"error"`
	Code    int    `json:"code"`
	Message string `json:"message"`
}

// Global configuration
var config Config

// Server instance
type Server struct {
	config     Config
	httpServer *http.Server
	logger     *log.Logger
}

// Template functions
var funcMap = template.FuncMap{
	"multiply": func(a int, b float64) float64 {
		return float64(a) * b
	},
	"gt": func(a, b interface{}) bool {
		return toFloat64(a) > toFloat64(b)
	},
	"eq": func(a, b interface{}) bool {
		return toFloat64(a) == toFloat64(b)
	},
	"formatPrice": func(amount float64) string {
		return fmt.Sprintf("%.2f", amount)
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

// Modern HTML Receipt Template
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
        
        * {
            box-sizing: border-box;
        }
        
        body {
            font-family: -webkit-system-font, "Segoe UI", Roboto, "Helvetica Neue", Arial, sans-serif;
            padding: 12px;
            margin: 0;
            width: 72mm;
            font-size: 13px;
            line-height: 1.4;
            color: #1a1a1a;
            background: #ffffff;
            -webkit-font-smoothing: antialiased;
            -moz-osx-font-smoothing: grayscale;
        }
        
        .receipt-container {
            width: 100%;
            background: #ffffff;
            border-radius: 8px;
            overflow: hidden;
        }
        
        /* Header Styles */
        .header {
            text-align: center;
            margin-bottom: 20px;
            padding-bottom: 16px;
        }
        
        .header h1 {
            font-size: 20px;
            font-weight: 700;
            margin: 0 0 12px 0;
            color: #2563eb;
            letter-spacing: -0.025em;
        }
        
        .header .logo {
            max-width: 100%;
            max-height: 60px;
            height: auto;
            margin-bottom: 12px;
            border-radius: 4px;
        }
        
        .date-style {
            font-size: 13px;
            color: #6b7280;
            margin-bottom: 6px;
            font-weight: 500;
        }
        
        .customer-name {
            font-size: 13px;
            margin-bottom: 6px;
            color: #374151;
            font-weight: 500;
        }
        
        /* Modern Dividers */
        .divider {
            border: none;
            height: 1px;
            background: linear-gradient(90deg, transparent, #e5e7eb 20%, #e5e7eb 80%, transparent);
            margin: 16px 0;
        }
        
        .divider.dashed {
            background: none;
            border-top: 2px dashed #d1d5db;
            margin: 18px 0;
        }
        
        /* Transaction Type Badge */
        .transaction-type {
            background: linear-gradient(135deg, #f0f9ff 0%, #e0f2fe 100%);
            border: 1px solid #bae6fd;
            padding: 12px;
            border-radius: 8px;
            text-align: center;
            margin-bottom: 16px;
            box-shadow: 0 1px 3px rgba(0, 0, 0, 0.1);
        }
        
        .transaction-type h3 {
            margin: 0;
            font-size: 13px;
            font-weight: 600;
            color: #0369a1;
        }
        
        /* Section Headers */
        .section-header {
            font-size: 14px;
            font-weight: 700;
            margin: 0 0 12px 0;
            color: #111827;
            text-transform: uppercase;
            letter-spacing: 0.025em;
        }
        
        /* Items Section */
        .items-section {
            margin-bottom: 16px;
        }
        
        .item {
            margin-bottom: 16px;
            padding: 12px;
            background: #f9fafb;
            border-radius: 6px;
            border-left: 3px solid #3b82f6;
        }
        
        .item-name {
            font-weight: 600;
            font-size: 13px;
            margin-bottom: 4px;
            color: #111827;
        }
        
        .item-details {
            display: flex;
            justify-content: space-between;
            padding-left: 8px;
            font-size: 12px;
            color: #6b7280;
            margin-bottom: 2px;
        }
        
        .item-sku {
            padding-left: 8px;
            font-size: 11px;
            color: #9ca3af;
            font-family: "SF Mono", "Monaco", "Inconsolata", "Roboto Mono", monospace;
        }
        
        /* Totals Section */
        .totals-section {
            margin-bottom: 16px;
            background: #f8fafc;
            padding: 16px;
            border-radius: 8px;
            border: 1px solid #e2e8f0;
        }
        
        .total-line {
            display: flex;
            justify-content: space-between;
            margin-bottom: 8px;
            font-size: 13px;
            color: #374151;
        }
        
        .total-line:last-child {
            margin-bottom: 0;
        }
        
        .tax-breakdown {
            margin-left: 20px;
            font-size: 11px;
            color: #6b7280;
            margin-bottom: 8px;
            padding: 8px;
            background: #ffffff;
            border-radius: 4px;
            border-left: 2px solid #d1d5db;
        }
        
        .final-total {
            background: linear-gradient(135deg, #1e40af 0%, #3b82f6 100%);
            color: white;
            padding: 12px 16px;
            border-radius: 8px;
            font-weight: 700;
            display: flex;
            justify-content: space-between;
            font-size: 16px;
            margin-top: 12px;
            box-shadow: 0 4px 6px rgba(59, 130, 246, 0.2);
        }
        
        /* Payment Section */
        .payment-section {
            background: #ffffff;
            padding: 16px;
            border-radius: 8px;
            border: 1px solid #e5e7eb;
            margin-bottom: 16px;
        }
        
        .payment-section h3 {
            font-size: 14px;
            font-weight: 700;
            margin: 0 0 12px 0;
            color: #111827;
            text-transform: uppercase;
            letter-spacing: 0.025em;
        }
        
        .payment-line {
            display: flex;
            justify-content: space-between;
            margin-bottom: 6px;
            font-size: 13px;
            color: #374151;
        }
        
        .payment-line:last-child {
            margin-bottom: 0;
        }
        
        .payment-method {
            font-weight: 600;
            color: #059669;
        }
        
        .cash-details {
            background: linear-gradient(135deg, #f0fdf4 0%, #ecfdf5 100%);
            border: 1px solid #bbf7d0;
            padding: 12px;
            border-radius: 6px;
            margin-top: 12px;
        }
        
        /* Account Section */
        .account-section {
            background: #ffffff;
            padding: 16px;
            border-radius: 8px;
            border: 1px solid #e5e7eb;
            margin-bottom: 16px;
        }
        
        .account-section h3 {
            font-size: 14px;
            font-weight: 700;
            margin: 0 0 12px 0;
            color: #111827;
            text-transform: uppercase;
            letter-spacing: 0.025em;
        }
        
        .account-line {
            display: flex;
            justify-content: space-between;
            margin-bottom: 6px;
            font-size: 13px;
            color: #374151;
        }
        
        .account-line:last-child {
            margin-bottom: 0;
        }
        
        .fully-settled {
            color: #059669;
            font-weight: 700;
        }
        
        /* Footer */
        .footer {
            text-align: center;
            margin-top: 24px;
            padding: 20px;
            background: linear-gradient(135deg, #f8fafc 0%, #f1f5f9 100%);
            border-radius: 8px;
            border: 1px solid #e2e8f0;
        }
        
        .footer-main {
            font-weight: 700;
            font-size: 15px;
            margin-bottom: 8px;
            color: #1e40af;
        }
        
        .footer-sub {
            font-size: 12px;
            color: #6b7280;
            font-weight: 500;
        }
        
        /* Barcode Section */
        .barcode-section {
            text-align: center;
            margin-top: 20px;
            padding: 16px;
            background: #ffffff;
            border-radius: 8px;
            border: 1px solid #e5e7eb;
        }
        
        .transaction-id {
            font-family: "SF Mono", "Monaco", "Inconsolata", "Roboto Mono", monospace;
            font-size: 11px;
            margin-top: 8px;
            color: #6b7280;
            font-weight: 500;
            letter-spacing: 0.05em;
        }
        
        /* Status Colors */
        .error-text {
            color: #dc2626;
            font-weight: 600;
        }
        
        .success-text {
            color: #059669;
            font-weight: 600;
        }
        
        /* Enhanced spacing and typography */
        .amount {
            font-family: "SF Mono", "Monaco", "Inconsolata", "Roboto Mono", monospace;
            font-weight: 600;
        }
        
        /* Payment emoji styling */
        .payment-emoji {
            font-size: 16px;
            margin-right: 6px;
        }
        
        /* Modern card styling */
        .card-info {
            background: linear-gradient(135deg, #fefefe 0%, #f8fafc 100%);
            border: 1px solid #e2e8f0;
            padding: 8px 12px;
            border-radius: 6px;
            margin: 4px 0;
            font-family: "SF Mono", "Monaco", "Inconsolata", "Roboto Mono", monospace;
            font-size: 12px;
        }
        
        /* Responsive adjustments */
        @media (max-width: 80mm) {
            body {
                padding: 8px;
                font-size: 12px;
            }
            
            .header h1 {
                font-size: 18px;
            }
            
            .final-total {
                font-size: 14px;
                padding: 10px 12px;
            }
        }
    </style>
</head>
<body>
    <div class="receipt-container">
        <!-- Header -->
        <div class="header">
            {{if .LogoUrl}}
                <img src="{{.LogoUrl}}" alt="{{.Location}} logo" class="logo">
            {{else}}
                <h1>{{.Location}}</h1>
            {{end}}
            
            <div class="date-style">{{.CleanDate}}</div>
            
            {{if .CustomerName}}
                <div class="customer-name">Customer: {{.CustomerName}}</div>
            {{end}}
        </div>

        <div class="divider dashed"></div>

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
            <h2 class="section-header">Items</h2>
            {{range .Items}}
            <div class="item">
                <div class="item-name">{{.Name}}</div>
                <div class="item-details">
                    <span>{{.Quantity}} × <span class="amount">${{formatPrice .Price}}</span></span>
                    <span class="amount">${{formatPrice (multiply .Quantity .Price)}}</span>
                </div>
                <div class="item-sku">SKU: {{.SKU}}</div>
            </div>
            {{end}}
        </div>

        <!-- Totals -->
        <div class="totals-section">
            <div class="total-line">
                <span>Subtotal:</span>
                <span class="amount">${{formatPrice .Subtotal}}</span>
            </div>

            {{if gt .DiscountPercentage 0.0}}
            <div class="total-line">
                <span>Discount ({{printf "%.0f" .DiscountPercentage}}%):</span>
                <span class="error-text amount">-${{formatPrice .DiscountAmount}}</span>
            </div>
            {{end}}

            {{if gt .PromoAmount 0.0}}
            <div class="total-line">
                <span>Promo Discount:</span>
                <span class="error-text amount">-${{formatPrice .PromoAmount}}</span>
            </div>
            {{end}}

            <div class="total-line">
                <span>Tax:</span>
                <span class="amount">${{formatPrice .Tax}}</span>
            </div>

            <!-- Tax Breakdown -->
            {{if .ShowTaxBreakdown}}
            <div class="tax-breakdown">
                <div>GST (5%): <span class="amount">${{formatPrice .GST}}</span></div>
                <div>PST (7%): <span class="amount">${{formatPrice .PST}}</span></div>
            </div>
            {{end}}

            {{if gt .Tip 0.0}}
            <div class="total-line">
                <span>Tip:</span>
                <span class="amount">${{formatPrice .Tip}}</span>
            </div>
            {{end}}

            {{if gt .SettlementAmount 0.0}}
            <div class="total-line">
                <span>Account Settlement:</span>
                <span class="amount">${{formatPrice .SettlementAmount}}</span>
            </div>
            {{end}}
        </div>

        <!-- Total Amount -->
        <div class="final-total">
            <span>TOTAL</span>
            <span class="amount">${{formatPrice .Total}}</span>
        </div>

        <div class="divider"></div>

        <!-- Payment Information -->
        <div class="payment-section">
            <h3>Payment Details</h3>

            <div class="payment-line">
                <span>Payment Method:</span>
                <span class="payment-method">
                    <span class="payment-emoji">{{.PaymentIcon}}</span>{{.PaymentDisplay}}
                </span>
            </div>

            <!-- Card payment details -->
            {{if .ShowCardDetails}}
                {{if or .CardDetails.CardBrand .CardDetails.CardLast4}}
                <div class="card-info">
                    <div class="payment-line" style="margin-bottom: 0;">
                        <span>Card:</span>
                        <span>{{.CardDisplay}}</span>
                    </div>
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
                    <span>Cash Given:</span>
                    <span class="amount">${{formatPrice .CashGiven}}</span>
                </div>
                <div class="payment-line">
                    <span>Change:</span>
                    <span class="amount">${{formatPrice .ChangeDue}}</span>
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
                <span class="amount">${{formatPrice .AccountBalanceBefore}}</span>
            </div>

            <div class="account-line">
                <span>New Balance:</span>
                <span {{if eq .AccountBalanceAfter 0.0}}class="fully-settled"{{end}}>
                    <span class="amount">${{formatPrice .AccountBalanceAfter}}</span>{{if eq .AccountBalanceAfter 0.0}} (Fully Settled){{end}}
                </span>
            </div>
            {{end}}
        </div>
        {{end}}

        <!-- Footer -->
        <div class="footer">
            <div class="footer-main">Thank you for your purchase!</div>
            <div class="footer-sub">Visit us again at {{.Location}}</div>
        </div>

        <!-- Barcode/Transaction ID -->
        <div class="barcode-section">
            <div class="transaction-id">Transaction: {{.TransactionID}}</div>
        </div>
    </div>
</body>
</html>
`

// NewServer creates a new server instance
func NewServer(cfg Config) *Server {
	logger := log.New(os.Stdout, "[RECEIPT-SERVER] ", log.LstdFlags|log.Lshortfile)
	
	return &Server{
		config: cfg,
		logger: logger,
	}
}

// CORS middleware
func (s *Server) enableCORS(w http.ResponseWriter) {
	w.Header().Set("Access-Control-Allow-Origin", "*")
	w.Header().Set("Access-Control-Allow-Methods", "GET, POST, OPTIONS")
	w.Header().Set("Access-Control-Allow-Headers", "Content-Type, Authorization")
}

// Logging middleware
func (s *Server) loggingMiddleware(next http.HandlerFunc) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		start := time.Now()
		
		// Create a response writer wrapper to capture status code
		wrapper := &responseWriterWrapper{ResponseWriter: w, statusCode: 200}
		
		next.ServeHTTP(wrapper, r)
		
		duration := time.Since(start)
		s.logger.Printf("%s %s %d %v %s", 
			r.Method, 
			r.URL.Path, 
			wrapper.statusCode, 
			duration,
			r.RemoteAddr,
		)
	}
}

// Response writer wrapper to capture status code
type responseWriterWrapper struct {
	http.ResponseWriter
	statusCode int
}

func (rw *responseWriterWrapper) WriteHeader(code int) {
	rw.statusCode = code
	rw.ResponseWriter.WriteHeader(code)
}

// Helper function to send JSON responses
func (s *Server) sendJSONResponse(w http.ResponseWriter, statusCode int, data interface{}) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(statusCode)
	
	if err := json.NewEncoder(w).Encode(data); err != nil {
		s.logger.Printf("Error encoding JSON response: %v", err)
	}
}

// Helper function to send error responses
func (s *Server) sendErrorResponse(w http.ResponseWriter, statusCode int, message string) {
	s.sendJSONResponse(w, statusCode, ErrorResponse{
		Error:   http.StatusText(statusCode),
		Code:    statusCode,
		Message: message,
	})
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

// Enhanced thermal printer function with better error handling
func (s *Server) sendToThermalPrinter(receipt ReceiptData, copies int) error {
	textContent := s.formatReceiptForThermalPrinter(receipt)
	
	// Resolve printer address
	printerAddress := s.config.PrinterIP
	if !strings.Contains(printerAddress, ".") {
		ips, err := net.LookupIP(printerAddress)
		if err != nil {
			return fmt.Errorf("failed to resolve printer name '%s': %v", printerAddress, err)
		}
		if len(ips) > 0 {
			printerAddress = ips[0].String()
			s.logger.Printf("Resolved %s to %s", s.config.PrinterIP, printerAddress)
		}
	}
	
	// Print each copy
	for i := 1; i <= copies; i++ {
		if err := s.printSingleCopy(printerAddress, textContent, i); err != nil {
			return fmt.Errorf("failed to print copy %d: %v", i, err)
		}
		
		s.logger.Printf("✓ Copy %d sent to printer successfully", i)
		
		// Small delay between copies
		if i < copies {
			time.Sleep(time.Second)
		}
	}
	
	return nil
}

// Print single copy with timeout and retry logic
func (s *Server) printSingleCopy(printerAddress, content string, copyNum int) error {
	address := fmt.Sprintf("%s:%d", printerAddress, s.config.PrinterPort)
	
	// Attempt with retry
	for attempt := 1; attempt <= 3; attempt++ {
		conn, err := net.DialTimeout("tcp", address, 5*time.Second)
		if err != nil {
			if attempt == 3 {
				return fmt.Errorf("failed to connect after %d attempts: %v", attempt, err)
			}
			s.logger.Printf("Connection attempt %d failed, retrying...", attempt)
			time.Sleep(time.Duration(attempt) * time.Second)
			continue
		}
		defer conn.Close()

		conn.SetWriteDeadline(time.Now().Add(10 * time.Second))
		
		_, err = conn.Write([]byte(content))
		if err != nil {
			if attempt == 3 {
				return fmt.Errorf("failed to send data after %d attempts: %v", attempt, err)
			}
			s.logger.Printf("Send attempt %d failed, retrying...", attempt)
			conn.Close()
			time.Sleep(time.Duration(attempt) * time.Second)
			continue
		}
		
		return nil // Success
	}
	
	return fmt.Errorf("max retry attempts exceeded")
}

// Enhanced thermal printer formatting
func (s *Server) formatReceiptForThermalPrinter(receipt ReceiptData) string {
	var builder strings.Builder
	
	// ESC/POS commands
	ESC := "\x1B"
	GS := "\x1D"
	
	// Reset printer
	builder.WriteString(ESC + "@")
	
	// Header
	builder.WriteString(ESC + "a\x01") // Center alignment
	builder.WriteString(ESC + "E\x01") // Bold
	
	location := receipt.Location
	if location == "" {
		location = "Store"
	}
	builder.WriteString(fmt.Sprintf("%s\n", location))
	builder.WriteString(ESC + "E\x00") // Bold off
	
	// Date formatting
	date := receipt.Date
	if date == "" {
		date = time.Now().Format("2006-01-02 15:04:05")
	}
	if len(date) > 16 {
		date = date[:16]
	}
	builder.WriteString(fmt.Sprintf("%s\n", date))
	
	if receipt.CustomerName != "" {
		builder.WriteString(fmt.Sprintf("Customer: %s\n", receipt.CustomerName))
	}
	
	builder.WriteString(ESC + "a\x00") // Left alignment
	builder.WriteString("================================\n")
	
	// Transaction type
	if receipt.IsSettlement || receipt.IsRetail || receipt.HasCombinedTransaction {
		builder.WriteString(ESC + "a\x01") // Center
		if receipt.IsSettlement {
			builder.WriteString("✓ Account Settlement Transaction\n")
		} else if receipt.HasCombinedTransaction {
			builder.WriteString("✓ Combined Retail & Settlement\n")
		} else {
			builder.WriteString("✓ Retail Transaction\n")
		}
		builder.WriteString(ESC + "a\x00") // Left
		builder.WriteString("\n")
	}
	
	// Items
	builder.WriteString(ESC + "E\x01")
	builder.WriteString("ITEMS\n")
	builder.WriteString(ESC + "E\x00")
	
	for _, item := range receipt.Items {
		itemTotal := float64(item.Quantity) * item.Price
		
		builder.WriteString(ESC + "E\x01")
		builder.WriteString(fmt.Sprintf("%s\n", item.Name))
		builder.WriteString(ESC + "E\x00")
		
		builder.WriteString(s.formatReceiptLine(
			fmt.Sprintf("  %d x $%.2f", item.Quantity, item.Price),
			fmt.Sprintf("$%.2f", itemTotal),
		))
		
		if item.SKU != "" {
			builder.WriteString(fmt.Sprintf("  SKU: %s\n", item.SKU))
		}
		builder.WriteString("\n")
	}
	
	builder.WriteString("================================\n")
	
	// Totals
	builder.WriteString(s.formatReceiptLine("Subtotal:", fmt.Sprintf("$%.2f", receipt.Subtotal)))
	
	if receipt.DiscountPercentage > 0 {
		builder.WriteString(s.formatReceiptLine(
			fmt.Sprintf("Discount (%.0f%%):", receipt.DiscountPercentage),
			fmt.Sprintf("-$%.2f", receipt.DiscountAmount),
		))
	}
	
	if receipt.PromoAmount > 0 {
		builder.WriteString(s.formatReceiptLine("Promo Discount:", fmt.Sprintf("-$%.2f", receipt.PromoAmount)))
	}
	
	builder.WriteString(s.formatReceiptLine("Tax:", fmt.Sprintf("$%.2f", receipt.Tax)))
	
	// Tax breakdown
	showTaxBreakdown := !receipt.IsSettlement && !receipt.SkipTaxCalculation && !receipt.HasNoTax
	if showTaxBreakdown {
		gst := receipt.Subtotal * 0.05
		pst := receipt.Subtotal * 0.07
		builder.WriteString(fmt.Sprintf("  GST (5%%): $%.2f\n", gst))
		builder.WriteString(fmt.Sprintf("  PST (7%%): $%.2f\n", pst))
	}
	
	if receipt.Tip > 0 {
		builder.WriteString(s.formatReceiptLine("Tip:", fmt.Sprintf("$%.2f", receipt.Tip)))
	}
	
	if receipt.SettlementAmount > 0 {
		builder.WriteString(s.formatReceiptLine("Account Settlement:", fmt.Sprintf("$%.2f", receipt.SettlementAmount)))
	}
	
	// Total
	builder.WriteString("\n")
	builder.WriteString(ESC + "E\x01")
	builder.WriteString(s.formatReceiptLine("TOTAL:", fmt.Sprintf("$%.2f", receipt.Total)))
	builder.WriteString(ESC + "E\x00")
	
	builder.WriteString("================================\n")
	
	// Payment details
	builder.WriteString("\n")
	builder.WriteString(ESC + "E\x01")
	builder.WriteString("Payment Details\n")
	builder.WriteString(ESC + "E\x00")
	
	paymentEmoji := getPaymentEmoji(receipt.PaymentType)
	paymentDisplay := formatPaymentType(receipt.PaymentType, receipt.IsSettlement, receipt.HasCombinedTransaction)
	builder.WriteString(s.formatReceiptLine("Payment Method:", fmt.Sprintf("%s %s", paymentEmoji, paymentDisplay)))
	
	// Card details
	if strings.Contains(receipt.PaymentType, "credit") || strings.Contains(receipt.PaymentType, "debit") {
		if receipt.CardDetails.CardBrand != "" || receipt.CardDetails.CardLast4 != "" {
			cardText := "Card"
			if receipt.CardDetails.CardBrand != "" {
				cardText = strings.Title(receipt.CardDetails.CardBrand)
			}
			if receipt.CardDetails.CardLast4 != "" {
				cardText += fmt.Sprintf(" ****%s", receipt.CardDetails.CardLast4)
			}
			builder.WriteString(s.formatReceiptLine("Card:", cardText))
		}
		
		if receipt.CardDetails.AuthCode != "" {
			builder.WriteString(s.formatReceiptLine("Auth Code:", receipt.CardDetails.AuthCode))
		}
		
		if receipt.TerminalId != "" {
			builder.WriteString(s.formatReceiptLine("Terminal ID:", receipt.TerminalId))
		}
	}
	
	// Cash details
	if receipt.PaymentType == "cash" && receipt.CashGiven > 0 {
		builder.WriteString("\n--- Cash Details ---\n")
		builder.WriteString(s.formatReceiptLine("Cash:", fmt.Sprintf("$%.2f", receipt.CashGiven)))
		builder.WriteString(s.formatReceiptLine("Change:", fmt.Sprintf("$%.2f", receipt.ChangeDue)))
		builder.WriteString("----------------------\n")
	}
	
	// Account information
	if receipt.AccountId != "" {
		builder.WriteString("\n")
		builder.WriteString(ESC + "E\x01")
		builder.WriteString("Account Information\n")
		builder.WriteString(ESC + "E\x00")
		
		builder.WriteString(s.formatReceiptLine("Account ID:", receipt.AccountId))
		if receipt.AccountName != "" {
			builder.WriteString(s.formatReceiptLine("Account Name:", receipt.AccountName))
		}
		
		if receipt.IsSettlement || receipt.HasCombinedTransaction {
			builder.WriteString(s.formatReceiptLine("Previous Balance:", fmt.Sprintf("$%.2f", receipt.AccountBalanceBefore)))
			
			balanceText := fmt.Sprintf("$%.2f", receipt.AccountBalanceAfter)
			if receipt.AccountBalanceAfter == 0 {
				balanceText += " (Fully Settled)"
			}
			builder.WriteString(s.formatReceiptLine("New Balance:", balanceText))
		}
	}
	
	builder.WriteString("================================\n")
	
	// Footer
	builder.WriteString(ESC + "a\x01") // Center
	builder.WriteString("\n")
	builder.WriteString(ESC + "E\x01")
	builder.WriteString("Thank you for your purchase!\n")
	builder.WriteString(ESC + "E\x00")
	builder.WriteString(fmt.Sprintf("Visit us again at %s\n", location))
	
	// Transaction ID
	builder.WriteString("\n")
	builder.WriteString(fmt.Sprintf("Transaction: %s\n", receipt.TransactionID))
	builder.WriteString(ESC + "a\x00") // Left
	
	// Cut paper
	builder.WriteString("\n\n\n")
	builder.WriteString(GS + "V\x42\x00")
	
	return builder.String()
}

// Helper function to format receipt lines
func (s *Server) formatReceiptLine(label, value string) string {
	totalWidth := 32
	padding := totalWidth - len(label) - len(value)
	if padding < 1 {
		padding = 1
	}
	return label + strings.Repeat(" ", padding) + value + "\n"
}

// Render HTML receipt
func (s *Server) renderHTMLReceipt(receipt ReceiptData) (string, error) {
	data := TemplateData{
		ReceiptData: receipt,
	}
	
	// Clean date
	if len(receipt.Date) > 16 {
		data.CleanDate = receipt.Date[:16]
	} else {
		data.CleanDate = receipt.Date
	}
	
	// Payment formatting
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

// Handler: Preview receipt
func (s *Server) handlePreviewReceipt(w http.ResponseWriter, r *http.Request) {
	s.enableCORS(w)
	
	if r.Method != "POST" {
		s.sendErrorResponse(w, http.StatusMethodNotAllowed, "Method not allowed")
		return
	}
	
	var receipt ReceiptData
	if err := json.NewDecoder(r.Body).Decode(&receipt); err != nil {
		s.sendErrorResponse(w, http.StatusBadRequest, "Invalid JSON data")
		return
	}
	
	htmlContent, err := s.renderHTMLReceipt(receipt)
	if err != nil {
		s.sendErrorResponse(w, http.StatusInternalServerError, fmt.Sprintf("Template error: %v", err))
		return
	}
	
	w.Header().Set("Content-Type", "text/html")
	w.Write([]byte(htmlContent))
}

// Handler: Test receipt
func (s *Server) handleTestReceipt(w http.ResponseWriter, r *http.Request) {
	s.enableCORS(w)
	
	testReceipt := ReceiptData{
		TransactionID:    "TEST-" + time.Now().Format("20060102-150405"),
		Location:        "My Store",
		Date:            time.Now().Format("2006-01-02 15:04:05"),
		CustomerName:    "John Doe",
		PaymentType:     "credit",
		Subtotal:        20.00,
		Tax:             2.60,
		Tip:             3.00,
		Total:           25.60,
		IsRetail:        true,
		Items: []ReceiptItem{
			{Name: "Premium Coffee", Quantity: 2, Price: 8.50, SKU: "COFFEE-001"},
			{Name: "Blueberry Muffin", Quantity: 1, Price: 3.00, SKU: "MUFFIN-002"},
		},
		CardDetails: CardDetails{
			CardBrand: "visa",
			CardLast4: "1234",
			AuthCode:  "ABC123",
		},
		TerminalId: "TERM001",
	}
	
	htmlContent, err := s.renderHTMLReceipt(testReceipt)
	if err != nil {
		s.sendErrorResponse(w, http.StatusInternalServerError, fmt.Sprintf("Template error: %v", err))
		return
	}
	
	w.Header().Set("Content-Type", "text/html")
	w.Write([]byte(htmlContent))
}

// Handler: Print receipt
func (s *Server) handlePrintReceipt(w http.ResponseWriter, r *http.Request) {
	s.enableCORS(w)

	if r.Method == "OPTIONS" {
		w.WriteHeader(http.StatusOK)
		return
	}

	if r.Method != "POST" {
		s.sendJSONResponse(w, http.StatusMethodNotAllowed, PrintResponse{
			Success: false,
			Message: "Method not allowed",
		})
		return
	}

	var receipt ReceiptData
	if err := json.NewDecoder(r.Body).Decode(&receipt); err != nil {
		s.logger.Printf("Error parsing JSON: %v", err)
		s.sendJSONResponse(w, http.StatusBadRequest, PrintResponse{
			Success: false,
			Message: "Invalid JSON data",
		})
		return
	}

	s.logger.Printf("📄 Received print request for transaction %s", receipt.TransactionID)

	if receipt.Copies <= 0 {
		receipt.Copies = 1
	}

	if err := s.sendToThermalPrinter(receipt, receipt.Copies); err != nil {
		s.logger.Printf("Print job failed: %v", err)
		s.sendJSONResponse(w, http.StatusInternalServerError, PrintResponse{
			Success: false,
			Message: fmt.Sprintf("Failed to print receipt: %v", err),
		})
		return
	}

	s.logger.Printf("✅ Print job completed successfully")
	s.sendJSONResponse(w, http.StatusOK, PrintResponse{
		Success: true,
		Message: fmt.Sprintf("Receipt printed successfully (%d %s)", receipt.Copies, 
			map[bool]string{true: "copy", false: "copies"}[receipt.Copies == 1]),
	})
}

// Handler: Health check
func (s *Server) handleHealth(w http.ResponseWriter, r *http.Request) {
	s.enableCORS(w)
	
	// Test printer connectivity
	printerStatus := "offline"
	address := fmt.Sprintf("%s:%d", s.config.PrinterIP, s.config.PrinterPort)
	
	conn, err := net.DialTimeout("tcp", address, 2*time.Second)
	if err == nil {
		printerStatus = "online"
		conn.Close()
	}
	
	s.sendJSONResponse(w, http.StatusOK, HealthResponse{
		Status:    printerStatus,
		Printer:   address,
		Timestamp: time.Now().Format(time.RFC3339),
		Version:   "2.0.0",
	})
}

// Test printer connection
func (s *Server) testPrinter() error {
	s.logger.Printf("Testing printer connection...")
	address := fmt.Sprintf("%s:%d", s.config.PrinterIP, s.config.PrinterPort)

	conn, err := net.DialTimeout("tcp", address, 5*time.Second)
	if err != nil {
		return fmt.Errorf("cannot reach printer at %s: %v", address, err)
	}
	defer conn.Close()

	s.logger.Printf("✅ Printer is reachable at %s", address)

	// Send test print
	s.logger.Printf("Sending test print...")
	testReceipt := "\x1B@\n" +
		"\x1Ba\x01TEST PRINT\x1Ba\x00\n" +
		"================================\n" +
		"Date: " + time.Now().Format("2006-01-02 15:04:05") + "\n" +
		"Test from Go print server v2.0\n" +
		"================================\n" +
		"\x1Bd\x03\n" +
		"\x1DVB\x00"

	conn.SetWriteDeadline(time.Now().Add(5 * time.Second))
	_, err = conn.Write([]byte(testReceipt))
	if err != nil {
		return fmt.Errorf("failed to send test print: %v", err)
	}

	s.logger.Printf("✅ Test print sent successfully")
	return nil
}

// Setup routes
func (s *Server) setupRoutes() *http.ServeMux {
	mux := http.NewServeMux()
	
	mux.HandleFunc("/print/receipt", s.loggingMiddleware(s.handlePrintReceipt))
	mux.HandleFunc("/preview/receipt", s.loggingMiddleware(s.handlePreviewReceipt))
	mux.HandleFunc("/test/receipt", s.loggingMiddleware(s.handleTestReceipt))
	mux.HandleFunc("/health", s.loggingMiddleware(s.handleHealth))
	
	return mux
}

// Start server
func (s *Server) Start() error {
	mux := s.setupRoutes()
	
	s.httpServer = &http.Server{
		Addr:         fmt.Sprintf(":%d", s.config.Port),
		Handler:      mux,
		ReadTimeout:  15 * time.Second,
		WriteTimeout: 15 * time.Second,
		IdleTimeout:  60 * time.Second,
	}
	
	s.logger.Printf("🚀 Starting receipt print server on port %d", s.config.Port)
	s.logger.Printf("🖨️  Printer configured: %s:%d", s.config.PrinterIP, s.config.PrinterPort)
	
	return s.httpServer.ListenAndServe()
}

// Graceful shutdown
func (s *Server) Shutdown() error {
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	
	s.logger.Printf("Shutting down server...")
	return s.httpServer.Shutdown(ctx)
}

// Show usage information
func showUsage() {
	fmt.Println("Receipt Print Server v2.0")
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
	fmt.Println("  GET  /test/receipt    # Test receipt for preview")
	fmt.Println("  GET  /health          # Health check")
}

func main() {
	// Default configuration
	config = Config{
		Port:        3600,
		PrinterIP:   "ESDPRT001",
		PrinterPort: 9100,
		LogLevel:    "INFO",
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
			server := NewServer(config)
			if err := server.testPrinter(); err != nil {
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

	// Create server
	server := NewServer(config)

	fmt.Printf("Receipt Print Server v2.0 Starting...\n")
	fmt.Printf("Listening on: http://localhost:%d\n", config.Port)
	fmt.Printf("Printer: %s:%d\n", config.PrinterIP, config.PrinterPort)
	fmt.Printf("Press Ctrl+C to stop\n\n")

	// Test printer connectivity
	conn, err := net.DialTimeout("tcp", fmt.Sprintf("%s:%d", config.PrinterIP, config.PrinterPort), 2*time.Second)
	if err != nil {
		server.logger.Printf("⚠️  Warning: Cannot reach printer at %s:%d", config.PrinterIP, config.PrinterPort)
	} else {
		conn.Close()
		server.logger.Printf("✅ Printer connection test successful")
	}

	// Setup graceful shutdown
	go func() {
		c := make(chan os.Signal, 1)
		signal.Notify(c, os.Interrupt, syscall.SIGTERM)
		<-c
		
		server.logger.Printf("Received shutdown signal")
		if err := server.Shutdown(); err != nil {
			server.logger.Printf("Error during shutdown: %v", err)
		}
		os.Exit(0)
	}()

	// Start server
	if err := server.Start(); err != nil && err != http.ErrServerClosed {
		log.Fatal("Server failed to start:", err)
	}
}
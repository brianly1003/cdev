// Package pairing handles mobile device pairing via QR codes.
package pairing

import (
	"encoding/json"
	"fmt"
	"strings"

	"github.com/skip2/go-qrcode"
)

// PairingInfo contains the information encoded in the QR code.
type PairingInfo struct {
	WebSocket string `json:"ws"`
	HTTP      string `json:"http"`
	SessionID string `json:"session"`
	Token     string `json:"token,omitempty"`
	RepoName  string `json:"repo"`
}

// QRGenerator generates QR codes for mobile pairing.
type QRGenerator struct {
	host            string
	wsPort          int
	httpPort        int
	sessionID       string
	repoName        string
	token           string
	externalWSURL   string // Optional: override WebSocket URL (for VS Code port forwarding)
	externalHTTPURL string // Optional: override HTTP URL (for VS Code port forwarding)
}

// NewQRGenerator creates a new QR code generator.
func NewQRGenerator(host string, wsPort, httpPort int, sessionID, repoName string) *QRGenerator {
	return &QRGenerator{
		host:      host,
		wsPort:    wsPort,
		httpPort:  httpPort,
		sessionID: sessionID,
		repoName:  repoName,
	}
}

// SetExternalURLs sets the external/public URLs for port forwarding scenarios.
// When set, these URLs are used in the QR code instead of the local host:port URLs.
func (g *QRGenerator) SetExternalURLs(wsURL, httpURL string) {
	g.externalWSURL = wsURL
	g.externalHTTPURL = httpURL
}

// SetToken sets the pairing token.
func (g *QRGenerator) SetToken(token string) {
	g.token = token
}

// GetPairingInfo returns the pairing information.
// If external URLs are set, they are used instead of local host:port URLs.
func (g *QRGenerator) GetPairingInfo() *PairingInfo {
	wsURL := fmt.Sprintf("ws://%s:%d", g.host, g.wsPort)
	httpURL := fmt.Sprintf("http://%s:%d", g.host, g.httpPort)

	// Use external URLs if configured (for VS Code port forwarding, etc.)
	if g.externalWSURL != "" {
		wsURL = g.externalWSURL
	}
	if g.externalHTTPURL != "" {
		httpURL = g.externalHTTPURL
	}

	return &PairingInfo{
		WebSocket: wsURL,
		HTTP:      httpURL,
		SessionID: g.sessionID,
		Token:     g.token,
		RepoName:  g.repoName,
	}
}

// GenerateJSON returns the pairing info as JSON.
func (g *QRGenerator) GenerateJSON() (string, error) {
	info := g.GetPairingInfo()
	data, err := json.Marshal(info)
	if err != nil {
		return "", err
	}
	return string(data), nil
}

// GenerateTerminal generates a QR code for terminal display.
func (g *QRGenerator) GenerateTerminal() (string, error) {
	jsonData, err := g.GenerateJSON()
	if err != nil {
		return "", err
	}

	// Generate QR code
	qr, err := qrcode.New(jsonData, qrcode.Medium)
	if err != nil {
		return "", err
	}

	// Convert to terminal-friendly string
	return qr.ToSmallString(false), nil
}

// GeneratePNG generates a PNG image of the QR code.
func (g *QRGenerator) GeneratePNG(size int) ([]byte, error) {
	jsonData, err := g.GenerateJSON()
	if err != nil {
		return nil, err
	}

	return qrcode.Encode(jsonData, qrcode.Medium, size)
}

// PrintToTerminal prints the QR code to the terminal with a border.
func (g *QRGenerator) PrintToTerminal() {
	qrStr, err := g.GenerateTerminal()
	if err != nil {
		fmt.Printf("  [Error generating QR code: %v]\n", err)
		return
	}

	// Add some formatting
	lines := strings.Split(qrStr, "\n")

	fmt.Println()
	fmt.Println("  Scan with cdev mobile app:")
	fmt.Println()

	for _, line := range lines {
		if line != "" {
			fmt.Printf("  %s\n", line)
		}
	}

	fmt.Println()
}

// SimpleTerminalQR generates a simple ASCII QR representation.
// This is a fallback if the QR library isn't available.
func SimpleTerminalQR(data string) string {
	// This is a placeholder - the actual implementation uses go-qrcode
	var sb strings.Builder
	sb.WriteString("┌────────────────────────────┐\n")
	sb.WriteString("│                            │\n")
	sb.WriteString("│   [QR CODE]                │\n")
	sb.WriteString("│                            │\n")
	sb.WriteString("│   Scan with mobile app     │\n")
	sb.WriteString("│                            │\n")
	sb.WriteString("└────────────────────────────┘\n")
	return sb.String()
}

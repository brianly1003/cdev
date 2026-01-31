// Package cmd contains the CLI commands for cdev.
package cmd

import (
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/brianly1003/cdev/internal/config"
	"github.com/brianly1003/cdev/internal/pairing"
	"github.com/google/uuid"
	"github.com/skip2/go-qrcode"
	"github.com/spf13/cobra"
)

var (
	pairJSON        bool
	pairURL         bool
	pairPage        bool
	pairRefresh     bool
	pairExternalURL string
)

// pairCmd displays QR code for mobile pairing.
var pairCmd = &cobra.Command{
	Use:   "pair",
	Short: "Display QR code for mobile app pairing",
	Long: `Display a QR code that can be scanned by the cdev mobile app to connect.

If a cdev server is running, it will use the server's session information.
Otherwise, it generates pairing info based on the configuration.

Examples:
  cdev pair              # Display QR code in terminal
  cdev pair --json       # Output pairing info as JSON
  cdev pair --url        # Output connection URL only
  cdev pair --refresh    # Generate new session ID`,
	RunE: runPair,
}

func init() {
	rootCmd.AddCommand(pairCmd)

	pairCmd.Flags().BoolVar(&pairJSON, "json", false, "output pairing info as JSON")
	pairCmd.Flags().BoolVar(&pairURL, "url", false, "output connection URL only")
	pairCmd.Flags().BoolVar(&pairPage, "page", false, "output pairing page URL (/pair)")
	pairCmd.Flags().BoolVar(&pairRefresh, "refresh", false, "generate new session ID (ignore running server)")
	pairCmd.Flags().StringVar(&pairExternalURL, "external-url", "", "override external URL for pairing output")
}

// serverPairingInfo represents the response from /api/pair/info
type serverPairingInfo struct {
	WebSocket string `json:"ws"`
	HTTP      string `json:"http"`
	SessionID string `json:"session"`
	Token     string `json:"token,omitempty"`
	RepoName  string `json:"repo"`
}

func runPair(cmd *cobra.Command, args []string) error {
	// Load config
	cfg, err := config.Load(cfgFile)
	if err != nil {
		return fmt.Errorf("failed to load config: %w", err)
	}

	var info *pairing.PairingInfo

	// Try to get pairing info from running server (unless --refresh)
	if !pairRefresh {
		serverInfo, err := getPairingFromServer(cfg)
		if err == nil && serverInfo != nil {
			info = &pairing.PairingInfo{
				WebSocket: serverInfo.WebSocket,
				HTTP:      serverInfo.HTTP,
				SessionID: serverInfo.SessionID,
				Token:     serverInfo.Token,
				RepoName:  serverInfo.RepoName,
			}
		}
	}

	// If no server or --refresh, generate new pairing info
	if info == nil {
		info = generatePairingInfo(cfg, pairExternalURL)
		if pairRefresh {
			fmt.Fprintln(os.Stderr, "Generated new session ID (not connected to running server)")
		} else {
			fmt.Fprintln(os.Stderr, "No running cdev server found, using config defaults")
		}
	} else {
		fmt.Fprintln(os.Stderr, "Connected to running cdev server")
	}

	if pairExternalURL != "" {
		applyExternalURL(info, pairExternalURL)
	}

	// Output based on flags
	if pairJSON {
		return outputJSON(info)
	}

	if pairPage {
		return outputPairPage(info)
	}

	if pairURL {
		return outputURL(info)
	}

	return outputQR(info)
}

func getPairingFromServer(cfg *config.Config) (*serverPairingInfo, error) {
	url := fmt.Sprintf("http://%s:%d/api/pair/info", cfg.Server.Host, cfg.Server.Port)

	client := &http.Client{
		Timeout: 2 * time.Second,
	}

	resp, err := client.Get(url)
	if err != nil {
		return nil, err
	}
	defer func() { _ = resp.Body.Close() }()

	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("server returned status %d", resp.StatusCode)
	}

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, err
	}

	var info serverPairingInfo
	if err := json.Unmarshal(body, &info); err != nil {
		return nil, err
	}

	return &info, nil
}

func generatePairingInfo(cfg *config.Config, externalURL string) *pairing.PairingInfo {
	// Generate new session ID
	sessionID := uuid.New().String()

	// Get repo name from current directory
	repoName := "unknown"
	if cwd, err := os.Getwd(); err == nil {
		repoName = filepath.Base(cwd)
	}

	// Create QR generator with unified port
	gen := pairing.NewQRGenerator(
		cfg.Server.Host,
		cfg.Server.Port,
		sessionID,
		repoName,
	)

	// Set external URL if configured
	if externalURL != "" {
		gen.SetExternalURL(externalURL)
	} else if cfg.Server.ExternalURL != "" {
		gen.SetExternalURL(cfg.Server.ExternalURL)
	}

	return gen.GetPairingInfo()
}

func outputJSON(info *pairing.PairingInfo) error {
	data, err := json.MarshalIndent(info, "", "  ")
	if err != nil {
		return err
	}
	fmt.Println(string(data))
	return nil
}

func outputURL(info *pairing.PairingInfo) error {
	fmt.Printf("WebSocket: %s\n", info.WebSocket)
	fmt.Printf("HTTP:      %s\n", info.HTTP)
	return nil
}

func outputPairPage(info *pairing.PairingInfo) error {
	base := strings.TrimRight(info.HTTP, "/")
	fmt.Printf("%s/pair\n", base)
	return nil
}

func outputQR(info *pairing.PairingInfo) error {
	// Generate QR code from info
	jsonData, err := json.Marshal(info)
	if err != nil {
		return err
	}

	fmt.Println()
	fmt.Println("╔════════════════════════════════════════════════════════════╗")
	fmt.Println("║                     Cdev Pairing                           ║")
	fmt.Println("╠════════════════════════════════════════════════════════════╣")
	fmt.Printf("║  WebSocket: %-47s ║\n", truncate(info.WebSocket, 47))
	fmt.Printf("║  HTTP:      %-47s ║\n", truncate(info.HTTP, 47))
	fmt.Printf("║  Session:   %-47s ║\n", truncate(info.SessionID, 47))
	fmt.Printf("║  Repo:      %-47s ║\n", truncate(info.RepoName, 47))
	fmt.Println("╚════════════════════════════════════════════════════════════╝")

	// Generate and print QR code
	qrStr, err := generateQRString(string(jsonData))
	if err != nil {
		return fmt.Errorf("failed to generate QR code: %w", err)
	}

	fmt.Println()
	fmt.Println("  Scan with cdev mobile app:")
	fmt.Println()
	// Indent QR code
	for _, line := range splitLines(qrStr) {
		if line != "" {
			fmt.Printf("  %s\n", line)
		}
	}
	fmt.Println()

	return nil
}

func applyExternalURL(info *pairing.PairingInfo, externalURL string) {
	if info == nil || externalURL == "" {
		return
	}
	base := strings.TrimRight(externalURL, "/")
	info.HTTP = base

	wsURL := base
	if strings.HasPrefix(wsURL, "https://") {
		wsURL = "wss://" + strings.TrimPrefix(wsURL, "https://")
	} else if strings.HasPrefix(wsURL, "http://") {
		wsURL = "ws://" + strings.TrimPrefix(wsURL, "http://")
	}
	info.WebSocket = wsURL + "/ws"
}

func truncate(s string, maxLen int) string {
	if len(s) <= maxLen {
		return s
	}
	return s[:maxLen-3] + "..."
}

func splitLines(s string) []string {
	var lines []string
	start := 0
	for i := 0; i < len(s); i++ {
		if s[i] == '\n' {
			lines = append(lines, s[start:i])
			start = i + 1
		}
	}
	if start < len(s) {
		lines = append(lines, s[start:])
	}
	return lines
}

func generateQRString(data string) (string, error) {
	// Use the go-qrcode library directly
	qr, err := qrcode.New(data, qrcode.Medium)
	if err != nil {
		return "", err
	}
	return qr.ToSmallString(false), nil
}

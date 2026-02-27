// Package cmd contains the CLI commands for cdev.
package cmd

import (
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	neturl "net/url"
	"os"
	"os/exec"
	"runtime"
	"strings"
	"time"

	"github.com/brianly1003/cdev/internal/config"
	"github.com/brianly1003/cdev/internal/pairing"
	"github.com/spf13/cobra"
)

var (
	pairJSON        bool
	pairExternalURL string
)

// pairCmd opens the pairing page for mobile app connection.
var pairCmd = &cobra.Command{
	Use:   "pair",
	Short: "Open pairing page for mobile app connection",
	Long: `Open the pairing page in your browser for the cdev mobile app to connect.

Requires a running cdev server. Start one first with: cdev start

Examples:
  cdev pair                                       # Open pairing page in browser
  cdev pair --json                                # Output pairing info as JSON
  cdev pair --external-url https://<tunnel>       # Override public URL`,
	RunE: runPair,
}

func init() {
	rootCmd.AddCommand(pairCmd)

	pairCmd.Flags().BoolVar(&pairJSON, "json", false, "output pairing info as JSON")
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
	cdevAccessToken := resolveCdevAccessToken(cfg)

	// Check if cdev server is running
	serverInfo, srvErr := getPairingFromServer(cfg, cdevAccessToken)
	if srvErr != nil || serverInfo == nil {
		return fmt.Errorf("cdev server is not running on %s:%d — start it first with: cdev start", cfg.Server.Host, cfg.Server.Port)
	}

	info := &pairing.PairingInfo{
		WebSocket: serverInfo.WebSocket,
		HTTP:      serverInfo.HTTP,
		SessionID: serverInfo.SessionID,
		Token:     serverInfo.Token,
		RepoName:  serverInfo.RepoName,
	}
	fmt.Fprintln(os.Stderr, "Connected to running cdev server")

	if pairExternalURL != "" {
		applyExternalURL(info, pairExternalURL)
	}

	// Output based on flags
	if pairJSON {
		return outputJSON(info)
	}

	// Default: open pairing page in browser
	return openPairPage(info, cdevAccessToken)
}

func getPairingFromServer(cfg *config.Config, cdevAccessToken string) (*serverPairingInfo, error) {
	url := fmt.Sprintf("http://%s:%d/api/pair/info", cfg.Server.Host, cfg.Server.Port)

	client := &http.Client{
		Timeout: 2 * time.Second,
	}

	req, err := http.NewRequest(http.MethodGet, url, nil)
	if err != nil {
		return nil, err
	}
	if cdevAccessToken != "" {
		req.Header.Set("X-Cdev-Token", cdevAccessToken)
	}

	resp, err := client.Do(req)
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

func outputJSON(info *pairing.PairingInfo) error {
	data, err := json.MarshalIndent(info, "", "  ")
	if err != nil {
		return err
	}
	fmt.Println(string(data))
	return nil
}

func openPairPage(info *pairing.PairingInfo, cdevAccessToken string) error {
	pageURL := pairPageURL(info, cdevAccessToken)
	fmt.Fprintf(os.Stderr, "Opening pairing page: %s\n", pageURL)
	return openBrowser(pageURL)
}

// openBrowser opens the given URL in the default browser.
func openBrowser(url string) error {
	switch runtime.GOOS {
	case "darwin":
		return exec.Command("open", url).Start()
	case "windows":
		return exec.Command("cmd", "/c", "start", "", url).Start()
	default: // linux, freebsd, etc.
		return exec.Command("xdg-open", url).Start()
	}
}

func resolveCdevAccessToken(cfg *config.Config) string {
	if cfg != nil {
		if token := strings.TrimSpace(cfg.Security.CdevAccessToken); token != "" {
			return token
		}
	}
	return strings.TrimSpace(os.Getenv("CDEV_ACCESS_TOKEN"))
}

func pairPageURL(info *pairing.PairingInfo, cdevAccessToken string) string {
	pageURL := strings.TrimRight(info.HTTP, "/") + "/pair"
	if cdevAccessToken == "" {
		return pageURL
	}
	return pageURL + "?token=" + neturl.QueryEscape(cdevAccessToken)
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


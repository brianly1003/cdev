// Package cmd contains the CLI commands for cdev.
package cmd

import (
	"fmt"
	"io"
	"net/http"
	"os"
	"time"

	"github.com/brianly1003/cdev/internal/config"
	"github.com/brianly1003/cdev/internal/security"
	"github.com/spf13/cobra"
)

var (
	authLocalOnly bool
)

// authCmd groups authentication utilities.
var authCmd = &cobra.Command{
	Use:   "auth",
	Short: "Authentication utilities",
}

// authResetCmd revokes tokens and clears local auth state.
var authResetCmd = &cobra.Command{
	Use:   "reset",
	Short: "Revoke tokens and clear local auth state",
	RunE:  runAuthReset,
}

func init() {
	rootCmd.AddCommand(authCmd)
	authCmd.AddCommand(authResetCmd)

	authResetCmd.Flags().BoolVar(&authLocalOnly, "local-only", false, "only clear local auth files (skip server revoke)")
}

func runAuthReset(cmd *cobra.Command, args []string) error {
	cfg, err := config.Load(cfgFile)
	if err != nil {
		return fmt.Errorf("failed to load config: %w", err)
	}

	if !authLocalOnly {
		if err := revokeServerTokens(cfg); err != nil {
			fmt.Fprintf(os.Stderr, "warning: failed to revoke tokens on running server: %v\n", err)
		} else {
			fmt.Fprintln(os.Stderr, "revoked tokens on running server")
		}
	}

	removed := 0
	if removeFile(security.DefaultTokenSecretPath()) {
		removed++
	}
	if removeFile(config.DefaultAuthRegistryPath()) {
		removed++
	}

	if removed == 0 {
		fmt.Fprintln(os.Stderr, "no local auth files found")
	} else {
		fmt.Fprintf(os.Stderr, "removed %d local auth file(s)\n", removed)
	}

	return nil
}

func revokeServerTokens(cfg *config.Config) error {
	url := fmt.Sprintf("http://%s:%d/api/pair/refresh", cfg.Server.Host, cfg.Server.Port)

	req, err := http.NewRequest(http.MethodPost, url, nil)
	if err != nil {
		return err
	}

	client := &http.Client{Timeout: 2 * time.Second}
	resp, err := client.Do(req)
	if err != nil {
		return err
	}
	defer func() { _ = resp.Body.Close() }()

	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(resp.Body)
		return fmt.Errorf("server returned status %d: %s", resp.StatusCode, string(body))
	}
	return nil
}

func removeFile(path string) bool {
	if err := os.Remove(path); err != nil {
		if os.IsNotExist(err) {
			return false
		}
		fmt.Fprintf(os.Stderr, "warning: failed to remove %s: %v\n", path, err)
		return false
	}
	return true
}

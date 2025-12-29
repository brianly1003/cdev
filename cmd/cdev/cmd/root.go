// Package cmd contains the CLI commands for cdev.
package cmd

import (
	"fmt"

	"github.com/spf13/cobra"
)

var (
	// Version info (set from main)
	version   = "dev"
	buildTime = "unknown"
	gitCommit = "unknown"

	// Global flags
	cfgFile string
	verbose bool
)

// rootCmd represents the base command when called without any subcommands.
var rootCmd = &cobra.Command{
	Use:   "cdev",
	Short: "Mobile AI Coding Monitor & Controller Agent",
	Long: `cdev is a lightweight daemon that enables remote monitoring and
control of AI coding agents (Claude Code, Codex, Gemini CLI) from mobile devices.

It watches your repository for changes, streams AI agent output in real-time,
and generates git diffs - all accessible via WebSocket from your mobile app.`,
	SilenceUsage: true,
}

// Execute adds all child commands to the root command and sets flags appropriately.
func Execute() error {
	return rootCmd.Execute()
}

// SetVersionInfo sets version information from the main package.
func SetVersionInfo(v, bt, gc string) {
	version = v
	buildTime = bt
	gitCommit = gc
}

func init() {
	cobra.OnInitialize(initConfig)

	// Global flags
	rootCmd.PersistentFlags().StringVar(&cfgFile, "config", "", "config file (default: ./config.yaml or ~/.cdev/config.yaml)")
	rootCmd.PersistentFlags().BoolVarP(&verbose, "verbose", "v", false, "enable verbose output")

	// Add subcommands
	rootCmd.AddCommand(startCmd)
	rootCmd.AddCommand(versionCmd)
	rootCmd.AddCommand(configCmd)
	rootCmd.AddCommand(workspaceCmd)
	rootCmd.AddCommand(workspaceManagerCmd)
}

func initConfig() {
	// Config initialization is handled by each command that needs it
}

// versionCmd displays version information.
var versionCmd = &cobra.Command{
	Use:   "version",
	Short: "Print version information",
	Run: func(cmd *cobra.Command, args []string) {
		fmt.Printf("cdev %s\n", version)
		fmt.Printf("  Build time: %s\n", buildTime)
		fmt.Printf("  Git commit: %s\n", gitCommit)
	},
}


package cmd

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"text/tabwriter"
	"time"

	"github.com/brianly1003/cdev/internal/config"
	"github.com/google/uuid"
	"github.com/spf13/cobra"
)

var (
	workspaceName       string
	workspaceAutoStart  bool
	workspaceConfigFile string
	workspacePort       int
)

// workspaceCmd is the parent command for workspace management
var workspaceCmd = &cobra.Command{
	Use:   "workspace",
	Short: "Manage workspaces",
	Long:  `Manage workspace configurations for multi-repository support.`,
}

// workspaceListCmd lists all configured workspaces
var workspaceListCmd = &cobra.Command{
	Use:   "list",
	Short: "List all workspaces",
	Long:  `List all configured workspaces with their status.`,
	RunE:  runWorkspaceList,
}

// workspaceAddCmd adds a new workspace
var workspaceAddCmd = &cobra.Command{
	Use:   "add <path>",
	Short: "Add a new workspace",
	Long: `Add a new workspace at the specified path.

Example:
  cdev workspace add /path/to/repo --name "My Project"
  cdev workspace add . --auto-start`,
	Args: cobra.ExactArgs(1),
	RunE: runWorkspaceAdd,
}

// workspaceRemoveCmd removes a workspace
var workspaceRemoveCmd = &cobra.Command{
	Use:   "remove <id>",
	Short: "Remove a workspace",
	Long:  `Remove a workspace by ID. Does not delete files, only the configuration.`,
	Args:  cobra.ExactArgs(1),
	RunE:  runWorkspaceRemove,
}

// workspaceStartCmd starts a workspace server
var workspaceStartCmd = &cobra.Command{
	Use:   "start <id>",
	Short: "Start a workspace server",
	Long:  `Start the cdev server for the specified workspace.`,
	Args:  cobra.ExactArgs(1),
	RunE:  runWorkspaceStart,
}

// workspaceStopCmd stops a workspace server
var workspaceStopCmd = &cobra.Command{
	Use:   "stop <id>",
	Short: "Stop a workspace server",
	Long:  `Stop the running cdev server for the specified workspace.`,
	Args:  cobra.ExactArgs(1),
	RunE:  runWorkspaceStop,
}

// workspaceInfoCmd shows workspace details
var workspaceInfoCmd = &cobra.Command{
	Use:   "info <id>",
	Short: "Show workspace details",
	Long:  `Display detailed information about a workspace.`,
	Args:  cobra.ExactArgs(1),
	RunE:  runWorkspaceInfo,
}

// workspaceDiscoverCmd discovers Git repositories
var workspaceDiscoverCmd = &cobra.Command{
	Use:   "discover",
	Short: "Discover Git repositories",
	Long:  `Scan common directories for Git repositories and let you choose which to add.`,
	RunE:  runWorkspaceDiscover,
}

func init() {
	// Add workspace subcommands
	workspaceCmd.AddCommand(workspaceListCmd)
	workspaceCmd.AddCommand(workspaceAddCmd)
	workspaceCmd.AddCommand(workspaceRemoveCmd)
	workspaceCmd.AddCommand(workspaceStartCmd)
	workspaceCmd.AddCommand(workspaceStopCmd)
	workspaceCmd.AddCommand(workspaceInfoCmd)
	workspaceCmd.AddCommand(workspaceDiscoverCmd)

	// Flags for 'workspace add'
	workspaceAddCmd.Flags().StringVar(&workspaceName, "name", "", "workspace name (default: directory name)")
	workspaceAddCmd.Flags().BoolVar(&workspaceAutoStart, "auto-start", false, "start workspace on manager launch")
	workspaceAddCmd.Flags().StringVar(&workspaceConfigFile, "config", "", "workspace-specific config file")
	workspaceAddCmd.Flags().IntVar(&workspacePort, "port", 0, "workspace port (0 = auto-allocate)")
}

func runWorkspaceList(cmd *cobra.Command, args []string) error {
	cfg, err := config.LoadWorkspaces("")
	if err != nil {
		return fmt.Errorf("failed to load workspaces config: %w", err)
	}

	if len(cfg.Workspaces) == 0 {
		fmt.Println("No workspaces configured.")
		fmt.Println("\nAdd a workspace with: cdev workspace add <path>")
		return nil
	}

	// Create table writer
	w := tabwriter.NewWriter(os.Stdout, 0, 0, 3, ' ', 0)
	_, _ = fmt.Fprintln(w, "ID\tNAME\tPATH\tPORT\tAUTO-START\tSTATUS")
	_, _ = fmt.Fprintln(w, "--\t----\t----\t----\t----------\t------")

	// Get status from workspace manager if running
	statuses := getWorkspaceStatuses()

	for _, ws := range cfg.Workspaces {
		status := statuses[ws.ID]
		if status == "" {
			status = "stopped"
		}

		autoStart := "no"
		if ws.AutoStart {
			autoStart = "yes"
		}

		_, _ = fmt.Fprintf(w, "%s\t%s\t%s\t%d\t%s\t%s\n",
			ws.ID,
			ws.Name,
			truncatePath(ws.Path, 40),
			ws.Port,
			autoStart,
			status,
		)
	}

	_ = w.Flush()
	return nil
}

func runWorkspaceAdd(cmd *cobra.Command, args []string) error {
	path := args[0]

	// Resolve absolute path
	absPath, err := filepath.Abs(path)
	if err != nil {
		return fmt.Errorf("invalid path: %w", err)
	}

	// Check if path exists
	if _, err := os.Stat(absPath); err != nil {
		return fmt.Errorf("path does not exist: %s", absPath)
	}

	// Load workspaces config
	cfg, err := config.LoadWorkspaces("")
	if err != nil {
		return fmt.Errorf("failed to load workspaces config: %w", err)
	}

	// Determine workspace name
	name := workspaceName
	if name == "" {
		name = filepath.Base(absPath)
	}

	// Check for duplicates
	for _, ws := range cfg.Workspaces {
		if ws.Path == absPath {
			return fmt.Errorf("workspace already exists at path: %s", absPath)
		}
		if ws.Name == name {
			return fmt.Errorf("workspace with name '%s' already exists", name)
		}
	}

	// Allocate port if not specified
	port := workspacePort
	if port == 0 {
		port = allocatePort(cfg)
	}

	// Create workspace definition
	ws := config.WorkspaceDefinition{
		ID:           generateWorkspaceID(),
		Name:         name,
		Path:         absPath,
		Port:         port,
		AutoStart:    workspaceAutoStart,
		ConfigFile:   workspaceConfigFile,
		CreatedAt:    time.Now(),
		LastAccessed: time.Now(),
	}

	// Add to config
	cfg.Workspaces = append(cfg.Workspaces, ws)

	// Save config
	configDir, _ := config.GetConfigDir()
	if err := config.SaveWorkspaces(filepath.Join(configDir, "workspaces.yaml"), cfg); err != nil {
		return fmt.Errorf("failed to save workspaces config: %w", err)
	}

	fmt.Printf("Added workspace '%s' (ID: %s) at port %d\n", ws.Name, ws.ID, ws.Port)
	fmt.Printf("Start it with: cdev workspace start %s\n", ws.ID)

	return nil
}

func runWorkspaceRemove(cmd *cobra.Command, args []string) error {
	id := args[0]

	cfg, err := config.LoadWorkspaces("")
	if err != nil {
		return fmt.Errorf("failed to load workspaces config: %w", err)
	}

	// Find and remove workspace
	found := false
	newWorkspaces := []config.WorkspaceDefinition{}
	var removedName string
	for _, ws := range cfg.Workspaces {
		if ws.ID == id || ws.Name == id {
			found = true
			removedName = ws.Name
			fmt.Printf("Removing workspace '%s' (ID: %s)\n", ws.Name, ws.ID)
		} else {
			newWorkspaces = append(newWorkspaces, ws)
		}
	}

	if !found {
		return fmt.Errorf("workspace not found: %s", id)
	}

	cfg.Workspaces = newWorkspaces

	// Save config
	configDir, _ := config.GetConfigDir()
	if err := config.SaveWorkspaces(filepath.Join(configDir, "workspaces.yaml"), cfg); err != nil {
		return fmt.Errorf("failed to save workspaces config: %w", err)
	}

	fmt.Printf("Workspace '%s' removed successfully\n", removedName)
	return nil
}

func runWorkspaceStart(cmd *cobra.Command, args []string) error {
	id := args[0]

	// Load config to get manager address
	cfg, err := config.LoadWorkspaces("")
	if err != nil {
		return err
	}

	// Call workspace manager API
	url := fmt.Sprintf("http://%s:%d/api/workspaces/%s/start", cfg.Manager.Host, cfg.Manager.Port, id)
	resp, err := http.Post(url, "application/json", nil)
	if err != nil {
		return fmt.Errorf("failed to connect to workspace manager: %w\nStart it with: cdev workspace-manager start", err)
	}
	defer func() { _ = resp.Body.Close() }()

	body, _ := io.ReadAll(resp.Body)

	if resp.StatusCode != http.StatusOK {
		var errResp map[string]interface{}
		if err := json.Unmarshal(body, &errResp); err == nil {
			if msg, ok := errResp["error"].(string); ok {
				return fmt.Errorf("failed to start workspace: %s", msg)
			}
		}
		return fmt.Errorf("failed to start workspace: HTTP %d", resp.StatusCode)
	}

	var result map[string]interface{}
	if err := json.Unmarshal(body, &result); err != nil {
		return fmt.Errorf("failed to parse response: %w", err)
	}

	fmt.Printf("✓ Workspace started successfully\n")
	if name, ok := result["name"].(string); ok {
		fmt.Printf("  Name: %s\n", name)
	}
	if port, ok := result["port"].(float64); ok {
		fmt.Printf("  Port: %d\n", int(port))
	}
	if status, ok := result["status"].(string); ok {
		fmt.Printf("  Status: %s\n", status)
	}

	return nil
}

func runWorkspaceStop(cmd *cobra.Command, args []string) error {
	id := args[0]

	// Load config to get manager address
	cfg, err := config.LoadWorkspaces("")
	if err != nil {
		return err
	}

	// Call workspace manager API
	url := fmt.Sprintf("http://%s:%d/api/workspaces/%s/stop", cfg.Manager.Host, cfg.Manager.Port, id)
	resp, err := http.Post(url, "application/json", nil)
	if err != nil {
		return fmt.Errorf("failed to connect to workspace manager: %w\nStart it with: cdev workspace-manager start", err)
	}
	defer func() { _ = resp.Body.Close() }()

	body, _ := io.ReadAll(resp.Body)

	if resp.StatusCode != http.StatusOK {
		var errResp map[string]interface{}
		if err := json.Unmarshal(body, &errResp); err == nil {
			if msg, ok := errResp["error"].(string); ok {
				return fmt.Errorf("failed to stop workspace: %s", msg)
			}
		}
		return fmt.Errorf("failed to stop workspace: HTTP %d", resp.StatusCode)
	}

	var result map[string]interface{}
	if err := json.Unmarshal(body, &result); err != nil {
		return fmt.Errorf("failed to parse response: %w", err)
	}

	fmt.Printf("✓ Workspace stopped successfully\n")
	if name, ok := result["name"].(string); ok {
		fmt.Printf("  Name: %s\n", name)
	}
	if status, ok := result["status"].(string); ok {
		fmt.Printf("  Status: %s\n", status)
	}

	return nil
}

func runWorkspaceInfo(cmd *cobra.Command, args []string) error {
	id := args[0]

	cfg, err := config.LoadWorkspaces("")
	if err != nil {
		return err
	}

	ws := findWorkspace(cfg, id)
	if ws == nil {
		return fmt.Errorf("workspace not found: %s", id)
	}

	fmt.Printf("Workspace: %s\n", ws.Name)
	fmt.Printf("ID:        %s\n", ws.ID)
	fmt.Printf("Path:      %s\n", ws.Path)
	fmt.Printf("Port:      %d\n", ws.Port)
	fmt.Printf("AutoStart: %t\n", ws.AutoStart)
	fmt.Printf("Created:   %s\n", ws.CreatedAt.Format(time.RFC3339))
	fmt.Printf("Accessed:  %s\n", ws.LastAccessed.Format(time.RFC3339))
	if ws.ConfigFile != "" {
		fmt.Printf("Config:    %s\n", ws.ConfigFile)
	}

	return nil
}

func runWorkspaceDiscover(cmd *cobra.Command, args []string) error {
	fmt.Println("Discovering Git repositories...")

	// Load config to get manager address
	cfg, err := config.LoadWorkspaces("")
	if err != nil {
		return err
	}

	// Call workspace manager API
	url := fmt.Sprintf("http://%s:%d/api/workspaces/discover", cfg.Manager.Host, cfg.Manager.Port)
	resp, err := http.Post(url, "application/json", bytes.NewReader([]byte("{}")))
	if err != nil {
		return fmt.Errorf("failed to connect to workspace manager: %w\nStart it with: cdev workspace-manager start", err)
	}
	defer func() { _ = resp.Body.Close() }()

	body, _ := io.ReadAll(resp.Body)

	if resp.StatusCode != http.StatusOK {
		var errResp map[string]interface{}
		if err := json.Unmarshal(body, &errResp); err == nil {
			if msg, ok := errResp["error"].(string); ok {
				return fmt.Errorf("discovery failed: %s", msg)
			}
		}
		return fmt.Errorf("discovery failed: HTTP %d", resp.StatusCode)
	}

	var result struct {
		Repositories []struct {
			Path         string `json:"path"`
			Name         string `json:"name"`
			RemoteURL    string `json:"remote_url"`
			IsConfigured bool   `json:"is_configured"`
		} `json:"repositories"`
		Count int `json:"count"`
	}

	if err := json.Unmarshal(body, &result); err != nil {
		return fmt.Errorf("failed to parse response: %w", err)
	}

	fmt.Printf("\nFound %d Git repositories:\n\n", result.Count)

	if result.Count == 0 {
		fmt.Println("No Git repositories found in ~/Projects, ~/Code, ~/Desktop")
		fmt.Println("Add a workspace manually with: cdev workspace add <path>")
		return nil
	}

	w := tabwriter.NewWriter(os.Stdout, 0, 0, 3, ' ', 0)
	_, _ = fmt.Fprintln(w, "NAME\tPATH\tREMOTE\tSTATUS")
	_, _ = fmt.Fprintln(w, "----\t----\t------\t------")

	for _, repo := range result.Repositories {
		status := "not configured"
		if repo.IsConfigured {
			status = "configured"
		}

		remote := repo.RemoteURL
		if len(remote) > 50 {
			remote = remote[:47] + "..."
		}

		_, _ = fmt.Fprintf(w, "%s\t%s\t%s\t%s\n",
			repo.Name,
			repo.Path,
			remote,
			status,
		)
	}
	_ = w.Flush()

	fmt.Println("\nTo add a repository as a workspace:")
	fmt.Println("  cdev workspace add <path> --name <name>")

	return nil
}

// Helper functions

func findWorkspace(cfg *config.WorkspacesConfig, id string) *config.WorkspaceDefinition {
	for i := range cfg.Workspaces {
		if cfg.Workspaces[i].ID == id || cfg.Workspaces[i].Name == id {
			return &cfg.Workspaces[i]
		}
	}
	return nil
}

func allocatePort(cfg *config.WorkspacesConfig) int {
	usedPorts := make(map[int]bool)
	for _, ws := range cfg.Workspaces {
		usedPorts[ws.Port] = true
	}

	for port := cfg.Manager.PortRangeStart; port <= cfg.Manager.PortRangeEnd; port++ {
		if !usedPorts[port] {
			return port
		}
	}

	return cfg.Manager.PortRangeStart // Fallback
}

func generateWorkspaceID() string {
	return "ws-" + uuid.New().String()[:8]
}

func truncatePath(path string, maxLen int) string {
	if len(path) <= maxLen {
		return path
	}
	return "..." + path[len(path)-maxLen+3:]
}

func getWorkspaceStatuses() map[string]string {
	// TODO: Query workspace manager API for statuses
	// For now, return empty map (all workspaces show as "stopped")
	return make(map[string]string)
}

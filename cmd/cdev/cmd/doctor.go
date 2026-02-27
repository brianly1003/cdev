// Package cmd contains the CLI commands for cdev.
package cmd

import (
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"

	"github.com/brianly1003/cdev/internal/config"
	"github.com/brianly1003/cdev/internal/security"
	"github.com/spf13/cobra"
	"gopkg.in/yaml.v3"
)

var (
	doctorJSON        bool
	doctorStrict      bool
	doctorHTTPTimeout int
)

type doctorStatus string

const (
	doctorStatusOK   doctorStatus = "ok"
	doctorStatusWarn doctorStatus = "warn"
	doctorStatusFail doctorStatus = "fail"
)

type doctorCheck struct {
	ID          string                 `json:"id"`
	Status      doctorStatus           `json:"status"`
	Message     string                 `json:"message"`
	Details     map[string]interface{} `json:"details,omitempty"`
	Remediation string                 `json:"remediation,omitempty"`
}

type doctorSummary struct {
	Total int `json:"total"`
	OK    int `json:"ok"`
	Warn  int `json:"warn"`
	Fail  int `json:"fail"`
}

type doctorReport struct {
	Version      string        `json:"version"`
	GeneratedAt  string        `json:"generated_at"`
	Overall      doctorStatus  `json:"overall_status"`
	Summary      doctorSummary `json:"summary"`
	Checks       []doctorCheck `json:"checks"`
	SearchConfig []string      `json:"config_search_paths,omitempty"`
}

var doctorCmd = &cobra.Command{
	Use:   "doctor",
	Short: "Run local diagnostics with remediation hints",
	Long: `Run read-only diagnostics against local cdev setup and print actionable hints.

By default the output is human-readable text.
Use --json for machine-readable output.`,
	RunE: runDoctor,
}

func init() {
	rootCmd.AddCommand(doctorCmd)

	doctorCmd.Flags().BoolVar(&doctorJSON, "json", false, "output machine-readable JSON")
	doctorCmd.Flags().BoolVar(&doctorStrict, "strict", false, "return non-zero on warnings")
	doctorCmd.Flags().IntVar(&doctorHTTPTimeout, "http-timeout", 2, "health endpoint timeout in seconds")
}

func runDoctor(cmd *cobra.Command, args []string) error {
	report := collectDoctorReport()

	if doctorJSON {
		if err := printDoctorJSON(report); err != nil {
			return err
		}
	} else {
		printDoctorText(report)
	}

	if report.Summary.Fail > 0 {
		return fmt.Errorf("doctor found %d failing check(s)", report.Summary.Fail)
	}
	if doctorStrict && report.Summary.Warn > 0 {
		return fmt.Errorf("doctor strict mode failed with %d warning(s)", report.Summary.Warn)
	}
	return nil
}

func collectDoctorReport() doctorReport {
	checks := make([]doctorCheck, 0, 16)

	cfg := defaultDoctorConfig()
	loadedCfg, cfgCheck := checkConfigLoad(cfgFile)
	checks = append(checks, cfgCheck)
	if loadedCfg != nil {
		cfg = loadedCfg
	}

	checks = append(checks, checkConfigDirectory())
	checks = append(checks, checkOptionalJSONFile(
		"auth.registry_file",
		config.DefaultAuthRegistryPath(),
		"Auth registry file is readable",
		"Run `cdev start` once or pair from cdev-ios to initialize auth registry.",
	))
	checks = append(checks, checkOptionalJSONFile(
		"auth.token_secret",
		security.DefaultTokenSecretPath(),
		"Token secret file is readable",
		"Run `cdev start` once to generate token secret.",
	))
	checks = append(checks, checkOptionalYAMLFile(
		"workspace.registry_file",
		config.DefaultWorkspacesPath(),
		"Workspace registry file is readable",
		"Run `cdev start` or add a workspace from cdev-ios to generate workspaces config.",
	))
	checks = append(checks, checkRepositoryPath(cfg.Repository.Path))

	checks = append(checks, checkCommandBinary("runtime.claude_cli", cfg.Claude.Command, true))
	checks = append(checks, checkCommandBinary("runtime.codex_cli", "codex", false))
	checks = append(checks, checkCommandBinary("runtime.git", cfg.Git.Command, true))

	checks = append(checks, checkDirectoryExists(
		"sessions.claude_dir",
		filepath.Join(userHomeDir(), ".claude", "projects"),
		"Claude sessions directory exists",
		"No Claude sessions found yet. Start a Claude run to create it.",
	))
	checks = append(checks, checkDirectoryExists(
		"sessions.codex_dir",
		filepath.Join(userHomeDir(), ".codex", "sessions"),
		"Codex sessions directory exists",
		"No Codex sessions found yet. Start a Codex run to create it.",
	))

	checks = append(checks, checkHealthEndpoint(cfg.Server.Host, cfg.Server.Port, doctorHTTPTimeout))

	summary := summarizeDoctorChecks(checks)
	return doctorReport{
		Version:      "1.0",
		GeneratedAt:  time.Now().UTC().Format(time.RFC3339),
		Overall:      overallStatus(summary),
		Summary:      summary,
		Checks:       checks,
		SearchConfig: configSearchPaths(cfgFile),
	}
}

func checkConfigLoad(path string) (*config.Config, doctorCheck) {
	cfg, err := config.Load(path)
	searchPaths := configSearchPaths(path)
	if err != nil {
		return nil, doctorCheck{
			ID:      "config.load",
			Status:  doctorStatusFail,
			Message: fmt.Sprintf("Failed to load config: %v", err),
			Details: map[string]interface{}{
				"config_path":  strings.TrimSpace(path),
				"search_paths": searchPaths,
			},
			Remediation: "Fix the config file syntax, or run `cdev config init --force` to regenerate defaults.",
		}
	}

	source := findFirstExistingPath(searchPaths)
	msg := "Configuration loaded using built-in defaults and environment overrides"
	if source != "" {
		msg = "Configuration loaded successfully"
	}

	return cfg, doctorCheck{
		ID:      "config.load",
		Status:  doctorStatusOK,
		Message: msg,
		Details: map[string]interface{}{
			"loaded_from":  source,
			"search_paths": searchPaths,
		},
	}
}

func checkConfigDirectory() doctorCheck {
	dir, err := config.GetConfigDir()
	if err != nil {
		return doctorCheck{
			ID:          "config.directory",
			Status:      doctorStatusFail,
			Message:     fmt.Sprintf("Failed to resolve config directory: %v", err),
			Remediation: "Verify your HOME environment and filesystem permissions.",
		}
	}

	info, statErr := os.Stat(dir)
	if statErr != nil {
		if os.IsNotExist(statErr) {
			return doctorCheck{
				ID:      "config.directory",
				Status:  doctorStatusWarn,
				Message: "Config directory does not exist yet",
				Details: map[string]interface{}{
					"path": dir,
				},
				Remediation: "Run `cdev config init` to create initial local configuration.",
			}
		}
		return doctorCheck{
			ID:      "config.directory",
			Status:  doctorStatusFail,
			Message: fmt.Sprintf("Failed to access config directory: %v", statErr),
			Details: map[string]interface{}{
				"path": dir,
			},
			Remediation: "Fix directory permissions or create the directory manually.",
		}
	}

	if !info.IsDir() {
		return doctorCheck{
			ID:      "config.directory",
			Status:  doctorStatusFail,
			Message: "Config path exists but is not a directory",
			Details: map[string]interface{}{
				"path": dir,
			},
			Remediation: "Remove the file and recreate directory with `mkdir -p ~/.cdev`.",
		}
	}

	return doctorCheck{
		ID:      "config.directory",
		Status:  doctorStatusOK,
		Message: "Config directory is available",
		Details: map[string]interface{}{
			"path": dir,
		},
	}
}

func checkOptionalJSONFile(id, path, okMessage, missingRemediation string) doctorCheck {
	content, err := os.ReadFile(path)
	if err != nil {
		if os.IsNotExist(err) {
			return doctorCheck{
				ID:      id,
				Status:  doctorStatusWarn,
				Message: "File not found",
				Details: map[string]interface{}{
					"path": path,
				},
				Remediation: missingRemediation,
			}
		}
		return doctorCheck{
			ID:      id,
			Status:  doctorStatusFail,
			Message: fmt.Sprintf("Failed to read file: %v", err),
			Details: map[string]interface{}{
				"path": path,
			},
			Remediation: "Check file permissions and ownership.",
		}
	}

	var payload interface{}
	if err := json.Unmarshal(content, &payload); err != nil {
		return doctorCheck{
			ID:      id,
			Status:  doctorStatusFail,
			Message: fmt.Sprintf("Invalid JSON format: %v", err),
			Details: map[string]interface{}{
				"path": path,
			},
			Remediation: "Backup and recreate the file by restarting cdev.",
		}
	}

	return doctorCheck{
		ID:      id,
		Status:  doctorStatusOK,
		Message: okMessage,
		Details: map[string]interface{}{
			"path": path,
		},
	}
}

func checkOptionalYAMLFile(id, path, okMessage, missingRemediation string) doctorCheck {
	content, err := os.ReadFile(path)
	if err != nil {
		if os.IsNotExist(err) {
			return doctorCheck{
				ID:      id,
				Status:  doctorStatusWarn,
				Message: "File not found",
				Details: map[string]interface{}{
					"path": path,
				},
				Remediation: missingRemediation,
			}
		}
		return doctorCheck{
			ID:      id,
			Status:  doctorStatusFail,
			Message: fmt.Sprintf("Failed to read file: %v", err),
			Details: map[string]interface{}{
				"path": path,
			},
			Remediation: "Check file permissions and ownership.",
		}
	}

	var payload interface{}
	if err := yaml.Unmarshal(content, &payload); err != nil {
		return doctorCheck{
			ID:      id,
			Status:  doctorStatusFail,
			Message: fmt.Sprintf("Invalid YAML format: %v", err),
			Details: map[string]interface{}{
				"path": path,
			},
			Remediation: "Fix YAML syntax errors in the file or regenerate it.",
		}
	}

	return doctorCheck{
		ID:      id,
		Status:  doctorStatusOK,
		Message: okMessage,
		Details: map[string]interface{}{
			"path": path,
		},
	}
}

func checkRepositoryPath(path string) doctorCheck {
	if strings.TrimSpace(path) == "" {
		return doctorCheck{
			ID:      "repository.path",
			Status:  doctorStatusOK,
			Message: "repository.path is empty (multi-workspace mode)",
		}
	}

	info, err := os.Stat(path)
	if err != nil {
		if os.IsNotExist(err) {
			return doctorCheck{
				ID:      "repository.path",
				Status:  doctorStatusFail,
				Message: "Configured repository path does not exist",
				Details: map[string]interface{}{
					"path": path,
				},
				Remediation: "Update `repository.path` in config or run cdev from a valid workspace.",
			}
		}
		return doctorCheck{
			ID:      "repository.path",
			Status:  doctorStatusFail,
			Message: fmt.Sprintf("Failed to inspect repository path: %v", err),
			Details: map[string]interface{}{
				"path": path,
			},
			Remediation: "Check filesystem permissions and path validity.",
		}
	}

	if !info.IsDir() {
		return doctorCheck{
			ID:      "repository.path",
			Status:  doctorStatusFail,
			Message: "Configured repository path is not a directory",
			Details: map[string]interface{}{
				"path": path,
			},
			Remediation: "Set `repository.path` to a directory path.",
		}
	}

	return doctorCheck{
		ID:      "repository.path",
		Status:  doctorStatusOK,
		Message: "Configured repository path is valid",
		Details: map[string]interface{}{
			"path": path,
		},
	}
}

func checkCommandBinary(id, command string, recommended bool) doctorCheck {
	execName := extractCommandName(command)
	if execName == "" {
		return doctorCheck{
			ID:          id,
			Status:      doctorStatusFail,
			Message:     "Command is empty",
			Remediation: "Set the command in config to a valid executable name or absolute path.",
		}
	}

	resolved, err := exec.LookPath(execName)
	if err != nil {
		status := doctorStatusWarn
		remediation := fmt.Sprintf("Install `%s` and ensure it is available in PATH.", execName)
		if recommended {
			status = doctorStatusFail
			remediation = fmt.Sprintf("Install `%s` or update config to a valid command path.", execName)
		}
		return doctorCheck{
			ID:      id,
			Status:  status,
			Message: fmt.Sprintf("Command not found in PATH: %s", execName),
			Details: map[string]interface{}{
				"configured": command,
			},
			Remediation: remediation,
		}
	}

	return doctorCheck{
		ID:      id,
		Status:  doctorStatusOK,
		Message: "Command is available",
		Details: map[string]interface{}{
			"configured": command,
			"resolved":   resolved,
		},
	}
}

func checkDirectoryExists(id, path, okMessage, missingRemediation string) doctorCheck {
	info, err := os.Stat(path)
	if err != nil {
		if os.IsNotExist(err) {
			return doctorCheck{
				ID:      id,
				Status:  doctorStatusWarn,
				Message: "Directory not found",
				Details: map[string]interface{}{
					"path": path,
				},
				Remediation: missingRemediation,
			}
		}
		return doctorCheck{
			ID:      id,
			Status:  doctorStatusFail,
			Message: fmt.Sprintf("Failed to read directory: %v", err),
			Details: map[string]interface{}{
				"path": path,
			},
			Remediation: "Check filesystem permissions.",
		}
	}

	if !info.IsDir() {
		return doctorCheck{
			ID:      id,
			Status:  doctorStatusFail,
			Message: "Path exists but is not a directory",
			Details: map[string]interface{}{
				"path": path,
			},
			Remediation: "Remove the file and create the directory path.",
		}
	}

	return doctorCheck{
		ID:      id,
		Status:  doctorStatusOK,
		Message: okMessage,
		Details: map[string]interface{}{
			"path": path,
		},
	}
}

func checkHealthEndpoint(host string, port, timeoutSeconds int) doctorCheck {
	if strings.TrimSpace(host) == "" {
		host = "127.0.0.1"
	}
	if port <= 0 {
		port = 16180
	}
	if timeoutSeconds <= 0 {
		timeoutSeconds = 2
	}

	url := fmt.Sprintf("http://%s:%d/health", host, port)
	client := &http.Client{Timeout: time.Duration(timeoutSeconds) * time.Second}

	resp, err := client.Get(url)
	if err != nil {
		return doctorCheck{
			ID:      "server.health_endpoint",
			Status:  doctorStatusWarn,
			Message: fmt.Sprintf("Health endpoint is not reachable: %v", err),
			Details: map[string]interface{}{
				"url": url,
			},
			Remediation: "Start cdev with `cdev start` and verify host/port configuration.",
		}
	}
	defer func() { _ = resp.Body.Close() }()

	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(io.LimitReader(resp.Body, 512))
		return doctorCheck{
			ID:      "server.health_endpoint",
			Status:  doctorStatusFail,
			Message: fmt.Sprintf("Health endpoint returned non-200 status: %d", resp.StatusCode),
			Details: map[string]interface{}{
				"url":         url,
				"status_code": resp.StatusCode,
				"body":        strings.TrimSpace(string(body)),
			},
			Remediation: "Check server logs (`cdev start -v`) to diagnose HTTP startup issues.",
		}
	}

	return doctorCheck{
		ID:      "server.health_endpoint",
		Status:  doctorStatusOK,
		Message: "Health endpoint is reachable",
		Details: map[string]interface{}{
			"url":         url,
			"status_code": resp.StatusCode,
		},
	}
}

func summarizeDoctorChecks(checks []doctorCheck) doctorSummary {
	summary := doctorSummary{Total: len(checks)}
	for _, check := range checks {
		switch check.Status {
		case doctorStatusOK:
			summary.OK++
		case doctorStatusWarn:
			summary.Warn++
		case doctorStatusFail:
			summary.Fail++
		}
	}
	return summary
}

func overallStatus(summary doctorSummary) doctorStatus {
	if summary.Fail > 0 {
		return doctorStatusFail
	}
	if summary.Warn > 0 {
		return doctorStatusWarn
	}
	return doctorStatusOK
}

func printDoctorJSON(report doctorReport) error {
	encoder := json.NewEncoder(os.Stdout)
	encoder.SetIndent("", "  ")
	return encoder.Encode(report)
}

func printDoctorText(report doctorReport) {
	fmt.Printf("cdev doctor v%s\n", report.Version)
	fmt.Printf("generated_at: %s\n", report.GeneratedAt)
	fmt.Printf("overall: %s  (ok=%d warn=%d fail=%d total=%d)\n\n",
		strings.ToUpper(string(report.Overall)),
		report.Summary.OK,
		report.Summary.Warn,
		report.Summary.Fail,
		report.Summary.Total,
	)

	for _, check := range report.Checks {
		label := "[OK]"
		if check.Status == doctorStatusWarn {
			label = "[WARN]"
		}
		if check.Status == doctorStatusFail {
			label = "[FAIL]"
		}

		fmt.Printf("%s %s: %s\n", label, check.ID, check.Message)
		if check.Remediation != "" && check.Status != doctorStatusOK {
			fmt.Printf("  fix: %s\n", check.Remediation)
		}
	}

	fmt.Println()
	fmt.Println("Tip: run `cdev doctor --json` for machine-readable output.")
}

func defaultDoctorConfig() *config.Config {
	return &config.Config{
		Server: config.ServerConfig{
			Host: "127.0.0.1",
			Port: 16180,
		},
		Claude: config.ClaudeConfig{
			Command: "claude",
		},
		Git: config.GitConfig{
			Command: "git",
		},
	}
}

func configSearchPaths(explicit string) []string {
	if strings.TrimSpace(explicit) != "" {
		return []string{explicit}
	}

	home := userHomeDir()
	return []string{
		filepath.Join(".", "config.yaml"),
		filepath.Join(home, ".cdev", "config.yaml"),
		"/etc/cdev/config.yaml",
	}
}

func findFirstExistingPath(paths []string) string {
	for _, candidate := range paths {
		if strings.TrimSpace(candidate) == "" {
			continue
		}
		if _, err := os.Stat(candidate); err == nil {
			return candidate
		}
	}
	return ""
}

func extractCommandName(command string) string {
	parts := strings.Fields(strings.TrimSpace(command))
	if len(parts) == 0 {
		return ""
	}
	return parts[0]
}

func userHomeDir() string {
	home, err := os.UserHomeDir()
	if err != nil {
		return "."
	}
	return home
}

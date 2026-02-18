package cmd

import "testing"

func TestExtractCommandName(t *testing.T) {
	tests := []struct {
		name    string
		input   string
		wantCmd string
	}{
		{name: "empty", input: "", wantCmd: ""},
		{name: "simple", input: "claude", wantCmd: "claude"},
		{name: "with flags", input: "claude --version", wantCmd: "claude"},
		{name: "with spaces", input: "  codex   resume  ", wantCmd: "codex"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := extractCommandName(tt.input)
			if got != tt.wantCmd {
				t.Fatalf("extractCommandName(%q) = %q, want %q", tt.input, got, tt.wantCmd)
			}
		})
	}
}

func TestSummarizeDoctorChecks(t *testing.T) {
	checks := []doctorCheck{
		{ID: "a", Status: doctorStatusOK},
		{ID: "b", Status: doctorStatusWarn},
		{ID: "c", Status: doctorStatusFail},
		{ID: "d", Status: doctorStatusOK},
	}

	summary := summarizeDoctorChecks(checks)
	if summary.Total != 4 || summary.OK != 2 || summary.Warn != 1 || summary.Fail != 1 {
		t.Fatalf("unexpected summary: %+v", summary)
	}
}

func TestOverallStatus(t *testing.T) {
	tests := []struct {
		name    string
		summary doctorSummary
		want    doctorStatus
	}{
		{
			name:    "all ok",
			summary: doctorSummary{Total: 2, OK: 2, Warn: 0, Fail: 0},
			want:    doctorStatusOK,
		},
		{
			name:    "warn only",
			summary: doctorSummary{Total: 2, OK: 1, Warn: 1, Fail: 0},
			want:    doctorStatusWarn,
		},
		{
			name:    "fail takes precedence",
			summary: doctorSummary{Total: 3, OK: 1, Warn: 1, Fail: 1},
			want:    doctorStatusFail,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := overallStatus(tt.summary)
			if got != tt.want {
				t.Fatalf("overallStatus(%+v) = %q, want %q", tt.summary, got, tt.want)
			}
		})
	}
}

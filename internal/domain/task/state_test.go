package task

import (
	"testing"
)

func TestValidTransitions(t *testing.T) {
	tests := []struct {
		from    Status
		to      Status
		allowed bool
	}{
		// Pending transitions
		{StatusPending, StatusPlanning, true},
		{StatusPending, StatusRunning, true},
		{StatusPending, StatusFailed, true},
		{StatusPending, StatusCompleted, false},
		{StatusPending, StatusValidating, false},

		// Planning transitions
		{StatusPlanning, StatusRunning, true},
		{StatusPlanning, StatusFailed, true},
		{StatusPlanning, StatusCompleted, false},

		// Running transitions
		{StatusRunning, StatusValidating, true},
		{StatusRunning, StatusFailed, true},
		{StatusRunning, StatusStuck, true},
		{StatusRunning, StatusCompleted, false},

		// Validating transitions
		{StatusValidating, StatusAwaitingApproval, true},
		{StatusValidating, StatusCompleted, true},
		{StatusValidating, StatusFailed, true},
		{StatusValidating, StatusRunning, true},
		{StatusValidating, StatusPending, false},

		// AwaitingApproval transitions
		{StatusAwaitingApproval, StatusCompleted, true},
		{StatusAwaitingApproval, StatusRunning, true},
		{StatusAwaitingApproval, StatusFailed, true},
		{StatusAwaitingApproval, StatusPending, false},

		// Completed is terminal
		{StatusCompleted, StatusRunning, false},
		{StatusCompleted, StatusPending, false},
		{StatusCompleted, StatusFailed, false},

		// Failed can retry
		{StatusFailed, StatusPending, true},
		{StatusFailed, StatusRunning, true},
		{StatusFailed, StatusCompleted, false},

		// Stuck can retry or fail
		{StatusStuck, StatusPending, true},
		{StatusStuck, StatusRunning, true},
		{StatusStuck, StatusFailed, true},
		{StatusStuck, StatusCompleted, false},
	}

	for _, tt := range tests {
		name := string(tt.from) + "_to_" + string(tt.to)
		t.Run(name, func(t *testing.T) {
			result := tt.from.CanTransitionTo(tt.to)
			if result != tt.allowed {
				t.Errorf("CanTransitionTo(%s, %s) = %v, want %v", tt.from, tt.to, result, tt.allowed)
			}
		})
	}
}

func TestTransition_SetsTimestamps(t *testing.T) {
	task := NewTask("ws-1", TaskTypeFixIssue, "Fix bug", "Description")

	if task.StartedAt != nil {
		t.Error("StartedAt should be nil for new task")
	}

	// Transition to running
	if err := task.Transition(StatusRunning); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if task.StartedAt == nil {
		t.Error("StartedAt should be set after transition to running")
	}
	if task.Status != StatusRunning {
		t.Errorf("status = %s, want running", task.Status)
	}

	// Transition to validating → completed
	if err := task.Transition(StatusValidating); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if err := task.Transition(StatusCompleted); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if task.CompletedAt == nil {
		t.Error("CompletedAt should be set after transition to completed")
	}
}

func TestTransition_InvalidReturnsError(t *testing.T) {
	task := NewTask("ws-1", TaskTypeFixIssue, "Fix bug", "Description")

	err := task.Transition(StatusCompleted) // pending → completed is invalid
	if err == nil {
		t.Fatal("expected error for invalid transition")
	}

	te, ok := err.(*TransitionError)
	if !ok {
		t.Fatalf("expected TransitionError, got %T", err)
	}
	if te.From != StatusPending || te.To != StatusCompleted {
		t.Errorf("TransitionError = %s → %s, want pending → completed", te.From, te.To)
	}
}

func TestTransition_AddsTimelineEvent(t *testing.T) {
	task := NewTask("ws-1", TaskTypeFixIssue, "Fix bug", "Description")
	initialLen := len(task.Timeline)

	_ = task.Transition(StatusRunning)
	if len(task.Timeline) != initialLen+1 {
		t.Errorf("timeline length = %d, want %d", len(task.Timeline), initialLen+1)
	}
	if task.Timeline[len(task.Timeline)-1].Type != "status_change" {
		t.Errorf("timeline event type = %s, want status_change", task.Timeline[len(task.Timeline)-1].Type)
	}
}

func TestIsTerminal(t *testing.T) {
	if !StatusCompleted.IsTerminal() {
		t.Error("completed should be terminal")
	}
	if StatusFailed.IsTerminal() {
		t.Error("failed should not be terminal")
	}
	if StatusRunning.IsTerminal() {
		t.Error("running should not be terminal")
	}
}

func TestIsActive(t *testing.T) {
	active := []Status{StatusPlanning, StatusRunning, StatusValidating}
	for _, s := range active {
		if !s.IsActive() {
			t.Errorf("%s should be active", s)
		}
	}

	inactive := []Status{StatusPending, StatusAwaitingApproval, StatusCompleted, StatusFailed, StatusStuck}
	for _, s := range inactive {
		if s.IsActive() {
			t.Errorf("%s should not be active", s)
		}
	}
}

func TestIsValidStatus(t *testing.T) {
	if !IsValidStatus("pending") {
		t.Error("pending should be valid")
	}
	if !IsValidStatus("running") {
		t.Error("running should be valid")
	}
	if IsValidStatus("invalid_status") {
		t.Error("invalid_status should not be valid")
	}
}

package task

import (
	"fmt"
	"time"
)

// Status represents the current state of an AgentTask.
type Status string

const (
	StatusPending          Status = "pending"
	StatusPlanning         Status = "planning"
	StatusRunning          Status = "running"
	StatusValidating       Status = "validating"
	StatusAwaitingApproval Status = "awaiting_approval"
	StatusCompleted        Status = "completed"
	StatusFailed           Status = "failed"
	StatusStuck            Status = "stuck"
)

// validTransitions defines the allowed state machine transitions.
// This matches LazyAdmin's AgentTaskStatusConstants.ValidTransitions exactly.
var validTransitions = map[Status][]Status{
	StatusPending:          {StatusPlanning, StatusRunning, StatusFailed},
	StatusPlanning:         {StatusRunning, StatusFailed},
	StatusRunning:          {StatusValidating, StatusFailed, StatusStuck},
	StatusValidating:       {StatusAwaitingApproval, StatusCompleted, StatusFailed, StatusRunning},
	StatusAwaitingApproval: {StatusCompleted, StatusRunning, StatusFailed},
	StatusCompleted:        {}, // terminal
	StatusFailed:           {StatusPending, StatusRunning},
	StatusStuck:            {StatusPending, StatusRunning, StatusFailed},
}

// IsTerminal returns true if the status is a terminal state.
func (s Status) IsTerminal() bool {
	return s == StatusCompleted
}

// IsActive returns true if the task is currently being worked on.
func (s Status) IsActive() bool {
	return s == StatusPlanning || s == StatusRunning || s == StatusValidating
}

// CanTransitionTo checks if a transition from the current status to the target is valid.
func (s Status) CanTransitionTo(target Status) bool {
	allowed, ok := validTransitions[s]
	if !ok {
		return false
	}
	for _, a := range allowed {
		if a == target {
			return true
		}
	}
	return false
}

// AllStatuses returns all valid status values.
func AllStatuses() []Status {
	return []Status{
		StatusPending, StatusPlanning, StatusRunning, StatusValidating,
		StatusAwaitingApproval, StatusCompleted, StatusFailed, StatusStuck,
	}
}

// IsValidStatus checks if a string is a valid Status.
func IsValidStatus(s string) bool {
	for _, status := range AllStatuses() {
		if string(status) == s {
			return true
		}
	}
	return false
}

// TransitionError is returned when an invalid state transition is attempted.
type TransitionError struct {
	From Status
	To   Status
}

func (e *TransitionError) Error() string {
	return fmt.Sprintf("invalid task status transition: %s → %s", e.From, e.To)
}

// Transition attempts to move the task to a new status. Returns an error
// if the transition is not allowed by the state machine.
func (t *AgentTask) Transition(newStatus Status) error {
	if !t.Status.CanTransitionTo(newStatus) {
		return &TransitionError{From: t.Status, To: newStatus}
	}

	oldStatus := t.Status
	t.Status = newStatus
	now := time.Now().UTC()

	// Set lifecycle timestamps
	switch newStatus {
	case StatusRunning:
		if t.StartedAt == nil {
			t.StartedAt = &now
		}
	case StatusCompleted, StatusFailed:
		t.CompletedAt = &now
	}

	t.AddTimelineEvent("status_change",
		fmt.Sprintf("Status changed: %s → %s", oldStatus, newStatus),
		"system",
	)

	return nil
}

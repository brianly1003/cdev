// Package security provides authentication and authorization for cdev.
package security

import (
	"crypto/rand"
	"encoding/hex"
	"fmt"
	"sync"
	"time"
)

// PairingApprovalStatus represents approval state for a pairing token nonce.
type PairingApprovalStatus string

const (
	PairingApprovalStatusNone     PairingApprovalStatus = "none"
	PairingApprovalStatusPending  PairingApprovalStatus = "pending"
	PairingApprovalStatusApproved PairingApprovalStatus = "approved"
	PairingApprovalStatusRejected PairingApprovalStatus = "rejected"
)

// PairingApprovalRequest represents a pending device pairing request.
type PairingApprovalRequest struct {
	RequestID  string    `json:"request_id"`
	RemoteAddr string    `json:"remote_addr"`
	UserAgent  string    `json:"user_agent"`
	CreatedAt  time.Time `json:"created_at"`
	ExpiresAt  time.Time `json:"expires_at"`

	nonce string
}

// PairingApprovalManager tracks pending/approved/rejected pairing requests.
type PairingApprovalManager struct {
	mu sync.Mutex

	pendingByID    map[string]*PairingApprovalRequest
	pendingByNonce map[string]string
	approvedNonces map[string]time.Time
	rejectedNonces map[string]time.Time
}

// NewPairingApprovalManager creates a new in-memory pairing approval manager.
func NewPairingApprovalManager() *PairingApprovalManager {
	return &PairingApprovalManager{
		pendingByID:    make(map[string]*PairingApprovalRequest),
		pendingByNonce: make(map[string]string),
		approvedNonces: make(map[string]time.Time),
		rejectedNonces: make(map[string]time.Time),
	}
}

// EnsurePending returns an existing pending request for nonce or creates a new one.
func (m *PairingApprovalManager) EnsurePending(nonce, remoteAddr, userAgent string, expiresAt time.Time) (*PairingApprovalRequest, error) {
	if nonce == "" {
		return nil, fmt.Errorf("nonce is required")
	}

	m.mu.Lock()
	defer m.mu.Unlock()

	now := time.Now()
	m.cleanupExpiredLocked(now)

	if requestID, ok := m.pendingByNonce[nonce]; ok {
		if req, found := m.pendingByID[requestID]; found {
			copyReq := *req
			return &copyReq, nil
		}
		delete(m.pendingByNonce, nonce)
	}

	requestID, err := newPairingRequestID()
	if err != nil {
		return nil, err
	}

	if expiresAt.IsZero() || !expiresAt.After(now) {
		expiresAt = now.Add(5 * time.Minute)
	}

	req := &PairingApprovalRequest{
		RequestID:  requestID,
		RemoteAddr: remoteAddr,
		UserAgent:  userAgent,
		CreatedAt:  now.UTC(),
		ExpiresAt:  expiresAt.UTC(),
		nonce:      nonce,
	}

	m.pendingByID[requestID] = req
	m.pendingByNonce[nonce] = requestID
	copyReq := *req
	return &copyReq, nil
}

// Status returns current approval status for a pairing token nonce.
func (m *PairingApprovalManager) Status(nonce string) PairingApprovalStatus {
	if nonce == "" {
		return PairingApprovalStatusNone
	}

	m.mu.Lock()
	defer m.mu.Unlock()

	now := time.Now()
	m.cleanupExpiredLocked(now)

	if _, ok := m.approvedNonces[nonce]; ok {
		return PairingApprovalStatusApproved
	}
	if _, ok := m.rejectedNonces[nonce]; ok {
		return PairingApprovalStatusRejected
	}
	if _, ok := m.pendingByNonce[nonce]; ok {
		return PairingApprovalStatusPending
	}
	return PairingApprovalStatusNone
}

// Approve marks a pending request as approved.
func (m *PairingApprovalManager) Approve(requestID string) (*PairingApprovalRequest, error) {
	m.mu.Lock()
	defer m.mu.Unlock()

	now := time.Now()
	m.cleanupExpiredLocked(now)

	req, ok := m.pendingByID[requestID]
	if !ok {
		return nil, fmt.Errorf("pending request not found")
	}

	delete(m.pendingByID, requestID)
	delete(m.pendingByNonce, req.nonce)
	m.approvedNonces[req.nonce] = req.ExpiresAt

	copyReq := *req
	return &copyReq, nil
}

// Reject marks a pending request as rejected.
func (m *PairingApprovalManager) Reject(requestID string) (*PairingApprovalRequest, error) {
	m.mu.Lock()
	defer m.mu.Unlock()

	now := time.Now()
	m.cleanupExpiredLocked(now)

	req, ok := m.pendingByID[requestID]
	if !ok {
		return nil, fmt.Errorf("pending request not found")
	}

	delete(m.pendingByID, requestID)
	delete(m.pendingByNonce, req.nonce)
	m.rejectedNonces[req.nonce] = req.ExpiresAt

	copyReq := *req
	return &copyReq, nil
}

// ListPending returns all currently pending pairing requests.
func (m *PairingApprovalManager) ListPending() []PairingApprovalRequest {
	m.mu.Lock()
	defer m.mu.Unlock()

	now := time.Now()
	m.cleanupExpiredLocked(now)

	out := make([]PairingApprovalRequest, 0, len(m.pendingByID))
	for _, req := range m.pendingByID {
		if req == nil {
			continue
		}
		out = append(out, *req)
	}
	return out
}

// ClearTokenDecision clears approved/rejected/pending state for a token nonce.
func (m *PairingApprovalManager) ClearTokenDecision(nonce string) {
	if nonce == "" {
		return
	}

	m.mu.Lock()
	defer m.mu.Unlock()

	if requestID, ok := m.pendingByNonce[nonce]; ok {
		delete(m.pendingByNonce, nonce)
		delete(m.pendingByID, requestID)
	}
	delete(m.approvedNonces, nonce)
	delete(m.rejectedNonces, nonce)
}

func (m *PairingApprovalManager) cleanupExpiredLocked(now time.Time) {
	for nonce, expiresAt := range m.approvedNonces {
		if !expiresAt.After(now) {
			delete(m.approvedNonces, nonce)
		}
	}
	for nonce, expiresAt := range m.rejectedNonces {
		if !expiresAt.After(now) {
			delete(m.rejectedNonces, nonce)
		}
	}
	for requestID, req := range m.pendingByID {
		if req == nil || !req.ExpiresAt.After(now) {
			delete(m.pendingByID, requestID)
			if req != nil {
				delete(m.pendingByNonce, req.nonce)
			}
		}
	}
}

func newPairingRequestID() (string, error) {
	buf := make([]byte, 12)
	if _, err := rand.Read(buf); err != nil {
		return "", fmt.Errorf("failed to generate pairing request id: %w", err)
	}
	return "pairreq_" + hex.EncodeToString(buf), nil
}

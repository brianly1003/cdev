//go:build deadlock

// Package sync provides mutex types that can be swapped for deadlock detection.
// In debug mode (build with -tags deadlock), this uses go-deadlock for detection.
package sync

import (
	"os"
	"sync"
	"time"

	"github.com/sasha-s/go-deadlock"
)

// Mutex is a mutual exclusion lock with deadlock detection.
// In debug mode, this wraps go-deadlock.Mutex.
type Mutex = deadlock.Mutex

// RWMutex is a reader/writer mutual exclusion lock with deadlock detection.
// In debug mode, this wraps go-deadlock.RWMutex.
type RWMutex = deadlock.RWMutex

// Locker is the standard sync.Locker interface.
type Locker = sync.Locker

// Once is the standard sync.Once.
type Once = sync.Once

// WaitGroup is the standard sync.WaitGroup.
type WaitGroup = sync.WaitGroup

// Cond is the standard sync.Cond.
type Cond = sync.Cond

// NewCond returns a new Cond.
func NewCond(l Locker) *Cond {
	return sync.NewCond(l)
}

// Map is the standard sync.Map.
type Map = sync.Map

// Pool is the standard sync.Pool.
type Pool = sync.Pool

func init() {
	// Configure go-deadlock options
	// DeadlockTimeout: how long to wait before reporting a potential deadlock
	deadlock.Opts.DeadlockTimeout = 30 * time.Second

	// Disable deadlock detection if CDEV_NO_DEADLOCK_DETECT is set
	if os.Getenv("CDEV_NO_DEADLOCK_DETECT") != "" {
		deadlock.Opts.Disable = true
		return
	}

	// OnPotentialDeadlock: callback when potential deadlock is detected
	deadlock.Opts.OnPotentialDeadlock = func() {
		// Default behavior: prints stack trace and exits
		// This can be customized if needed
	}

	// PrintAllCurrentGoroutines: print all goroutines when deadlock detected
	deadlock.Opts.PrintAllCurrentGoroutines = true

	// LogBuf: where to write deadlock reports (nil = os.Stderr)
	deadlock.Opts.LogBuf = nil

	println("[DEADLOCK DETECTION ENABLED] Using go-deadlock for mutex operations")
}

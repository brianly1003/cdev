//go:build !deadlock

// Package sync provides mutex types that can be swapped for deadlock detection.
// In release mode (default), this uses the standard sync package.
// In debug mode (build with -tags deadlock), this uses go-deadlock for detection.
package sync

import "sync"

// Mutex is a mutual exclusion lock.
// In release mode, this is the standard sync.Mutex.
type Mutex = sync.Mutex

// RWMutex is a reader/writer mutual exclusion lock.
// In release mode, this is the standard sync.RWMutex.
type RWMutex = sync.RWMutex

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
	// No-op in release mode
}

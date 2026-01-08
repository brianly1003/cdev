// Package http implements the HTTP API server for cdev.
package http

import (
	"fmt"
	"net/http"
	"net/http/pprof"
	"runtime"
	"time"

	"github.com/rs/zerolog/log"
)

// DebugHandler provides debug and profiling endpoints.
type DebugHandler struct {
	pprofEnabled bool
	startTime    time.Time
}

// NewDebugHandler creates a new debug handler.
func NewDebugHandler(pprofEnabled bool) *DebugHandler {
	return &DebugHandler{
		pprofEnabled: pprofEnabled,
		startTime:    time.Now(),
	}
}

// Register registers debug endpoints on the given ServeMux.
func (h *DebugHandler) Register(mux *http.ServeMux) {
	// Debug index page
	mux.HandleFunc("/debug/", h.handleDebugIndex)

	// Runtime info endpoint
	mux.HandleFunc("/debug/runtime", h.handleRuntimeInfo)

	// pprof endpoints (when enabled)
	if h.pprofEnabled {
		// Main pprof index
		mux.HandleFunc("/debug/pprof/", pprof.Index)

		// Specific pprof handlers
		mux.HandleFunc("/debug/pprof/cmdline", pprof.Cmdline)
		mux.HandleFunc("/debug/pprof/profile", pprof.Profile)
		mux.HandleFunc("/debug/pprof/symbol", pprof.Symbol)
		mux.HandleFunc("/debug/pprof/trace", pprof.Trace)

		// Named profiles (heap, goroutine, block, mutex, etc.) are handled by Index

		log.Info().Msg("pprof endpoints registered at /debug/pprof/")
	}

	log.Info().Bool("pprof", h.pprofEnabled).Msg("debug endpoints registered at /debug/")
}

// handleDebugIndex shows available debug endpoints.
func (h *DebugHandler) handleDebugIndex(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}

	// Only serve index at exact /debug/ path
	if r.URL.Path != "/debug/" {
		http.NotFound(w, r)
		return
	}

	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	w.WriteHeader(http.StatusOK)

	_, _ = fmt.Fprintf(w, `<!DOCTYPE html>
<html>
<head>
    <title>cdev Debug</title>
    <style>
        body { font-family: -apple-system, BlinkMacSystemFont, "Segoe UI", Roboto, sans-serif; margin: 40px; }
        h1 { color: #333; }
        h2 { color: #666; margin-top: 30px; }
        ul { line-height: 1.8; }
        a { color: #0066cc; text-decoration: none; }
        a:hover { text-decoration: underline; }
        .disabled { color: #999; }
        code { background: #f4f4f4; padding: 2px 6px; border-radius: 3px; }
    </style>
</head>
<body>
    <h1>cdev Debug Endpoints</h1>

    <h2>Runtime Information</h2>
    <ul>
        <li><a href="/debug/runtime">/debug/runtime</a> - Go runtime statistics (JSON)</li>
    </ul>
`)

	if h.pprofEnabled {
		_, _ = fmt.Fprintf(w, `
    <h2>Profiling (pprof)</h2>
    <ul>
        <li><a href="/debug/pprof/">/debug/pprof/</a> - pprof index (all profiles)</li>
        <li><a href="/debug/pprof/heap">/debug/pprof/heap</a> - Memory allocation profile</li>
        <li><a href="/debug/pprof/goroutine?debug=1">/debug/pprof/goroutine?debug=1</a> - All goroutine stacks</li>
        <li><a href="/debug/pprof/goroutine?debug=2">/debug/pprof/goroutine?debug=2</a> - Goroutines with full stacks</li>
        <li><a href="/debug/pprof/block">/debug/pprof/block</a> - Blocking profile</li>
        <li><a href="/debug/pprof/mutex">/debug/pprof/mutex</a> - Mutex contention</li>
        <li><a href="/debug/pprof/threadcreate">/debug/pprof/threadcreate</a> - Thread creation</li>
    </ul>

    <h2>CPU Profiling</h2>
    <p>To capture a 30-second CPU profile:</p>
    <pre><code>go tool pprof http://localhost:8766/debug/pprof/profile?seconds=30</code></pre>

    <h2>Trace</h2>
    <p>To capture a 5-second execution trace:</p>
    <pre><code>curl -o trace.out http://localhost:8766/debug/pprof/trace?seconds=5
go tool trace trace.out</code></pre>
`)
	} else {
		_, _ = fmt.Fprintf(w, `
    <h2>Profiling (pprof)</h2>
    <p class="disabled">pprof is disabled. Enable with <code>debug.pprof_enabled: true</code> in config.</p>
`)
	}

	_, _ = fmt.Fprintf(w, `
</body>
</html>
`)
}

// handleRuntimeInfo returns Go runtime information as JSON.
func (h *DebugHandler) handleRuntimeInfo(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}

	var memStats runtime.MemStats
	runtime.ReadMemStats(&memStats)

	info := map[string]interface{}{
		"go_version":   runtime.Version(),
		"go_os":        runtime.GOOS,
		"go_arch":      runtime.GOARCH,
		"num_cpu":      runtime.NumCPU(),
		"num_goroutine": runtime.NumGoroutine(),
		"uptime_seconds": int64(time.Since(h.startTime).Seconds()),
		"memory": map[string]interface{}{
			"alloc_mb":        float64(memStats.Alloc) / 1024 / 1024,
			"total_alloc_mb":  float64(memStats.TotalAlloc) / 1024 / 1024,
			"sys_mb":          float64(memStats.Sys) / 1024 / 1024,
			"heap_alloc_mb":   float64(memStats.HeapAlloc) / 1024 / 1024,
			"heap_sys_mb":     float64(memStats.HeapSys) / 1024 / 1024,
			"heap_idle_mb":    float64(memStats.HeapIdle) / 1024 / 1024,
			"heap_inuse_mb":   float64(memStats.HeapInuse) / 1024 / 1024,
			"heap_objects":    memStats.HeapObjects,
			"stack_inuse_mb":  float64(memStats.StackInuse) / 1024 / 1024,
			"num_gc":          memStats.NumGC,
			"gc_pause_total_ms": float64(memStats.PauseTotalNs) / 1e6,
		},
	}

	writeJSON(w, http.StatusOK, info)
}

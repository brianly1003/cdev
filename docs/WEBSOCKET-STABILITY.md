# WebSocket Connection Stability Analysis

**Document Type:** Technical Deep Dive
**Version:** 1.0.0
**Date:** December 2025
**Status:** Analysis & Recommendations

---

## Executive Summary

This document provides an in-depth analysis of WebSocket connection stability for cdev, particularly for mobile client scenarios. Mobile networks present unique challenges including intermittent connectivity, IP address changes, and device power management that can disrupt WebSocket connections.

---

## 1. Current Implementation Analysis

### 1.1 Configuration Parameters

```go
// server.go
const (
    writeWait      = 10 * time.Second   // Write deadline
    pongWait       = 60 * time.Second   // Pong timeout
    pingPeriod     = 54 * time.Second   // Ping interval (9/10 of pongWait)
    maxMessageSize = 512 * 1024         // 512KB max message
)

// client.go
send: make(chan []byte, 256)  // 256-message buffer
```

### 1.2 Connection Flow

```
┌─────────────┐     HTTP Upgrade      ┌─────────────┐
│   Mobile    │ ───────────────────► │   Agent     │
│   Client    │                       │   Server    │
└──────┬──────┘                       └──────┬──────┘
       │                                      │
       │ ◄──── session_start event ────────  │
       │                                      │
       │ ◄──────── ping (54s) ─────────────  │
       │ ─────────  pong ──────────────────► │
       │                                      │
       │ ◄──── claude_log events ──────────  │
       │                                      │
       │ ──── run_claude command ──────────► │
       │                                      │
```

### 1.3 Identified Stability Issues

| Issue | Severity | Impact |
|-------|----------|--------|
| No automatic reconnection | High | Client disconnects permanently |
| Message batching corrupts JSON | Medium | Multiple JSON objects merged |
| HTTP timeouts conflict with WS | Medium | Premature connection close |
| No application heartbeat | Medium | Can't detect app-level issues |
| Send buffer overflow | Medium | Silent message loss |
| No message acknowledgment | Low | Event delivery not guaranteed |
| No compression | Low | High bandwidth on mobile |

---

## 2. Detailed Issue Analysis

### 2.1 CRITICAL: Message Batching Bug

**Location:** `internal/server/websocket/client.go:140-145`

```go
// Current implementation - PROBLEMATIC
w, err := c.conn.NextWriter(websocket.TextMessage)
w.Write(message)

// Batch any queued messages
n := len(c.send)
for i := 0; i < n; i++ {
    w.Write([]byte{'\n'})      // Joins with newline
    w.Write(<-c.send)
}
```

**Problem:** This joins multiple JSON events with `\n`, creating invalid JSON when parsed as a single message.

**Example of corrupted output:**
```json
{"event":"claude_log","payload":{"line":"..."}}
{"event":"claude_log","payload":{"line":"..."}}
```

A client expecting single JSON objects will fail to parse this.

**Recommendation:** Send each message as a separate WebSocket frame:

```go
// Fixed implementation
func (c *Client) writePump() {
    ticker := time.NewTicker(pingPeriod)
    defer func() {
        ticker.Stop()
        c.conn.Close()
    }()

    for {
        select {
        case <-c.done:
            c.conn.WriteMessage(websocket.CloseMessage, []byte{})
            return

        case message, ok := <-c.send:
            if !ok {
                c.conn.WriteMessage(websocket.CloseMessage, []byte{})
                return
            }

            c.conn.SetWriteDeadline(time.Now().Add(writeWait))

            // Send as individual frame - don't batch
            if err := c.conn.WriteMessage(websocket.TextMessage, message); err != nil {
                return
            }

        case <-ticker.C:
            c.conn.SetWriteDeadline(time.Now().Add(writeWait))
            if err := c.conn.WriteMessage(websocket.PingMessage, nil); err != nil {
                return
            }
        }
    }
}
```

---

### 2.2 HIGH: HTTP Server Timeouts Conflict

**Location:** `internal/server/websocket/server.go:67-72`

```go
s.server = &http.Server{
    Addr:         addr,
    Handler:      mux,
    ReadTimeout:  10 * time.Second,   // PROBLEM
    WriteTimeout: 10 * time.Second,   // PROBLEM
}
```

**Problem:** These timeouts apply to the underlying HTTP connection. After WebSocket upgrade, they can cause premature disconnection if no data flows for 10 seconds.

**Recommendation:**

```go
s.server = &http.Server{
    Addr:    addr,
    Handler: mux,
    // Don't set ReadTimeout/WriteTimeout for WebSocket server
    // The gorilla/websocket library handles its own deadlines

    // Only set IdleTimeout if needed for non-WS connections
    IdleTimeout: 120 * time.Second,
}
```

---

### 2.3 HIGH: No Automatic Reconnection Support

**Current State:** When a connection drops, the client must:
1. Detect the disconnection
2. Reconnect manually
3. Resubscribe to events
4. Potentially miss events during reconnection

**Recommendation: Server-Side Session Persistence**

```go
// Session manager for reconnection support
type SessionManager struct {
    mu       sync.RWMutex
    sessions map[string]*Session
}

type Session struct {
    ID           string
    ClientID     string
    CreatedAt    time.Time
    LastSeen     time.Time
    EventBuffer  *RingBuffer  // Buffer recent events for replay
    ReconnectKey string       // Secret key for reconnection
}

// On initial connect
func (sm *SessionManager) CreateSession(clientID string) *Session {
    session := &Session{
        ID:           uuid.New().String(),
        ClientID:     clientID,
        CreatedAt:    time.Now(),
        LastSeen:     time.Now(),
        EventBuffer:  NewRingBuffer(100), // Keep last 100 events
        ReconnectKey: generateSecureKey(),
    }
    sm.sessions[session.ID] = session
    return session
}

// On reconnect with session_id + reconnect_key
func (sm *SessionManager) ReconnectSession(sessionID, reconnectKey string, newClientID string) (*Session, []events.Event, error) {
    session := sm.sessions[sessionID]
    if session == nil {
        return nil, nil, ErrSessionNotFound
    }
    if session.ReconnectKey != reconnectKey {
        return nil, nil, ErrInvalidReconnectKey
    }

    // Get missed events
    missedEvents := session.EventBuffer.Since(session.LastSeen)

    // Update session
    session.ClientID = newClientID
    session.LastSeen = time.Now()

    return session, missedEvents, nil
}
```

**Client Protocol:**

```json
// Initial connect response
{
    "event": "session_start",
    "payload": {
        "session_id": "abc-123",
        "reconnect_key": "secret-key-xyz",
        "reconnect_url": "ws://host:port?session=abc-123"
    }
}

// Reconnect request
{
    "command": "reconnect",
    "payload": {
        "session_id": "abc-123",
        "reconnect_key": "secret-key-xyz",
        "last_event_id": "evt-456"
    }
}

// Reconnect response with missed events
{
    "event": "reconnected",
    "payload": {
        "missed_events": [...]
    }
}
```

---

### 2.4 MEDIUM: No Application-Level Heartbeat

**Current State:** Only WebSocket ping/pong frames (not visible to application layer).

**Problem:**
- Can't detect application-level issues (e.g., event hub frozen)
- Some proxies/load balancers strip ping/pong frames
- Mobile apps can't display connection status based on ping/pong

**Recommendation: Add Application Heartbeat**

```go
// Server sends heartbeat event every 30 seconds
type HeartbeatEvent struct {
    ServerTime    time.Time `json:"server_time"`
    Sequence      int64     `json:"sequence"`
    ClaudeStatus  string    `json:"claude_status"`
    EventsQueued  int       `json:"events_queued"`
}

// In writePump, add heartbeat ticker
heartbeatTicker := time.NewTicker(30 * time.Second)
defer heartbeatTicker.Stop()

for {
    select {
    // ... existing cases ...

    case <-heartbeatTicker.C:
        heartbeat := events.NewHeartbeatEvent(
            time.Now(),
            atomic.AddInt64(&c.heartbeatSeq, 1),
            getClaudeStatus(),
            len(c.send),
        )
        data, _ := heartbeat.ToJSON()
        c.conn.SetWriteDeadline(time.Now().Add(writeWait))
        c.conn.WriteMessage(websocket.TextMessage, data)
    }
}
```

**Client can monitor:**
```javascript
let lastHeartbeat = Date.now();
let heartbeatTimeout = null;

ws.onmessage = (event) => {
    const data = JSON.parse(event.data);
    if (data.event === 'heartbeat') {
        lastHeartbeat = Date.now();
        clearTimeout(heartbeatTimeout);
        heartbeatTimeout = setTimeout(() => {
            console.warn('Heartbeat timeout - connection may be stale');
            ws.close();
            reconnect();
        }, 45000); // 1.5x heartbeat interval
    }
};
```

---

### 2.5 MEDIUM: Send Buffer Overflow

**Location:** `internal/server/websocket/client.go:57-62`

```go
select {
case c.send <- message:
default:
    // Channel full, client is too slow
    log.Warn().Str("client_id", c.id).Msg("client send channel full, dropping message")
}
```

**Problem:** Messages are silently dropped when buffer (256) is full. Client has no way to know events were lost.

**Recommendations:**

**Option A: Backpressure with Disconnect**
```go
func (c *Client) Send(message []byte) error {
    c.mu.Lock()
    if c.closed {
        c.mu.Unlock()
        return ErrClientClosed
    }
    c.mu.Unlock()

    select {
    case c.send <- message:
        return nil
    case <-time.After(5 * time.Second):
        // Client too slow, disconnect
        log.Warn().Str("client_id", c.id).Msg("client too slow, disconnecting")
        c.Close()
        return ErrClientTooSlow
    }
}
```

**Option B: Event Sequence Numbers**
```go
type SequencedEvent struct {
    Sequence int64       `json:"seq"`
    Event    events.Event `json:"event"`
}

// Client can detect gaps and request replay
// "I received seq 100, 101, 103 - please resend 102"
```

**Option C: Increase Buffer + Monitor**
```go
send: make(chan []byte, 1024),  // Larger buffer

// Add metrics
var (
    sendBufferUsage = prometheus.NewGaugeVec(
        prometheus.GaugeOpts{
            Name: "cdev_ws_send_buffer_usage",
            Help: "WebSocket send buffer usage",
        },
        []string{"client_id"},
    )
)
```

---

### 2.6 LOW: No Compression

**Problem:** Claude output can be large (code blocks, long explanations). On mobile networks, this consumes bandwidth and increases latency.

**Recommendation: Enable Per-Message Deflate**

```go
var upgrader = websocket.Upgrader{
    ReadBufferSize:  1024,
    WriteBufferSize: 1024,
    CheckOrigin:     checkOrigin,
    EnableCompression: true,  // Enable permessage-deflate
}
```

**Note:** Test on target mobile devices - compression adds CPU overhead.

---

## 3. Mobile-Specific Considerations

### 3.1 Network Transitions

Mobile devices frequently switch networks (WiFi ↔ Cellular). This causes:
- IP address change
- TCP connection reset
- No graceful close (connection just dies)

**Detection:**
```go
// Server-side: Detect stale connections
func (c *Client) monitorConnection() {
    staleCheck := time.NewTicker(30 * time.Second)
    defer staleCheck.Stop()

    for {
        select {
        case <-staleCheck.C:
            // Check if we've received any data recently
            c.mu.Lock()
            lastActivity := c.lastActivity
            c.mu.Unlock()

            if time.Since(lastActivity) > 90*time.Second {
                log.Warn().Str("client_id", c.id).Msg("connection appears stale")
                c.Close()
                return
            }
        case <-c.done:
            return
        }
    }
}
```

**Client-side: Network change detection (iOS/Android):**
```swift
// iOS - Network path monitor
let monitor = NWPathMonitor()
monitor.pathUpdateHandler = { path in
    if path.status == .satisfied {
        // Network available - check if WS still connected
        if !websocket.isConnected {
            websocket.reconnect()
        }
    }
}
```

### 3.2 Device Sleep/Background

When mobile app goes to background:
- iOS: WebSocket disconnected after ~30 seconds
- Android: Depends on battery optimization settings

**Recommendations:**

1. **Graceful background handling:**
```swift
// iOS
func applicationDidEnterBackground() {
    // Send goodbye message
    websocket.send(json: ["command": "going_background"])

    // Keep connection for brief period
    backgroundTask = UIApplication.shared.beginBackgroundTask {
        self.websocket.disconnect()
    }
}

func applicationWillEnterForeground() {
    websocket.reconnect()
}
```

2. **Server-side session preservation:**
- Don't immediately clean up session on disconnect
- Keep session state for 5-10 minutes
- Allow reconnection with event replay

### 3.3 Aggressive Timeouts for Mobile

Mobile networks have higher latency. Current timeouts may be too tight.

**Recommended Mobile-Optimized Configuration:**

```go
const (
    // More tolerant timeouts for mobile
    writeWait      = 15 * time.Second   // Was 10s
    pongWait       = 90 * time.Second   // Was 60s
    pingPeriod     = 45 * time.Second   // Was 54s

    // Longer reconnect window
    sessionTimeout = 10 * time.Minute   // Keep session for reconnect
)
```

---

## 4. Recommended Architecture

### 4.1 Enhanced Connection State Machine

```
                    ┌─────────────────────────────────────┐
                    │                                     │
                    ▼                                     │
    ┌───────────┐  connect   ┌────────────┐             │
    │DISCONNECTED│──────────►│ CONNECTING │             │
    └───────────┘            └─────┬──────┘             │
          ▲                        │                     │
          │                        │ upgraded            │
          │                        ▼                     │
          │                  ┌────────────┐             │
          │   timeout/       │  CONNECTED │◄────────────┤
          │   error          └─────┬──────┘   reconnect │
          │                        │                     │
          │                        │ network loss        │
          │                        ▼                     │
          │                  ┌────────────┐             │
          └──────────────────│RECONNECTING│─────────────┘
             max retries     └────────────┘
```

### 4.2 Connection Manager Implementation

```go
type ConnectionManager struct {
    client          *Client
    sessionManager  *SessionManager

    state           ConnectionState
    stateMu         sync.RWMutex

    reconnectCount  int
    maxReconnects   int
    reconnectDelay  time.Duration

    eventBuffer     *RingBuffer
    lastEventSeq    int64
}

func (cm *ConnectionManager) handleDisconnect() {
    cm.stateMu.Lock()
    cm.state = StateReconnecting
    cm.stateMu.Unlock()

    for cm.reconnectCount < cm.maxReconnects {
        cm.reconnectCount++

        delay := cm.reconnectDelay * time.Duration(math.Pow(2, float64(cm.reconnectCount-1)))
        if delay > 30*time.Second {
            delay = 30 * time.Second
        }

        log.Info().
            Int("attempt", cm.reconnectCount).
            Dur("delay", delay).
            Msg("attempting reconnection")

        time.Sleep(delay)

        if err := cm.reconnect(); err == nil {
            cm.reconnectCount = 0
            cm.stateMu.Lock()
            cm.state = StateConnected
            cm.stateMu.Unlock()
            return
        }
    }

    // Max retries exceeded
    cm.stateMu.Lock()
    cm.state = StateDisconnected
    cm.stateMu.Unlock()
}
```

### 4.3 Event Delivery Guarantees

```go
type EventDeliveryManager struct {
    // Track sent events
    sentEvents     map[int64]*SentEvent
    sentMu         sync.RWMutex

    // Acknowledgment handling
    ackChan        chan int64

    // Retry configuration
    ackTimeout     time.Duration
    maxRetries     int
}

type SentEvent struct {
    Sequence   int64
    Event      events.Event
    SentAt     time.Time
    RetryCount int
}

func (edm *EventDeliveryManager) SendWithAck(event events.Event) error {
    seq := atomic.AddInt64(&edm.sequence, 1)

    sentEvent := &SentEvent{
        Sequence: seq,
        Event:    event,
        SentAt:   time.Now(),
    }

    edm.sentMu.Lock()
    edm.sentEvents[seq] = sentEvent
    edm.sentMu.Unlock()

    // Send event
    if err := edm.send(seq, event); err != nil {
        return err
    }

    // Wait for ack with timeout
    select {
    case ackSeq := <-edm.ackChan:
        if ackSeq == seq {
            edm.sentMu.Lock()
            delete(edm.sentEvents, seq)
            edm.sentMu.Unlock()
            return nil
        }
    case <-time.After(edm.ackTimeout):
        return edm.retry(sentEvent)
    }

    return nil
}
```

---

## 5. Implementation Roadmap

### Phase 1: Critical Fixes (1-2 days)

| Task | Priority | Effort |
|------|----------|--------|
| Fix message batching bug | P0 | 1h |
| Remove HTTP server timeouts | P0 | 0.5h |
| Add application heartbeat | P1 | 2h |
| Increase send buffer | P1 | 0.5h |

### Phase 2: Reconnection Support (3-5 days)

| Task | Priority | Effort |
|------|----------|--------|
| Session manager | P1 | 4h |
| Event sequence numbers | P1 | 2h |
| Reconnection protocol | P1 | 4h |
| Event replay buffer | P1 | 3h |
| Client reconnection docs | P2 | 2h |

### Phase 3: Mobile Optimization (2-3 days)

| Task | Priority | Effort |
|------|----------|--------|
| Enable compression | P2 | 1h |
| Adjust timeouts for mobile | P2 | 1h |
| Stale connection detection | P2 | 2h |
| Background handling docs | P2 | 2h |
| Connection state events | P3 | 3h |

### Phase 4: Advanced Features (Optional)

| Task | Priority | Effort |
|------|----------|--------|
| Event delivery acknowledgment | P3 | 8h |
| Connection quality metrics | P3 | 4h |
| Adaptive heartbeat interval | P4 | 4h |
| Message priority queuing | P4 | 6h |

---

## 6. Client Implementation Guidelines

### 6.1 Recommended Client Architecture

```typescript
class CdevWebSocket {
    private ws: WebSocket | null = null;
    private sessionId: string | null = null;
    private reconnectKey: string | null = null;
    private lastEventSeq: number = 0;

    private reconnectAttempts = 0;
    private maxReconnectAttempts = 10;
    private reconnectDelay = 1000;

    private heartbeatTimeout: NodeJS.Timeout | null = null;
    private heartbeatInterval = 30000;
    private heartbeatMissThreshold = 45000;

    connect(url: string): Promise<void> {
        return new Promise((resolve, reject) => {
            this.ws = new WebSocket(url);

            this.ws.onopen = () => {
                this.reconnectAttempts = 0;
                this.startHeartbeatMonitor();
                resolve();
            };

            this.ws.onclose = (event) => {
                this.stopHeartbeatMonitor();

                if (!event.wasClean) {
                    this.scheduleReconnect();
                }
            };

            this.ws.onerror = (error) => {
                reject(error);
            };

            this.ws.onmessage = (event) => {
                this.handleMessage(JSON.parse(event.data));
            };
        });
    }

    private handleMessage(data: any) {
        // Update sequence tracking
        if (data.seq) {
            this.lastEventSeq = data.seq;
        }

        // Reset heartbeat on any message
        this.resetHeartbeatTimeout();

        // Handle session info
        if (data.event === 'session_start') {
            this.sessionId = data.payload.session_id;
            this.reconnectKey = data.payload.reconnect_key;
        }

        // Handle heartbeat
        if (data.event === 'heartbeat') {
            this.onHeartbeat(data.payload);
            return;
        }

        // Dispatch to handlers
        this.dispatchEvent(data);
    }

    private scheduleReconnect() {
        if (this.reconnectAttempts >= this.maxReconnectAttempts) {
            this.onMaxReconnectAttemptsReached();
            return;
        }

        const delay = Math.min(
            this.reconnectDelay * Math.pow(2, this.reconnectAttempts),
            30000
        );

        this.reconnectAttempts++;

        setTimeout(() => {
            this.reconnect();
        }, delay);
    }

    private async reconnect() {
        const url = this.buildReconnectUrl();

        try {
            await this.connect(url);

            // Request missed events
            if (this.sessionId && this.reconnectKey) {
                this.send({
                    command: 'reconnect',
                    payload: {
                        session_id: this.sessionId,
                        reconnect_key: this.reconnectKey,
                        last_event_seq: this.lastEventSeq
                    }
                });
            }
        } catch (error) {
            this.scheduleReconnect();
        }
    }

    private startHeartbeatMonitor() {
        this.heartbeatTimeout = setTimeout(() => {
            console.warn('Heartbeat missed - connection may be stale');
            this.ws?.close();
        }, this.heartbeatMissThreshold);
    }

    private resetHeartbeatTimeout() {
        if (this.heartbeatTimeout) {
            clearTimeout(this.heartbeatTimeout);
        }
        this.startHeartbeatMonitor();
    }
}
```

### 6.2 iOS-Specific Considerations

```swift
class CdevWebSocketManager: NSObject {
    private var socket: URLSessionWebSocketTask?
    private var session: URLSession!
    private var backgroundTask: UIBackgroundTaskIdentifier = .invalid

    override init() {
        super.init()

        let config = URLSessionConfiguration.default
        config.waitsForConnectivity = true
        config.allowsCellularAccess = true

        self.session = URLSession(
            configuration: config,
            delegate: self,
            delegateQueue: .main
        )

        // Monitor network changes
        setupNetworkMonitor()

        // Handle app lifecycle
        setupAppLifecycleObservers()
    }

    private func setupAppLifecycleObservers() {
        NotificationCenter.default.addObserver(
            self,
            selector: #selector(appDidEnterBackground),
            name: UIApplication.didEnterBackgroundNotification,
            object: nil
        )

        NotificationCenter.default.addObserver(
            self,
            selector: #selector(appWillEnterForeground),
            name: UIApplication.willEnterForegroundNotification,
            object: nil
        )
    }

    @objc private func appDidEnterBackground() {
        // Start background task to keep connection briefly
        backgroundTask = UIApplication.shared.beginBackgroundTask { [weak self] in
            self?.endBackgroundTask()
        }

        // Schedule disconnect after 25 seconds (before iOS kills us)
        DispatchQueue.main.asyncAfter(deadline: .now() + 25) { [weak self] in
            self?.gracefulDisconnect()
            self?.endBackgroundTask()
        }
    }

    @objc private func appWillEnterForeground() {
        endBackgroundTask()
        reconnect()
    }
}
```

---

## 7. Testing Recommendations

### 7.1 Connection Stability Tests

```go
func TestReconnectionAfterNetworkLoss(t *testing.T) {
    // 1. Connect client
    // 2. Simulate network loss (close TCP without WS close frame)
    // 3. Verify server detects stale connection
    // 4. Reconnect with session ID
    // 5. Verify missed events are replayed
}

func TestMessageDeliveryUnderLoad(t *testing.T) {
    // 1. Connect slow client (artificial delays)
    // 2. Send 1000 events rapidly
    // 3. Verify no messages lost
    // 4. Verify correct order
}

func TestHeartbeatTimeout(t *testing.T) {
    // 1. Connect client
    // 2. Stop sending pong responses
    // 3. Verify server disconnects after pongWait
}
```

### 7.2 Mobile Simulation Tests

```bash
# Network condition simulation (macOS)
sudo dnctl pipe 1 config bw 100Kbit/s delay 500ms plr 0.05
sudo pfctl -e
echo "dummynet out proto tcp from any to any port 8765 pipe 1" | sudo pfctl -f -

# Test with degraded network
go test -v ./test/integration -run TestMobileNetwork
```

---

## 8. Metrics to Monitor

```go
var (
    wsConnectionsTotal = prometheus.NewCounter(
        prometheus.CounterOpts{
            Name: "cdev_ws_connections_total",
            Help: "Total WebSocket connections",
        },
    )

    wsReconnectionsTotal = prometheus.NewCounter(
        prometheus.CounterOpts{
            Name: "cdev_ws_reconnections_total",
            Help: "Total reconnection attempts",
        },
    )

    wsMessagesDropped = prometheus.NewCounter(
        prometheus.CounterOpts{
            Name: "cdev_ws_messages_dropped_total",
            Help: "Messages dropped due to full buffer",
        },
    )

    wsLatency = prometheus.NewHistogram(
        prometheus.HistogramOpts{
            Name:    "cdev_ws_message_latency_seconds",
            Help:    "WebSocket message delivery latency",
            Buckets: []float64{.001, .005, .01, .025, .05, .1, .25, .5, 1},
        },
    )
)
```

---

## 9. Summary

### Critical Actions (Do Now)

1. **Fix message batching** - Prevents JSON parsing errors
2. **Remove HTTP timeouts** - Prevents premature disconnection
3. **Add application heartbeat** - Enables connection health monitoring

### High Priority (This Sprint)

4. **Session persistence** - Enables reconnection
5. **Event sequence numbers** - Enables gap detection
6. **Increase buffer + metrics** - Prevents silent message loss

### Medium Priority (Next Sprint)

7. **Reconnection protocol** - Full reconnection support
8. **Mobile timeout tuning** - Better mobile experience
9. **Compression** - Bandwidth optimization

The current implementation provides basic WebSocket functionality but lacks the robustness needed for mobile clients. The most critical issues are the message batching bug and lack of reconnection support. Addressing these will significantly improve connection stability.

---

*Document Version: 1.0.0*
*Generated: December 2025*

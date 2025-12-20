import { useState, useEffect } from 'react'
import { GetConnectionStatus, GetQRCodeData, GetConnectionURLs } from '../../wailsjs/go/main/DesktopApp'

interface ConnectionStatus {
  server_running: boolean
  server_port: number
  server_address: string
  connected_clients: number
  claude_state: string
  active_repo: string
  session_id?: string
}

function Dashboard() {
  const [status, setStatus] = useState<ConnectionStatus | null>(null)
  const [qrCode, setQrCode] = useState<string>('')
  const [urls, setUrls] = useState<{ websocket: string; http: string } | null>(null)
  const [loading, setLoading] = useState(true)

  useEffect(() => {
    loadData()
  }, [])

  async function loadData() {
    try {
      const [statusData, qrData, urlData] = await Promise.all([
        GetConnectionStatus(),
        GetQRCodeData(),
        GetConnectionURLs()
      ])
      setStatus(statusData)
      setQrCode(qrData)
      setUrls(urlData as { websocket: string; http: string })
    } catch (err) {
      console.error('Failed to load data:', err)
    } finally {
      setLoading(false)
    }
  }

  function copyToClipboard(text: string) {
    navigator.clipboard.writeText(text)
  }

  if (loading) {
    return (
      <div style={{ display: 'flex', justifyContent: 'center', alignItems: 'center', height: '100%' }}>
        <div className="text-secondary">Loading...</div>
      </div>
    )
  }

  return (
    <div style={{ maxWidth: 1000, margin: '0 auto' }}>
      {/* Status Cards */}
      <div className="grid-3" style={{ marginBottom: 'var(--space-lg)' }}>
        <div className="status-card">
          <div className="status-card-header">
            <div className={`status-dot ${status?.server_running ? 'connected' : 'disconnected'}`} />
            <span className="status-card-label">Server</span>
          </div>
          <div className="status-card-value">
            {status?.server_running ? 'Running' : 'Stopped'}
          </div>
          <div className="status-card-meta">:{status?.server_port}</div>
        </div>

        <div className="status-card">
          <div className="status-card-header">
            <svg width="14" height="14" viewBox="0 0 24 24" fill="none" stroke="var(--info-blue)" strokeWidth="2">
              <rect x="5" y="2" width="14" height="20" rx="2" ry="2" />
              <line x1="12" y1="18" x2="12.01" y2="18" />
            </svg>
            <span className="status-card-label">Mobile</span>
          </div>
          <div className="status-card-value">
            {status?.connected_clients || 0} Connected
          </div>
          <div className="status-card-meta">via WebSocket</div>
        </div>

        <div className="status-card">
          <div className="status-card-header">
            <div className={`status-dot ${
              status?.claude_state === 'running' ? 'connected' :
              status?.claude_state === 'waiting' ? 'waiting' : 'disconnected'
            }`} />
            <span className="status-card-label">Claude</span>
          </div>
          <div className="status-card-value" style={{ textTransform: 'capitalize' }}>
            {status?.claude_state || 'Idle'}
          </div>
          <div className="status-card-meta">{status?.active_repo || 'No repo'}</div>
        </div>
      </div>

      {/* Main Content Grid */}
      <div className="grid-2">
        {/* QR Code Section */}
        <div className="qr-container">
          <h3 style={{ marginBottom: 'var(--space-md)', color: 'var(--text-primary)' }}>
            Quick Connect
          </h3>

          <div className="qr-code">
            {qrCode ? (
              <img src={qrCode} alt="Connection QR Code" />
            ) : (
              <div style={{ width: 200, height: 200, display: 'flex', alignItems: 'center', justifyContent: 'center', background: 'var(--bg-highlight)' }}>
                <span className="text-tertiary">No QR Code</span>
              </div>
            )}
          </div>

          <p className="qr-instructions">
            Scan with cdev-ios to connect
          </p>

          {urls && (
            <div style={{ marginTop: 'var(--space-md)', width: '100%' }}>
              <div style={{
                display: 'flex',
                flexDirection: 'column',
                gap: 'var(--space-xs)',
                padding: 'var(--space-sm)',
                background: 'var(--bg-deep)',
                borderRadius: 'var(--radius-md)',
                fontFamily: 'var(--font-mono)',
                fontSize: 'var(--text-xs)'
              }}>
                <div style={{ display: 'flex', justifyContent: 'space-between', alignItems: 'center' }}>
                  <span className="text-tertiary">WebSocket:</span>
                  <span className="text-secondary">{urls.websocket}</span>
                </div>
                <div style={{ display: 'flex', justifyContent: 'space-between', alignItems: 'center' }}>
                  <span className="text-tertiary">HTTP:</span>
                  <span className="text-secondary">{urls.http}</span>
                </div>
              </div>

              <div style={{ display: 'flex', gap: 'var(--space-xs)', marginTop: 'var(--space-sm)' }}>
                <button
                  className="btn btn-secondary"
                  style={{ flex: 1 }}
                  onClick={() => copyToClipboard(JSON.stringify(urls))}
                >
                  Copy URLs
                </button>
                <button
                  className="btn btn-secondary"
                  style={{ flex: 1 }}
                  onClick={loadData}
                >
                  Refresh
                </button>
              </div>
            </div>
          )}
        </div>

        {/* Recent Activity */}
        <div className="card">
          <div className="card-header">
            <h3 className="card-title">Recent Activity</h3>
          </div>

          <div className="activity-list">
            <div className="activity-item">
              <span className="activity-time">Now</span>
              <span className="activity-text text-success">Server started</span>
            </div>
            <div className="activity-item">
              <span className="activity-time">--:--</span>
              <span className="activity-text">Waiting for connections...</span>
            </div>
          </div>

          <div style={{ marginTop: 'var(--space-lg)', paddingTop: 'var(--space-md)', borderTop: '1px solid var(--border-default)' }}>
            <div className="card-header" style={{ marginBottom: 'var(--space-xs)' }}>
              <span className="text-tertiary" style={{ fontSize: 'var(--text-xs)', textTransform: 'uppercase' }}>
                Active Repository
              </span>
            </div>
            <div style={{ display: 'flex', alignItems: 'center', gap: 'var(--space-sm)' }}>
              <svg width="16" height="16" viewBox="0 0 24 24" fill="none" stroke="var(--brand-coral)" strokeWidth="2">
                <path d="M22 19a2 2 0 01-2 2H4a2 2 0 01-2-2V5a2 2 0 012-2h5l2 3h9a2 2 0 012 2z" />
              </svg>
              <span style={{ fontWeight: 500 }}>{status?.active_repo || 'No repository selected'}</span>
            </div>
          </div>
        </div>
      </div>
    </div>
  )
}

export default Dashboard

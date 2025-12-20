import { useState, useEffect } from 'react'
import { GetConfig, UpdateConfig } from '../../wailsjs/go/main/DesktopApp'

interface Config {
  http_port: number
  address: string
  claude_path: string
  skip_permissions: boolean
  session_mode: string
  repository_path: string
}

function Settings() {
  const [config, setConfig] = useState<Config | null>(null)
  const [loading, setLoading] = useState(true)
  const [saving, setSaving] = useState(false)

  useEffect(() => {
    loadConfig()
  }, [])

  async function loadConfig() {
    try {
      const data = await GetConfig()
      setConfig(data as Config)
    } catch (err) {
      console.error('Failed to load config:', err)
    } finally {
      setLoading(false)
    }
  }

  async function handleChange(key: keyof Config, value: any) {
    if (!config) return

    // Update local state
    setConfig({ ...config, [key]: value })

    // Save to backend
    setSaving(true)
    try {
      await UpdateConfig(key, value)
    } catch (err) {
      console.error('Failed to update config:', err)
    } finally {
      setSaving(false)
    }
  }

  if (loading) {
    return (
      <div style={{ display: 'flex', justifyContent: 'center', alignItems: 'center', height: '100%' }}>
        <div className="text-secondary">Loading...</div>
      </div>
    )
  }

  if (!config) {
    return (
      <div style={{ display: 'flex', justifyContent: 'center', alignItems: 'center', height: '100%' }}>
        <div className="text-error">Failed to load configuration</div>
      </div>
    )
  }

  return (
    <div style={{ maxWidth: 600, margin: '0 auto' }}>
      {/* Server Settings */}
      <section style={{ marginBottom: 'var(--space-xl)' }}>
        <h3 style={{ marginBottom: 'var(--space-md)', color: 'var(--text-tertiary)', fontSize: 'var(--text-xs)', textTransform: 'uppercase', letterSpacing: '0.5px' }}>
          Server
        </h3>

        <div className="card">
          <div style={{ display: 'flex', flexDirection: 'column', gap: 'var(--space-md)' }}>
            <div>
              <label style={{ display: 'block', marginBottom: 'var(--space-xs)', fontSize: 'var(--text-sm)', color: 'var(--text-secondary)' }}>
                HTTP Port
              </label>
              <input
                type="number"
                className="input"
                value={config.http_port}
                onChange={(e) => handleChange('http_port', parseInt(e.target.value))}
                style={{ width: 120 }}
              />
            </div>

            <div>
              <label style={{ display: 'block', marginBottom: 'var(--space-xs)', fontSize: 'var(--text-sm)', color: 'var(--text-secondary)' }}>
                Bind Address
              </label>
              <input
                type="text"
                className="input input-mono"
                value={config.address}
                onChange={(e) => handleChange('address', e.target.value)}
                placeholder="0.0.0.0"
              />
              <p style={{ marginTop: 'var(--space-xxs)', fontSize: 'var(--text-xs)', color: 'var(--text-tertiary)' }}>
                Use 0.0.0.0 to accept connections from any device on the network
              </p>
            </div>
          </div>
        </div>
      </section>

      {/* Claude Settings */}
      <section style={{ marginBottom: 'var(--space-xl)' }}>
        <h3 style={{ marginBottom: 'var(--space-md)', color: 'var(--text-tertiary)', fontSize: 'var(--text-xs)', textTransform: 'uppercase', letterSpacing: '0.5px' }}>
          Claude
        </h3>

        <div className="card">
          <div style={{ display: 'flex', flexDirection: 'column', gap: 'var(--space-md)' }}>
            <div>
              <label style={{ display: 'block', marginBottom: 'var(--space-xs)', fontSize: 'var(--text-sm)', color: 'var(--text-secondary)' }}>
                Claude CLI Path
              </label>
              <input
                type="text"
                className="input input-mono"
                value={config.claude_path}
                onChange={(e) => handleChange('claude_path', e.target.value)}
                placeholder="/usr/local/bin/claude"
              />
            </div>

            <div>
              <label style={{ display: 'block', marginBottom: 'var(--space-xs)', fontSize: 'var(--text-sm)', color: 'var(--text-secondary)' }}>
                Session Mode
              </label>
              <div style={{ display: 'flex', gap: 'var(--space-sm)' }}>
                {['new', 'continue', 'resume'].map((mode) => (
                  <label
                    key={mode}
                    style={{
                      display: 'flex',
                      alignItems: 'center',
                      gap: 'var(--space-xs)',
                      padding: 'var(--space-xs) var(--space-sm)',
                      background: config.session_mode === mode ? 'var(--bg-selected)' : 'var(--bg-deep)',
                      border: `1px solid ${config.session_mode === mode ? 'var(--primary-cyan)' : 'var(--border-default)'}`,
                      borderRadius: 'var(--radius-md)',
                      cursor: 'pointer',
                      transition: 'all var(--transition-fast)'
                    }}
                  >
                    <input
                      type="radio"
                      name="session_mode"
                      value={mode}
                      checked={config.session_mode === mode}
                      onChange={() => handleChange('session_mode', mode)}
                      style={{ display: 'none' }}
                    />
                    <span style={{ textTransform: 'capitalize' }}>{mode}</span>
                  </label>
                ))}
              </div>
            </div>

            <div style={{ display: 'flex', alignItems: 'center', justifyContent: 'space-between' }}>
              <div>
                <label style={{ fontSize: 'var(--text-sm)', color: 'var(--text-secondary)' }}>
                  Skip Permission Prompts
                </label>
                <p style={{ fontSize: 'var(--text-xs)', color: 'var(--text-tertiary)' }}>
                  Auto-approve file operations (use with caution)
                </p>
              </div>
              <button
                className={`btn ${config.skip_permissions ? 'btn-primary' : 'btn-secondary'}`}
                onClick={() => handleChange('skip_permissions', !config.skip_permissions)}
                style={{ minWidth: 60 }}
              >
                {config.skip_permissions ? 'On' : 'Off'}
              </button>
            </div>
          </div>
        </div>
      </section>

      {/* About */}
      <section>
        <h3 style={{ marginBottom: 'var(--space-md)', color: 'var(--text-tertiary)', fontSize: 'var(--text-xs)', textTransform: 'uppercase', letterSpacing: '0.5px' }}>
          About
        </h3>

        <div className="card">
          <div style={{ textAlign: 'center', padding: 'var(--space-md)' }}>
            <div style={{
              width: 64,
              height: 64,
              borderRadius: 'var(--radius-lg)',
              background: 'linear-gradient(135deg, var(--brand-coral), var(--primary-cyan))',
              display: 'flex',
              alignItems: 'center',
              justifyContent: 'center',
              margin: '0 auto var(--space-md)',
              fontSize: '24px',
              fontWeight: 'bold',
              color: 'white'
            }}>
              CD
            </div>
            <h4 style={{ marginBottom: 'var(--space-xxs)' }}>cdev Desktop</h4>
            <p className="text-secondary" style={{ fontSize: 'var(--text-sm)', marginBottom: 'var(--space-sm)' }}>
              Enterprise Edition
            </p>
            <p className="text-tertiary" style={{ fontSize: 'var(--text-xs)' }}>
              Version 1.0.0
            </p>
          </div>
        </div>
      </section>

      {/* Saving Indicator */}
      {saving && (
        <div style={{
          position: 'fixed',
          bottom: 'var(--space-lg)',
          right: 'var(--space-lg)',
          padding: 'var(--space-sm) var(--space-md)',
          background: 'var(--bg-elevated)',
          border: '1px solid var(--border-default)',
          borderRadius: 'var(--radius-md)',
          fontSize: 'var(--text-sm)',
          color: 'var(--text-secondary)'
        }}>
          Saving...
        </div>
      )}
    </div>
  )
}

export default Settings

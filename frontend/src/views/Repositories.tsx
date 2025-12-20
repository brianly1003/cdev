import { useState, useEffect } from 'react'
import {
  GetRepositories,
  AddRepository,
  RemoveRepository,
  SwitchRepository,
  OpenDirectoryDialog
} from '../../wailsjs/go/main/DesktopApp'

interface Repository {
  id: string
  path: string
  display_name: string
  git_branch?: string
  git_remote?: string
  is_active: boolean
  last_active?: string
}

function Repositories() {
  const [repos, setRepos] = useState<Repository[]>([])
  const [loading, setLoading] = useState(true)

  useEffect(() => {
    loadRepos()
  }, [])

  async function loadRepos() {
    try {
      const data = await GetRepositories()
      setRepos(data || [])
    } catch (err) {
      console.error('Failed to load repos:', err)
    } finally {
      setLoading(false)
    }
  }

  async function handleAddRepo() {
    try {
      const path = await OpenDirectoryDialog()
      if (path) {
        await AddRepository(path, '')
        loadRepos()
      }
    } catch (err) {
      console.error('Failed to add repo:', err)
    }
  }

  async function handleRemoveRepo(id: string) {
    try {
      await RemoveRepository(id)
      loadRepos()
    } catch (err) {
      console.error('Failed to remove repo:', err)
    }
  }

  async function handleSwitchRepo(id: string) {
    try {
      await SwitchRepository(id)
      loadRepos()
    } catch (err) {
      console.error('Failed to switch repo:', err)
    }
  }

  if (loading) {
    return (
      <div style={{ display: 'flex', justifyContent: 'center', alignItems: 'center', height: '100%' }}>
        <div className="text-secondary">Loading...</div>
      </div>
    )
  }

  return (
    <div style={{ maxWidth: 800, margin: '0 auto' }}>
      {/* Header */}
      <div style={{ display: 'flex', justifyContent: 'space-between', alignItems: 'center', marginBottom: 'var(--space-lg)' }}>
        <div>
          <h2 style={{ marginBottom: 'var(--space-xxs)' }}>Manage Repositories</h2>
          <p className="text-secondary" style={{ fontSize: 'var(--text-sm)' }}>
            Add and switch between repositories
          </p>
        </div>
        <button className="btn btn-primary" onClick={handleAddRepo}>
          <svg width="16" height="16" viewBox="0 0 24 24" fill="none" stroke="currentColor" strokeWidth="2">
            <line x1="12" y1="5" x2="12" y2="19" />
            <line x1="5" y1="12" x2="19" y2="12" />
          </svg>
          Add Repository
        </button>
      </div>

      {/* Repository List */}
      <div className="repo-list">
        {repos.length === 0 ? (
          <div
            className="drop-zone"
            onClick={handleAddRepo}
          >
            <svg width="48" height="48" viewBox="0 0 24 24" fill="none" stroke="currentColor" strokeWidth="1.5">
              <path d="M22 19a2 2 0 01-2 2H4a2 2 0 01-2-2V5a2 2 0 012-2h5l2 3h9a2 2 0 012 2z" />
              <line x1="12" y1="11" x2="12" y2="17" />
              <line x1="9" y1="14" x2="15" y2="14" />
            </svg>
            <span>Click to add a repository</span>
            <span style={{ fontSize: 'var(--text-xs)' }}>or drag and drop a folder here</span>
          </div>
        ) : (
          repos.map((repo) => (
            <div key={repo.id} className={`repo-item ${repo.is_active ? 'active' : ''}`}>
              <div className="repo-info">
                <div style={{ display: 'flex', alignItems: 'center', gap: 'var(--space-sm)' }}>
                  <span className="repo-name">{repo.display_name}</span>
                  {repo.is_active && (
                    <span className="badge badge-active">Active</span>
                  )}
                </div>
                <div className="repo-path">{repo.path}</div>
                <div className="repo-meta">
                  {repo.git_branch && (
                    <>
                      <svg width="12" height="12" viewBox="0 0 24 24" fill="none" stroke="currentColor" strokeWidth="2">
                        <line x1="6" y1="3" x2="6" y2="15" />
                        <circle cx="18" cy="6" r="3" />
                        <circle cx="6" cy="18" r="3" />
                        <path d="M18 9a9 9 0 01-9 9" />
                      </svg>
                      <span>{repo.git_branch}</span>
                    </>
                  )}
                  {repo.last_active && (
                    <>
                      <span>•</span>
                      <span>Last active: {repo.last_active}</span>
                    </>
                  )}
                </div>
              </div>

              <div className="repo-actions">
                {!repo.is_active && (
                  <button
                    className="btn btn-secondary"
                    onClick={() => handleSwitchRepo(repo.id)}
                  >
                    Switch
                  </button>
                )}
                <button
                  className="btn btn-ghost"
                  onClick={() => handleRemoveRepo(repo.id)}
                  style={{ color: 'var(--error-crimson)' }}
                >
                  <svg width="16" height="16" viewBox="0 0 24 24" fill="none" stroke="currentColor" strokeWidth="2">
                    <polyline points="3 6 5 6 21 6" />
                    <path d="M19 6v14a2 2 0 01-2 2H7a2 2 0 01-2-2V6m3 0V4a2 2 0 012-2h4a2 2 0 012 2v2" />
                  </svg>
                </button>
              </div>
            </div>
          ))
        )}
      </div>

      {repos.length > 0 && (
        <div
          className="drop-zone"
          onClick={handleAddRepo}
          style={{ marginTop: 'var(--space-md)' }}
        >
          <svg width="24" height="24" viewBox="0 0 24 24" fill="none" stroke="currentColor" strokeWidth="2">
            <line x1="12" y1="5" x2="12" y2="19" />
            <line x1="5" y1="12" x2="19" y2="12" />
          </svg>
          <span>Add another repository</span>
        </div>
      )}
    </div>
  )
}

export default Repositories

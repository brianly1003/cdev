import { useState, useEffect } from 'react'
import Dashboard from './views/Dashboard'
import Repositories from './views/Repositories'
import Settings from './views/Settings'

type View = 'dashboard' | 'repositories' | 'settings'

function App() {
  const [currentView, setCurrentView] = useState<View>('dashboard')

  return (
    <div className="app-container">
      {/* Header */}
      <header className="header">
        <div style={{ display: 'flex', alignItems: 'center', gap: 'var(--space-md)' }}>
          {currentView !== 'dashboard' && (
            <button
              className="btn btn-ghost"
              onClick={() => setCurrentView('dashboard')}
              style={{ padding: 'var(--space-xs)' }}
            >
              <svg width="20" height="20" viewBox="0 0 24 24" fill="none" stroke="currentColor" strokeWidth="2">
                <path d="M15 18l-6-6 6-6" />
              </svg>
            </button>
          )}
          <h1 className="header-title">
            {currentView === 'dashboard' && 'Dashboard'}
            {currentView === 'repositories' && 'Repositories'}
            {currentView === 'settings' && 'Settings'}
          </h1>
        </div>

        <div style={{ display: 'flex', gap: 'var(--space-xs)' }}>
          <button
            className={`btn ${currentView === 'repositories' ? 'btn-primary' : 'btn-ghost'}`}
            onClick={() => setCurrentView('repositories')}
          >
            <svg width="16" height="16" viewBox="0 0 24 24" fill="none" stroke="currentColor" strokeWidth="2">
              <path d="M3 3h7v7H3zM14 3h7v7h-7zM14 14h7v7h-7zM3 14h7v7H3z" />
            </svg>
            Repos
          </button>
          <button
            className={`btn ${currentView === 'settings' ? 'btn-primary' : 'btn-ghost'}`}
            onClick={() => setCurrentView('settings')}
          >
            <svg width="16" height="16" viewBox="0 0 24 24" fill="none" stroke="currentColor" strokeWidth="2">
              <circle cx="12" cy="12" r="3" />
              <path d="M19.4 15a1.65 1.65 0 00.33 1.82l.06.06a2 2 0 010 2.83 2 2 0 01-2.83 0l-.06-.06a1.65 1.65 0 00-1.82-.33 1.65 1.65 0 00-1 1.51V21a2 2 0 01-2 2 2 2 0 01-2-2v-.09A1.65 1.65 0 009 19.4a1.65 1.65 0 00-1.82.33l-.06.06a2 2 0 01-2.83 0 2 2 0 010-2.83l.06-.06a1.65 1.65 0 00.33-1.82 1.65 1.65 0 00-1.51-1H3a2 2 0 01-2-2 2 2 0 012-2h.09A1.65 1.65 0 004.6 9a1.65 1.65 0 00-.33-1.82l-.06-.06a2 2 0 010-2.83 2 2 0 012.83 0l.06.06a1.65 1.65 0 001.82.33H9a1.65 1.65 0 001-1.51V3a2 2 0 012-2 2 2 0 012 2v.09a1.65 1.65 0 001 1.51 1.65 1.65 0 001.82-.33l.06-.06a2 2 0 012.83 0 2 2 0 010 2.83l-.06.06a1.65 1.65 0 00-.33 1.82V9a1.65 1.65 0 001.51 1H21a2 2 0 012 2 2 2 0 01-2 2h-.09a1.65 1.65 0 00-1.51 1z" />
            </svg>
            Settings
          </button>
        </div>
      </header>

      {/* Main Content */}
      <main className="main-content">
        {currentView === 'dashboard' && <Dashboard />}
        {currentView === 'repositories' && <Repositories />}
        {currentView === 'settings' && <Settings />}
      </main>
    </div>
  )
}

export default App

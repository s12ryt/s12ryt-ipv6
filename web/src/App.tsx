import { FormEvent, useEffect, useMemo, useState } from 'react'
import {
  Activity,
  Boxes,
  CircleGauge,
  FileClock,
  Globe2,
  LockKeyhole,
  LogOut,
  Monitor,
  Moon,
  Network,
  ShieldAlert,
  Sun,
} from 'lucide-react'
import { AdminEvent, APIError, ApiClient, InitialData } from './api'
import { NodesView } from './NodesView'
import { NetworkView } from './NetworkView'
import { LogsView } from './LogsView'
import { ResourcesView } from './ResourcesView'
import { PanelMode, persistPanelMode, storedPanelMode } from './panelMode'

type AppPhase = 'checking' | 'login' | 'loading' | 'ready'
type Theme = 'system' | 'light' | 'dark'
type View = 'overview' | 'nodes' | 'resources' | 'network' | 'logs'

const navigation: Array<{ id: View; label: string; icon: typeof CircleGauge }> = [
  { id: 'overview', label: '總覽', icon: CircleGauge },
  { id: 'nodes', label: '節點', icon: Boxes },
  { id: 'resources', label: 'IPv6 資源', icon: Globe2 },
  { id: 'network', label: '網路', icon: Network },
  { id: 'logs', label: '日誌', icon: FileClock },
]

function storedTheme(): Theme {
  const value = localStorage.getItem('s12ryt_theme')
  return value === 'light' || value === 'dark' ? value : 'system'
}

function applyTheme(theme: Theme) {
  localStorage.setItem('s12ryt_theme', theme)
  if (theme === 'system') document.documentElement.removeAttribute('data-theme')
  else document.documentElement.dataset.theme = theme
}

export function App() {
  const client = useMemo(() => new ApiClient(), [])
  const [phase, setPhase] = useState<AppPhase>('checking')
  const [data, setData] = useState<InitialData | null>(null)
  const [view, setView] = useState<View>('overview')
  const [theme, setTheme] = useState<Theme>(storedTheme)
  const [panelMode, setPanelMode] = useState<PanelMode>(storedPanelMode)
  const [error, setError] = useState('')
  const [logRevision, setLogRevision] = useState(0)

  useEffect(() => applyTheme(theme), [theme])
  useEffect(() => persistPanelMode(panelMode), [panelMode])

  useEffect(() => {
    let active = true
    void client
      .currentSession()
      .then(async (authenticated) => {
        if (!active) return
        if (!authenticated) {
          setPhase('login')
          return
        }
        setPhase('loading')
        const initial = await client.loadInitial()
        if (active) {
          setData(initial)
          setPhase('ready')
        }
      })
      .catch(() => {
        if (active) {
          setError('無法連線至管理服務')
          setPhase('login')
        }
      })
    return () => {
      active = false
    }
  }, [client])

  useEffect(() => {
    if (phase !== 'ready') return
    let active = true
    const refresh = async (event: AdminEvent) => {
      try {
        if (event.resource === 'node') {
          const nodes = await client.get<InitialData['nodes']>('/api/nodes')
          if (active) setData((current) => current ? { ...current, nodes } : current)
        } else if (event.resource === 'template' || event.resource === 'fixed-address' || event.resource === 'pool') {
          const resources = await client.get<InitialData['resources']>('/api/resources')
          if (active) setData((current) => current ? { ...current, resources } : current)
        } else if (event.resource === 'statistics') {
          const statistics = await client.get<InitialData['statistics']>('/api/stats')
          if (active) setData((current) => current ? { ...current, statistics } : current)
        } else if (event.resource === 'nat64' || event.resource === 'resolver' || event.resource === 'system') {
          const overview = await client.get<InitialData['overview']>('/api/overview')
          if (active) setData((current) => current ? { ...current, overview } : current)
        } else if (event.resource === 'log') {
          if (active) setLogRevision((current) => current + 1)
        }
      } catch {
        if (active) setError('即時資料更新失敗，請稍後重新整理')
      }
    }
    const close = client.subscribe((event) => void refresh(event), () => {
      if (active) setError('即時更新連線中斷，資料可能延遲')
    })
    return () => {
      active = false
      close()
    }
  }, [client, phase])

  const login = async (password: string) => {
    setError('')
    setPhase('loading')
    try {
      await client.login(password)
      const initial = await client.loadInitial()
      setData(initial)
      setPhase('ready')
    } catch (reason) {
      if (reason instanceof APIError && reason.status === 401) {
        setError('登入失敗，請檢查管理員密碼')
      } else {
        setError(reason instanceof APIError ? reason.message : '登入失敗')
      }
      setPhase('login')
    }
  }

  const logout = async () => {
    setError('')
    try {
      await client.logout()
    } catch {
      setError('登出要求未完成，本機工作階段已清除')
    } finally {
      setData(null)
      setPhase('login')
    }
  }

  if (phase === 'checking' || phase === 'loading') {
    return (
      <main className="loading-page" aria-busy="true">
        <Activity className="loading-icon" aria-hidden="true" />
        <span>{phase === 'checking' ? '正在確認管理工作階段' : '正在載入節點狀態'}</span>
      </main>
    )
  }

  if (phase === 'login' || data === null) {
    return <LoginView error={error} onLogin={login} theme={theme} onThemeChange={setTheme} />
  }

  return (
    <div className="app-shell">
      <HTTPWarning />
      <header className="topbar">
        <div className="brand-lockup">
          <span className="product-mark" aria-hidden="true"><LockKeyhole size={19} /></span>
          <div><strong>s12ryt IPv6</strong><span>出口節點控制台</span></div>
        </div>
        <div className="topbar-actions">
          <ModeControl mode={panelMode} onChange={setPanelMode} />
          <ThemeControl theme={theme} onChange={setTheme} />
          <button className="icon-text-button" type="button" onClick={() => void logout()}>
            <LogOut size={17} aria-hidden="true" />登出
          </button>
        </div>
      </header>
      <aside className="sidebar">
        <nav aria-label="主要導覽">
          {navigation.map((item) => {
            const Icon = item.icon
            return (
              <button
                className={view === item.id ? 'nav-button active' : 'nav-button'}
                type="button"
                key={item.id}
                aria-current={view === item.id ? 'page' : undefined}
                onClick={() => setView(item.id)}
              >
                <Icon size={18} aria-hidden="true" /><span>{item.label}</span>
              </button>
            )
          })}
        </nav>
      </aside>
      <main className="workspace">
        {error && <div className="inline-error" role="alert">{error}</div>}
        {view === 'overview' && <OverviewView data={data} />}
        {view === 'nodes' && <NodesView mode={panelMode} client={client} nodes={data.nodes} resources={data.resources} onChange={(nodes) => setData({ ...data, nodes })} />}
        {view === 'resources' && <ResourcesView mode={panelMode} client={client} resources={data.resources} onChange={(resources) => setData({ ...data, resources })} />}
        {view === 'network' && <NetworkView mode={panelMode} client={client} overview={data.overview} onChange={(overview) => setData({ ...data, overview })} onPasswordChanged={() => { setData(null); setPhase('login') }} />}
        {view === 'logs' && <LogsView key={logRevision} mode={panelMode} client={client} statistics={data.statistics} onStatisticsChange={(statistics) => setData({ ...data, statistics })} />}
      </main>
    </div>
  )
}

function LoginView({
  error,
  onLogin,
  theme,
  onThemeChange,
}: {
  error: string
  onLogin: (password: string) => Promise<void>
  theme: Theme
  onThemeChange: (theme: Theme) => void
}) {
  const [password, setPassword] = useState('')
  const submit = (event: FormEvent<HTMLFormElement>) => {
    event.preventDefault()
    if (password) void onLogin(password)
  }
  return (
    <main className="login-page">
      <div className="login-theme"><ThemeControl theme={theme} onChange={onThemeChange} /></div>
      <section className="login-panel" aria-labelledby="login-title">
        <header>
          <span className="product-mark" aria-hidden="true"><LockKeyhole size={20} /></span>
          <div><h1 id="login-title">s12ryt IPv6 管理中心</h1><p>請驗證管理員身分</p></div>
        </header>
        <HTTPWarning />
        {error && <div className="inline-error" role="alert">{error}</div>}
        <form onSubmit={submit}>
          <label htmlFor="admin-password">管理員密碼</label>
          <input
            id="admin-password"
            name="password"
            type="password"
            value={password}
            onChange={(event) => setPassword(event.target.value)}
            autoComplete="current-password"
            required
          />
          <button type="submit">登入</button>
        </form>
      </section>
    </main>
  )
}

function HTTPWarning() {
  return (
    <div className="risk-alert" role="alert">
      <ShieldAlert size={18} aria-hidden="true" />
      <span>此管理介面使用未加密 HTTP，登入資訊可能被網路上的第三方讀取。</span>
    </div>
  )
}

function ThemeControl({ theme, onChange }: { theme: Theme; onChange: (theme: Theme) => void }) {
  const choices: Array<{ id: Theme; label: string; icon: typeof Monitor }> = [
    { id: 'system', label: '跟隨系統主題', icon: Monitor },
    { id: 'light', label: '使用亮色主題', icon: Sun },
    { id: 'dark', label: '使用深色主題', icon: Moon },
  ]
  return (
    <div className="theme-control" role="group" aria-label="顯示主題">
      {choices.map((choice) => {
        const Icon = choice.icon
        return (
          <button
            key={choice.id}
            type="button"
            title={choice.label}
            aria-label={choice.label}
            aria-pressed={theme === choice.id}
            onClick={() => onChange(choice.id)}
          >
            <Icon size={16} aria-hidden="true" />
          </button>
        )
      })}
    </div>
  )
}

function ModeControl({ mode, onChange }: { mode: PanelMode; onChange: (mode: PanelMode) => void }) {
  const choices: Array<{ id: PanelMode; label: string }> = [
    { id: 'basic', label: '基礎模式' },
    { id: 'advanced', label: '進階模式' },
  ]
  return (
    <div className="mode-control" role="group" aria-label="介面模式">
      {choices.map((choice) => (
        <button
          key={choice.id}
          type="button"
          aria-pressed={mode === choice.id}
          onClick={() => onChange(choice.id)}
        >
          {choice.label}
        </button>
      ))}
    </div>
  )
}

function OverviewView({ data }: { data: InitialData }) {
  const totals = Object.values(data.statistics.nodes).reduce(
    (sum, item) => ({
      active: sum.active + item.active_tcp + item.active_udp,
      connections: sum.connections + item.total_connections,
      errors: sum.errors + item.errors,
    }),
    { active: 0, connections: 0, errors: 0 },
  )
  const running = data.nodes.filter((item) => item.status === 'running').length
  return (
    <section aria-labelledby="page-title">
      <div className="page-heading">
        <div><p className="eyebrow">系統狀態</p><h1 id="page-title">總覽</h1></div>
        <StatusBadge state={data.overview.health} />
      </div>
      <div className="metrics" aria-label="即時統計">
        <Metric label="運行節點" value={`${running} / ${data.nodes.length}`} />
        <Metric label="活躍連線" value={String(totals.active)} />
        <Metric label="累計連線" value={totals.connections.toLocaleString('zh-TW')} />
        <Metric label="錯誤" value={totals.errors.toLocaleString('zh-TW')} tone={totals.errors > 0 ? 'danger' : undefined} />
      </div>
      <div className="overview-grid">
        <section className="data-section" aria-labelledby="nat64-title">
          <div className="section-heading"><h2 id="nat64-title">NAT64</h2><StatusBadge state={data.overview.nat64.state} /></div>
          <dl className="detail-list">
            <div><dt>前綴</dt><dd className="mono">{data.overview.nat64.prefix ?? '尚未探索'}</dd></div>
            <div><dt>來源</dt><dd>{data.overview.nat64.source ?? '無'}</dd></div>
            <div><dt>模式</dt><dd>{data.overview.nat64.manual ? '手動' : '自動探索'}</dd></div>
          </dl>
        </section>
        <section className="data-section" aria-labelledby="nodes-title">
          <div className="section-heading"><h2 id="nodes-title">節點狀態</h2><span className="quiet-count">{data.nodes.length} 個</span></div>
          <div className="compact-table" role="table" aria-label="節點狀態">
            {data.nodes.length === 0 ? <p className="empty-state">尚未建立節點</p> : data.nodes.map((item) => (
              <div className="table-row" role="row" key={item.id}>
                <div role="cell"><strong>{item.name}</strong><span>{item.protocol.toUpperCase()} · {item.port}</span></div>
                <div role="cell"><StatusBadge state={item.status === 'running' ? 'healthy' : 'unhealthy'} label={item.status === 'running' ? '運行中' : '已停止'} /></div>
              </div>
            ))}
          </div>
        </section>
      </div>
    </section>
  )
}

function Metric({ label, value, tone }: { label: string; value: string; tone?: 'danger' }) {
  return <div className="metric"><span>{label}</span><strong className={tone === 'danger' ? 'danger-text' : ''}>{value}</strong></div>
}

function StatusBadge({ state, label }: { state: HealthStateLike; label?: string }) {
  const text = label ?? ({ healthy: '正常', degraded: '降級', unhealthy: '異常' }[state] ?? state)
  return <span className={`status-badge status-${state}`}>{text}</span>
}

type HealthStateLike = 'healthy' | 'degraded' | 'unhealthy'

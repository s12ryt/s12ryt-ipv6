import { FormEvent, useCallback, useEffect, useState } from 'react'
import { Filter, RefreshCw, RotateCcw, Trash2 } from 'lucide-react'
import { APIError, ApiClient, LogEvent, LogKind, StatisticsSnapshot } from './api'
import type { PanelMode } from './panelMode'
import { ModalDialog } from './ModalDialog'

type LogsClient = Pick<ApiClient, 'get' | 'mutate'>
type ConfirmAction = { kind: 'logs' } | { kind: 'stats'; node: string } | null

export function LogsView({ mode, client, statistics, onStatisticsChange }: {
  mode: PanelMode
  client: LogsClient
  statistics: StatisticsSnapshot
  onStatisticsChange: (statistics: StatisticsSnapshot) => void
}) {
  const [events, setEvents] = useState<LogEvent[]>([])
  const [kind, setKind] = useState<LogKind | ''>('')
  const [node, setNode] = useState('')
  const [action, setAction] = useState('')
  const [success, setSuccess] = useState('')
  const [limit, setLimit] = useState('200')
  const [confirm, setConfirm] = useState<ConfirmAction>(null)
  const [busy, setBusy] = useState('')
  const [error, setError] = useState('')

  const logPath = useCallback(() => {
    const query = new URLSearchParams()
    if (kind) query.set('kind', kind)
    if (node.trim()) query.set('node', node.trim())
    if (action.trim()) query.set('action', action.trim())
    if (success) query.set('success', success)
    query.set('limit', limit || '200')
    return `/api/logs?${query.toString()}`
  }, [action, kind, limit, node, success])

  const loadLogs = useCallback(async () => {
    setError('')
    try {
      setEvents(await client.get<LogEvent[]>(logPath()))
    } catch (reason) {
      setError(reason instanceof APIError ? reason.message : '日誌載入失敗')
    }
  }, [client, logPath])

  useEffect(() => { void loadLogs() }, [loadLogs])

  const submitFilters = (event: FormEvent<HTMLFormElement>) => { event.preventDefault(); void loadLogs() }

  const clearLogs = async () => {
    setBusy('logs')
    setError('')
    try {
      await client.mutate<void>('/api/logs/clear', 'POST', { confirm: true })
      setConfirm(null)
      await loadLogs()
    } catch (reason) {
      setError(reason instanceof APIError ? reason.message : '日誌清除失敗')
    } finally {
      setBusy('')
    }
  }

  const resetStatistics = async (target: string) => {
    setBusy(`stats:${target}`)
    setError('')
    try {
      await client.mutate<void>('/api/stats/reset', 'POST', { node: target, confirm: true })
      const refreshed = await client.get<StatisticsSnapshot>('/api/stats')
      onStatisticsChange(refreshed)
      setConfirm(null)
    } catch (reason) {
      setError(reason instanceof APIError ? reason.message : '統計歸零失敗')
    } finally {
      setBusy('')
    }
  }

  return (
    <section aria-labelledby="page-title">
      <div className="page-heading"><div><p className="eyebrow">稽核與流量中繼資料</p><h1 id="page-title">日誌</h1></div><button className="secondary-button" type="button" disabled={busy !== ''} onClick={() => void loadLogs()}><RefreshCw size={16} aria-hidden="true" />重新整理</button></div>
      {error && <div className="inline-error page-message" role="alert">{error}</div>}

      <section className="data-section" aria-labelledby="statistics-title">
        <div className="section-heading"><h2 id="statistics-title">節點統計</h2>{mode === 'advanced' && <button className="secondary-button" type="button" onClick={() => setConfirm({ kind: 'stats', node: '' })}><RotateCcw size={16} aria-hidden="true" />歸零全部統計</button>}</div>
        <div className={`stats-table${mode === 'basic' ? ' basic' : ''}`} role="table" aria-label="節點統計">
          <div className="stats-head" role="row"><span>節點</span><span>活躍</span><span>累計</span><span>上行</span><span>下行</span><span>錯誤</span>{mode === 'advanced' && <span>操作</span>}</div>
          {Object.entries(statistics.nodes).sort(([left], [right]) => left.localeCompare(right)).map(([name, counters]) => (
            <div className="stats-row" role="row" aria-label={`${name} 統計`} key={name}>
              <strong>{name}</strong><span>{counters.active_tcp + counters.active_udp}</span><span>{counters.total_connections.toLocaleString('zh-TW')}</span><span>{formatBytes(counters.bytes_up)}</span><span>{formatBytes(counters.bytes_down)}</span><span className={counters.errors ? 'danger-text' : ''}>{counters.errors.toLocaleString('zh-TW')}</span>
              {mode === 'advanced' && <div><button className="icon-button" type="button" title={`歸零 ${name} 統計`} aria-label={`歸零 ${name} 統計`} onClick={() => setConfirm({ kind: 'stats', node: name })}><RotateCcw size={16} aria-hidden="true" /></button></div>}
            </div>
          ))}
          {Object.keys(statistics.nodes).length === 0 && <p className="empty-state">目前沒有節點統計</p>}
        </div>
      </section>

      <section className="resource-section" aria-labelledby="events-title">
        <div className="section-heading"><h2 id="events-title">事件</h2>{mode === 'advanced' && <button className="danger-button" type="button" onClick={() => setConfirm({ kind: 'logs' })}><Trash2 size={16} aria-hidden="true" />清除全部日誌</button>}</div>
        <form className="log-filters" aria-label="日誌篩選" onSubmit={submitFilters}>
          <label className="field"><span>事件類型</span><select value={kind} onChange={(event) => setKind(event.target.value as LogKind | '')}><option value="">全部</option><option value="proxy">代理</option><option value="system">系統</option><option value="audit">稽核</option></select></label>
          <label className="field"><span>節點篩選</span><input value={node} maxLength={128} onChange={(event) => setNode(event.target.value)} /></label>
          <label className="field"><span>動作篩選</span><input value={action} maxLength={128} onChange={(event) => setAction(event.target.value)} /></label>
          <label className="field"><span>結果</span><select value={success} onChange={(event) => setSuccess(event.target.value)}><option value="">全部</option><option value="true">成功</option><option value="false">失敗</option></select></label>
          <label className="field"><span>筆數</span><input type="number" min="1" max="1000" value={limit} onChange={(event) => setLimit(event.target.value)} required /></label>
          <button className="primary-button filter-button" type="submit"><Filter size={16} aria-hidden="true" />套用篩選</button>
        </form>
        <div className="event-list">
          {events.map((event, index) => <EventRow event={event} key={`${event.time}:${event.kind}:${event.action}:${index}`} />)}
          {events.length === 0 && <p className="empty-state">沒有符合條件的事件</p>}
        </div>
      </section>
      {confirm && <ModalDialog title={confirmationTitle(confirm)} onClose={() => setConfirm(null)} size="medium" footer={(requestClose) => <><button className="secondary-button" type="button" onClick={requestClose}>取消</button><button className="danger-button" type="button" disabled={busy !== ''} onClick={() => confirm.kind === 'logs' ? void clearLogs() : void resetStatistics(confirm.node)}>確認{confirm.kind === 'logs' ? '清除全部日誌' : '歸零'}</button></>}><p>{confirmationMessage(confirm)}</p></ModalDialog>}
    </section>
  )
}

function EventRow({ event }: { event: LogEvent }) {
  const destination = event.destination_host ? `${event.destination_host}${event.destination_port ? `:${event.destination_port}` : ''}` : ''
  return <article className="event-row"><time dateTime={event.time}>{formatTime(event.time)}</time><span className={`event-kind kind-${event.kind}`}>{kindLabel(event.kind)}</span><div className="event-main"><strong>{event.action}</strong><span>{[event.node, event.protocol, event.actor].filter(Boolean).join(' · ') || '系統'}</span></div><div className="event-route"><span>{event.source_ip || '無來源'}</span><strong>{destination || '無目的地'}</strong><span>{event.outbound_ip || '無出站位址'}</span></div><span className={`status-badge status-${event.success ? 'healthy' : 'unhealthy'}`}>{event.success ? '成功' : '失敗'}</span>{event.error && <span className="event-error">{event.error}</span>}</article>
}

function confirmationTitle(confirm: Exclude<ConfirmAction, null>) {
  if (confirm.kind === 'logs') return '清除全部日誌'
  return confirm.node ? `歸零 ${confirm.node} 統計` : '歸零全部統計'
}
function confirmationMessage(confirm: Exclude<ConfirmAction, null>) {
  if (confirm.kind === 'logs') return '將刪除目前日誌與所有輪替檔，操作本身會成為新日誌的第一筆稽核事件。'
  return confirm.node ? `將清除 ${confirm.node} 的累計連線、流量與錯誤；活躍連線數保持不變。` : '將清除所有節點的累計連線、流量與錯誤；活躍連線數保持不變。'
}
function kindLabel(kind: LogKind) { return ({ proxy: '代理', system: '系統', audit: '稽核' })[kind] }
function formatTime(value: string) { return new Date(value).toLocaleString('zh-TW') }
function formatBytes(value: number) {
  if (value < 1024) return `${value} B`
  if (value < 1024 * 1024) return `${(value / 1024).toFixed(1)} KiB`
  if (value < 1024 * 1024 * 1024) return `${(value / 1024 / 1024).toFixed(1)} MiB`
  return `${(value / 1024 / 1024 / 1024).toFixed(1)} GiB`
}

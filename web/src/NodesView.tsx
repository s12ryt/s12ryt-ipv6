import { FormEvent, ReactNode, useState } from 'react'
import { Copy, Eye, EyeOff, Pencil, Play, Plus, RotateCcw, Square, Trash2, X } from 'lucide-react'
import { APIError, ApiClient, InboundMode, NodeMutation, NodeProtocol, NodeRecord, ResourceSnapshot, ULAOverride } from './api'
import type { PanelMode } from './panelMode'

type NodeClient = Pick<ApiClient, 'mutate'>

interface NodesViewProps {
  mode: PanelMode
  client: NodeClient
  nodes: NodeRecord[]
  resources: ResourceSnapshot
  onChange: (nodes: NodeRecord[]) => void
}

interface NodeFormState {
  id: string
  name: string
  protocol: NodeProtocol
  authentication: 'credentials' | 'none'
  username: string
  password: string
  outbound: string
  port: string
  inboundMode: InboundMode
  inboundResource: string
  maxTCP: string
  maxUDP: string
  dialTimeout: string
  handshakeTimeout: string
  tunnelIdleTimeout: string
  udpIdleTimeout: string
  ulaOverride: ULAOverride
  confirmUnauthenticated: boolean
}

const emptyForm: NodeFormState = {
  id: '', name: '', protocol: 'mixed', authentication: 'credentials', username: '', password: '',
  outbound: '', port: '0', inboundMode: 'ipv6', inboundResource: '',
  maxTCP: '4096', maxUDP: '1024', dialTimeout: '10s', handshakeTimeout: '30s',
  tunnelIdleTimeout: '0s', udpIdleTimeout: '5m', ulaOverride: 'inherit', confirmUnauthenticated: false,
}

export function NodesView({ mode, client, nodes, resources, onChange }: NodesViewProps) {
  const [form, setForm] = useState<NodeFormState | null>(null)
  const [editingID, setEditingID] = useState('')
  const [visibleSecrets, setVisibleSecrets] = useState<Set<string>>(new Set())
  const [deleteID, setDeleteID] = useState('')
  const [busy, setBusy] = useState('')
  const [error, setError] = useState('')

  const replaceNode = (value: NodeRecord) => {
    const next = nodes.some((item) => item.id === value.id)
      ? nodes.map((item) => (item.id === value.id ? value : item))
      : [...nodes, value].sort((a, b) => a.id.localeCompare(b.id))
    onChange(next)
  }

  const action = async (node: NodeRecord, operation: 'start' | 'stop') => {
    setBusy(`${node.id}:${operation}`)
    setError('')
    try {
      replaceNode(await client.mutate<NodeRecord>(`/api/nodes/${encodeURIComponent(node.id)}/${operation}`, 'POST', {}))
    } catch (reason) {
      setError(messageFor(reason, '節點狀態變更失敗'))
    } finally {
      setBusy('')
    }
  }

  const remove = async (id: string) => {
    setBusy(`${id}:delete`)
    setError('')
    try {
      await client.mutate<void>(`/api/nodes/${encodeURIComponent(id)}`, 'DELETE', {})
      onChange(nodes.filter((item) => item.id !== id))
      setDeleteID('')
    } catch (reason) {
      setError(messageFor(reason, '節點刪除失敗'))
    } finally {
      setBusy('')
    }
  }

  const resetCredentials = async (node: NodeRecord) => {
    setBusy(`${node.id}:credentials`)
    setError('')
    try {
      const payload = mutationFromNode(node)
      payload.authentication = 'credentials'
      payload.username = ''
      payload.password = ''
      replaceNode(await client.mutate<NodeRecord>(`/api/nodes/${encodeURIComponent(node.id)}`, 'PUT', payload))
      setVisibleSecrets((current) => new Set(current).add(node.id))
    } catch (reason) {
      setError(messageFor(reason, '帳密重設失敗'))
    } finally {
      setBusy('')
    }
  }

  const submit = async (event: FormEvent<HTMLFormElement>) => {
    event.preventDefault()
    if (!form) return
    setBusy('form')
    setError('')
    try {
      const payload = mutationFromForm(form)
      const path = editingID ? `/api/nodes/${encodeURIComponent(editingID)}` : '/api/nodes'
      const result = await client.mutate<NodeRecord>(path, editingID ? 'PUT' : 'POST', payload)
      replaceNode(result)
      if (result.authentication !== 'none') setVisibleSecrets((current) => new Set(current).add(result.id))
      setForm(null)
      setEditingID('')
    } catch (reason) {
      setError(messageFor(reason, '節點設定未儲存'))
    } finally {
      setBusy('')
    }
  }

  const beginEdit = (node: NodeRecord) => {
    setEditingID(node.id)
    setForm({
      id: node.id, name: node.name, protocol: node.protocol,
      authentication: node.authentication ?? (node.username ? 'credentials' : 'none'),
      username: node.username ?? '', password: node.password ?? '', outbound: node.outbound,
      port: String(node.port), inboundMode: node.inbound_mode, inboundResource: node.inbound_resource ?? '',
      maxTCP: String(node.max_tcp), maxUDP: String(node.max_udp), dialTimeout: node.dial_timeout,
      handshakeTimeout: node.handshake_timeout, tunnelIdleTimeout: node.tunnel_idle_timeout,
      udpIdleTimeout: node.udp_idle_timeout, ulaOverride: node.ula_override, confirmUnauthenticated: false,
    })
  }

  return (
    <section aria-labelledby="page-title">
      <div className="page-heading">
        <div><p className="eyebrow">代理服務</p><h1 id="page-title">節點</h1></div>
        <button className="primary-button" type="button" onClick={() => { setEditingID(''); setForm(defaultForm(resources)) }}>
          <Plus size={17} aria-hidden="true" />新增節點
        </button>
      </div>
      {error && <div className="inline-error page-message" role="alert">{error}</div>}
      {form && <NodeEditor mode={mode} form={form} editing={Boolean(editingID)} busy={busy === 'form'} resources={resources} onChange={setForm} onSubmit={submit} onCancel={() => { setForm(null); setEditingID('') }} />}
      <div className="resource-table" role="table" aria-label="代理節點">
        <div className="resource-table-head" role="row"><span>節點</span><span>入站</span><span>出站</span><span>狀態</span><span>操作</span></div>
        {nodes.length === 0 ? <p className="empty-state">尚未建立節點</p> : nodes.map((node) => {
          const visible = visibleSecrets.has(node.id)
          const authenticated = (node.authentication ?? (node.username ? 'credentials' : 'none')) === 'credentials'
          return (
            <div className="resource-table-row" role="row" key={node.id}>
              <div role="cell"><strong>{node.name}</strong><span className="mono">{node.id}</span></div>
              <div role="cell"><span>{node.protocol.toUpperCase()} · {node.port}</span><span>{inboundLabel(node.inbound_mode)}{node.inbound_resource ? ` · ${node.inbound_resource}` : ''}</span></div>
              <div role="cell"><span className="mono">{node.outbound}</span><span>{authenticated ? '帳密認證' : '無認證'}</span></div>
              <div role="cell"><span className={`status-badge status-${node.status === 'running' ? 'healthy' : 'unhealthy'}`}>{node.status === 'running' ? '運行中' : '已停止'}</span></div>
              <div className="row-actions" role="cell">
                {node.status === 'running' ? (
                  <IconButton label={`停止 ${node.id}`} icon={Square} disabled={busy !== ''} onClick={() => void action(node, 'stop')} />
                ) : (
                  <IconButton label={`啟動 ${node.id}`} icon={Play} disabled={busy !== ''} onClick={() => void action(node, 'start')} />
                )}
                <IconButton label={`編輯 ${node.id}`} icon={Pencil} disabled={busy !== ''} onClick={() => beginEdit(node)} />
                {authenticated && <IconButton label={`${visible ? '隱藏' : '顯示'} ${node.id} 帳密`} icon={visible ? EyeOff : Eye} onClick={() => setVisibleSecrets(toggleSet(visibleSecrets, node.id))} />}
                {authenticated && <IconButton label={`複製 ${node.id} 連線帳密`} icon={Copy} onClick={() => void navigator.clipboard.writeText(`${node.username}:${node.password}`).catch(() => setError('無法存取剪貼簿'))} />}
                {authenticated && <IconButton label={`重設 ${node.id} 帳密`} icon={RotateCcw} disabled={busy !== ''} onClick={() => void resetCredentials(node)} />}
                <IconButton label={`刪除 ${node.id}`} icon={Trash2} tone="danger" disabled={busy !== ''} onClick={() => setDeleteID(node.id)} />
              </div>
              {visible && authenticated && <div className="secret-row"><span className="mono">{node.username}</span><span className="mono">{node.password}</span></div>}
              {deleteID === node.id && (
                <div className="confirm-row" role="alert"><span>刪除會立即中止所有連線。</span><button className="danger-button" type="button" onClick={() => void remove(node.id)}>確認刪除 {node.id}</button><button className="secondary-button" type="button" onClick={() => setDeleteID('')}>取消</button></div>
              )}
            </div>
          )
        })}
      </div>
    </section>
  )
}

function NodeEditor({ mode, form, editing, busy, resources, onChange, onSubmit, onCancel }: {
  mode: PanelMode
  form: NodeFormState
  editing: boolean
  busy: boolean
  resources: ResourceSnapshot
  onChange: (value: NodeFormState) => void
  onSubmit: (event: FormEvent<HTMLFormElement>) => void
  onCancel: () => void
}) {
  const set = <K extends keyof NodeFormState>(key: K, value: NodeFormState[K]) => onChange({ ...form, [key]: value })
  const inboundResources = [...resources.fixed.map((item) => item.name), ...resources.pools.filter((item) => item.kind === 'inbound').map((item) => item.name)]
  const outboundResources = [...resources.fixed.map((item) => item.name), ...resources.pools.filter((item) => item.kind !== 'inbound').map((item) => item.name)]
  return (
    <form className="editor-panel" onSubmit={onSubmit}>
      <div className="section-heading"><h2>{editing ? '編輯節點' : '新增節點'}</h2><IconButton label="關閉節點表單" icon={X} onClick={onCancel} /></div>
      <div className="form-grid">
        <Field label="節點 ID"><input value={form.id} onChange={(event) => set('id', event.target.value)} disabled={editing} required /></Field>
        <Field label="顯示名稱"><input value={form.name} onChange={(event) => set('name', event.target.value)} required /></Field>
        <Field label="協定"><select value={form.protocol} onChange={(event) => set('protocol', event.target.value as NodeProtocol)}><option value="mixed">SOCKS + HTTP</option><option value="socks">SOCKS5</option><option value="http">HTTP</option></select></Field>
        <Field label="代理認證"><select value={form.authentication} onChange={(event) => set('authentication', event.target.value as 'credentials' | 'none')}><option value="credentials">帳號密碼</option><option value="none">無認證</option></select></Field>
        {form.authentication === 'credentials' ? <>
          <Field label="代理帳號"><input value={form.username} onChange={(event) => set('username', event.target.value)} placeholder="留空自動生成" autoComplete="off" /></Field>
          <Field label="代理密碼"><input value={form.password} onChange={(event) => set('password', event.target.value)} placeholder="留空自動生成" autoComplete="new-password" /></Field>
        </> : <div className="risk-field" role="alert"><span>無認證可能使此節點成為公開代理。</span><label><input type="checkbox" checked={form.confirmUnauthenticated} onChange={(event) => set('confirmUnauthenticated', event.target.checked)} />我確認承擔公開代理風險</label></div>}
        <Field label="出站資源"><select value={form.outbound} onChange={(event) => set('outbound', event.target.value)} required><option value="">請選擇</option>{outboundResources.map((name) => <option key={name} value={name}>{name}</option>)}</select></Field>
        <Field label="代理埠"><input type="number" min="0" max="65535" value={form.port} onChange={(event) => set('port', event.target.value)} required /></Field>
        <Field label="入站位址族"><select value={form.inboundMode} onChange={(event) => set('inboundMode', event.target.value as InboundMode)}><option value="ipv6">僅 IPv6</option><option value="ipv4">僅 IPv4</option><option value="dual">雙棧</option></select></Field>
        {form.inboundMode !== 'ipv4' && <Field label="IPv6 入站資源"><select value={form.inboundResource} onChange={(event) => set('inboundResource', event.target.value)} required><option value="">請選擇</option>{inboundResources.map((name) => <option key={name} value={name}>{name}</option>)}</select></Field>}
        {mode === 'advanced' && <>
          <Field label="TCP 上限"><input type="number" min="1" value={form.maxTCP} onChange={(event) => set('maxTCP', event.target.value)} required /></Field>
          <Field label="UDP association 上限"><input type="number" min="1" value={form.maxUDP} onChange={(event) => set('maxUDP', event.target.value)} required /></Field>
          <Field label="Dial timeout"><input value={form.dialTimeout} onChange={(event) => set('dialTimeout', event.target.value)} required /></Field>
          <Field label="握手 timeout"><input value={form.handshakeTimeout} onChange={(event) => set('handshakeTimeout', event.target.value)} required /></Field>
          <Field label="Tunnel idle timeout"><input value={form.tunnelIdleTimeout} onChange={(event) => set('tunnelIdleTimeout', event.target.value)} required /></Field>
          <Field label="UDP idle timeout"><input value={form.udpIdleTimeout} onChange={(event) => set('udpIdleTimeout', event.target.value)} required /></Field>
          <Field label="ULA 政策"><select value={form.ulaOverride} onChange={(event) => set('ulaOverride', event.target.value as ULAOverride)}><option value="inherit">沿用全域</option><option value="allow">允許</option><option value="deny">拒絕</option></select></Field>
        </>}
      </div>
      <div className="form-actions"><button className="primary-button" type="submit" disabled={busy || (form.authentication === 'none' && !form.confirmUnauthenticated)}>{editing ? '儲存並切換' : '建立並啟動'}</button><button className="secondary-button" type="button" onClick={onCancel}>取消</button></div>
    </form>
  )
}

function Field({ label, children }: { label: string; children: ReactNode }) {
  return <label className="field"><span>{label}</span>{children}</label>
}

function IconButton({ label, icon: Icon, onClick, disabled, tone }: { label: string; icon: typeof Eye; onClick: () => void; disabled?: boolean; tone?: 'danger' }) {
  return <button className={`icon-button${tone === 'danger' ? ' danger-text' : ''}`} type="button" title={label} aria-label={label} disabled={disabled} onClick={onClick}><Icon size={16} aria-hidden="true" /></button>
}

function toggleSet(current: Set<string>, value: string) {
  const next = new Set(current)
  if (next.has(value)) next.delete(value)
  else next.add(value)
  return next
}

function mutationFromForm(form: NodeFormState): NodeMutation {
  return {
    id: form.id.trim(), name: form.name.trim(), protocol: form.protocol,
    authentication: form.authentication,
    username: form.authentication === 'credentials' ? form.username : '',
    password: form.authentication === 'credentials' ? form.password : '',
    max_tcp: Number(form.maxTCP), max_udp: Number(form.maxUDP),
    dial_timeout: form.dialTimeout, handshake_timeout: form.handshakeTimeout,
    tunnel_idle_timeout: form.tunnelIdleTimeout, udp_idle_timeout: form.udpIdleTimeout,
    ula_override: form.ulaOverride, outbound: form.outbound.trim(), port: Number(form.port),
    inbound_mode: form.inboundMode,
    inbound_resource: form.inboundMode === 'ipv4' ? '' : form.inboundResource,
    confirm_unauthenticated: form.authentication === 'none' && form.confirmUnauthenticated,
  }
}

function defaultForm(resources: ResourceSnapshot): NodeFormState {
  const inboundResource = resources.fixed[0]?.name ?? resources.pools.find((item) => item.kind === 'inbound')?.name ?? ''
  const outbound = resources.fixed[0]?.name ?? resources.pools.find((item) => item.kind !== 'inbound')?.name ?? ''
  return { ...emptyForm, inboundResource, outbound }
}

function inboundLabel(mode: InboundMode) {
  return ({ ipv4: '僅 IPv4', ipv6: '僅 IPv6', dual: '雙棧' })[mode]
}

function mutationFromNode(node: NodeRecord): NodeMutation {
  return {
    ...node,
    authentication: node.authentication ?? (node.username ? 'credentials' : 'none'),
    confirm_unauthenticated: false,
  }
}

function messageFor(reason: unknown, fallback: string) {
  return reason instanceof APIError ? reason.message : fallback
}

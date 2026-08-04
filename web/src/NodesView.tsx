import { FormEvent, ReactNode, useEffect, useRef, useState } from 'react'
import { ChevronDown, ChevronRight, Copy, Eye, EyeOff, FolderPlus, Layers3, Link2, Pencil, Play, Plus, RotateCcw, Square, Trash2, X } from 'lucide-react'
import { APIError, ApiClient, InboundMode, NodeMutation, NodeProtocol, NodeRecord, ResourceSnapshot, ULAOverride } from './api'
import { nextBatchFolderName, nextNodeIdentities, nextNodeIdentity } from './automaticNames'
import { copyText } from './clipboard'
import { buildNodeConnectionInfo } from './nodeConnection'
import { groupNodesByFolder, persistCollapsedFolders, storedCollapsedFolders } from './nodeFolders'
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
  folder: string
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

interface BatchPreviewRow {
  id: string
  name: string
  port: string
}

interface BatchFormState {
  folder: string
  count: string
  settings: NodeFormState
  preview: BatchPreviewRow[]
}

interface FolderActionResponse {
  succeeded: string[]
  failed: Array<{ id: string; error: string }>
}

interface ManualCopyState {
  title: string
  value: string
}

interface CopyFeedback {
  target: string
  kind: 'connection' | 'credentials'
}

const emptyForm: NodeFormState = {
  id: '', name: '', folder: '', protocol: 'mixed', authentication: 'credentials', username: '', password: '',
  outbound: '', port: '0', inboundMode: 'ipv6', inboundResource: '',
  maxTCP: '4096', maxUDP: '1024', dialTimeout: '10s', handshakeTimeout: '30s',
  tunnelIdleTimeout: '0s', udpIdleTimeout: '5m', ulaOverride: 'inherit', confirmUnauthenticated: false,
}

export function NodesView({ mode, client, nodes, resources, onChange }: NodesViewProps) {
  const [form, setForm] = useState<NodeFormState | null>(null)
  const [batch, setBatch] = useState<BatchFormState | null>(null)
  const [editingID, setEditingID] = useState('')
  const [visibleSecrets, setVisibleSecrets] = useState<Set<string>>(new Set())
  const [deleteID, setDeleteID] = useState('')
  const [busy, setBusy] = useState('')
  const [error, setError] = useState('')
  const [manualCopy, setManualCopy] = useState<ManualCopyState | null>(null)
  const [copyFeedback, setCopyFeedback] = useState<CopyFeedback | null>(null)
  const [collapsedFolders, setCollapsedFolders] = useState<Set<string>>(storedCollapsedFolders)
  const [renamingFolder, setRenamingFolder] = useState('')
  const [renameTarget, setRenameTarget] = useState('')
  const [deleteFolder, setDeleteFolder] = useState('')
  const copyTimer = useRef<ReturnType<typeof setTimeout> | null>(null)
  const manualCopyInput = useRef<HTMLTextAreaElement | null>(null)
  const groups = groupNodesByFolder(nodes)
  const folderNames = groups.filter((group) => group.name).map((group) => group.name)

  useEffect(() => () => {
    if (copyTimer.current) clearTimeout(copyTimer.current)
  }, [])

  useEffect(() => {
    if (!manualCopy || !manualCopyInput.current) return
    manualCopyInput.current.focus()
    manualCopyInput.current.select()
  }, [manualCopy])

  const replaceNode = (value: NodeRecord) => {
    const next = nodes.some((item) => item.id === value.id)
      ? nodes.map((item) => (item.id === value.id ? value : item))
      : [...nodes, value].sort((a, b) => a.id.localeCompare(b.id))
    onChange(next)
  }

  const mergeNodes = (values: NodeRecord[]) => {
    const replacements = new Map(values.map((node) => [node.id, node]))
    const next = nodes.map((node) => replacements.get(node.id) ?? node)
    for (const value of values) {
      if (!nodes.some((node) => node.id === value.id)) next.push(value)
    }
    onChange(next.sort((left, right) => left.id.localeCompare(right.id)))
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

  const copyNodeValue = async (node: NodeRecord, kind: CopyFeedback['kind']) => {
    setError('')
    let value: string
    try {
      value = kind === 'connection'
        ? buildNodeConnectionInfo(node, resources, window.location.hostname)
        : credentialText(node)
    } catch (reason) {
      setError(reason instanceof Error ? reason.message : '無法產生可複製的節點資訊')
      return
    }

    try {
      await copyText(value)
      setManualCopy(null)
      setCopyFeedback({ target: node.id, kind })
      if (copyTimer.current) clearTimeout(copyTimer.current)
      copyTimer.current = setTimeout(() => setCopyFeedback((current) => (
        current?.target === node.id && current.kind === kind ? null : current
      )), 2000)
    } catch {
      setCopyFeedback(null)
      setManualCopy({
        title: kind === 'connection' ? '手動複製連線資訊' : '手動複製連線帳密',
        value,
      })
    }
  }

  const copyFolderValue = async (folder: string, members: NodeRecord[], kind: CopyFeedback['kind']) => {
    setError('')
    let value: string
    try {
      value = kind === 'connection'
        ? members.flatMap((node) => buildNodeConnectionInfo(node, resources, window.location.hostname).split('\n')).join('\n')
        : members.filter(hasCredentials).map((node) => `${node.id}\t${credentialText(node)}`).join('\n')
      if (!value) throw new Error('資料夾內沒有可複製的節點帳密')
    } catch (reason) {
      setError(reason instanceof Error ? reason.message : '無法產生可複製的資料夾資訊')
      return
    }
    try {
      await copyText(value)
      setManualCopy(null)
      setCopyFeedback({ target: folder, kind })
      if (copyTimer.current) clearTimeout(copyTimer.current)
      copyTimer.current = setTimeout(() => setCopyFeedback((current) => (
        current?.target === folder && current.kind === kind ? null : current
      )), 2000)
    } catch {
      setCopyFeedback(null)
      setManualCopy({ title: kind === 'connection' ? '手動複製資料夾連線資訊' : '手動複製資料夾帳密', value })
    }
  }

  const toggleFolder = (folder: string) => {
    const next = toggleSet(collapsedFolders, folder)
    setCollapsedFolders(next)
    persistCollapsedFolders(next)
  }

  const rename = async () => {
    if (!renamingFolder) return
    setBusy(`folder:${renamingFolder}:rename`)
    setError('')
    try {
      const renamed = await client.mutate<NodeRecord[]>('/api/node-folders/rename', 'PUT', { source: renamingFolder, target: renameTarget })
      mergeNodes(renamed)
      setRenamingFolder('')
      setRenameTarget('')
    } catch (reason) {
      setError(messageFor(reason, '資料夾重新命名失敗'))
    } finally {
      setBusy('')
    }
  }

  const moveNode = async (node: NodeRecord, folder: string) => {
    if ((node.folder ?? '') === folder) return
    setBusy(`${node.id}:folder`)
    setError('')
    try {
      replaceNode(await client.mutate<NodeRecord>(`/api/nodes/${encodeURIComponent(node.id)}/folder`, 'PUT', { folder }))
    } catch (reason) {
      setError(messageFor(reason, '節點移動失敗'))
    } finally {
      setBusy('')
    }
  }

  const folderAction = async (folder: string, operation: 'start' | 'stop' | 'delete', confirm = false) => {
    setBusy(`folder:${folder}:${operation}`)
    setError('')
    try {
      const result = await client.mutate<FolderActionResponse>('/api/node-folders/action', 'POST', {
        folder,
        action: operation,
        ...(operation === 'delete' ? { confirm } : {}),
      })
      const succeeded = new Set(result.succeeded)
      if (operation === 'delete') onChange(nodes.filter((node) => !succeeded.has(node.id)))
      else onChange(nodes.map((node) => succeeded.has(node.id) ? { ...node, status: operation === 'start' ? 'running' : 'stopped' } : node))
      if (result.failed.length) setError(result.failed.map((failure) => `${failure.id}：${failure.error}`).join('；'))
      if (operation === 'delete') setDeleteFolder('')
    } catch (reason) {
      setError(messageFor(reason, '資料夾批量操作失敗'))
    } finally {
      setBusy('')
    }
  }

  const submitBatch = async (event: FormEvent<HTMLFormElement>) => {
    event.preventDefault()
    if (!batch || batch.preview.length === 0) return
    setBusy('batch')
    setError('')
    try {
      const common = mutationFromForm(batch.settings)
      const batchSettings = withoutFolder(common)
      const created = await client.mutate<NodeRecord[]>('/api/nodes/batch', 'POST', {
        folder: batch.folder.trim(),
        confirm_unauthenticated: common.authentication === 'none' && common.confirm_unauthenticated,
        nodes: batch.preview.map((row) => ({
          ...batchSettings,
          id: row.id.trim(),
          name: row.name.trim(),
          port: Number(row.port),
          username: '',
          password: '',
          confirm_unauthenticated: false,
        })),
      })
      mergeNodes(created)
      setVisibleSecrets((current) => new Set([...current, ...created.filter(hasCredentials).map((node) => node.id)]))
      setBatch(null)
    } catch (reason) {
      setError(messageFor(reason, '批次節點未建立'))
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
      id: node.id, name: node.name, folder: node.folder ?? '', protocol: node.protocol,
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
        <div className="toolbar-actions">
          <button className="secondary-button" type="button" onClick={() => { setForm(null); setEditingID(''); setBatch(defaultBatch(resources, nodes)) }}>
            <Layers3 size={17} aria-hidden="true" />一鍵建立多節點
          </button>
          <button className="primary-button" type="button" onClick={() => { setBatch(null); setEditingID(''); setForm(defaultForm(resources, nodes)) }}>
            <Plus size={17} aria-hidden="true" />新增節點
          </button>
        </div>
      </div>
      {error && <div className="inline-error page-message" role="alert">{error}</div>}
      {form && <NodeEditor mode={mode} form={form} editing={Boolean(editingID)} busy={busy === 'form'} resources={resources} folders={folderNames} onChange={setForm} onSubmit={submit} onCancel={() => { setForm(null); setEditingID('') }} />}
      {batch && <BatchNodeEditor mode={mode} value={batch} busy={busy === 'batch'} resources={resources} existingNodes={nodes} onChange={setBatch} onSubmit={submitBatch} onCancel={() => setBatch(null)} />}
      {renamingFolder && (
        <div className="compact-confirm folder-rename" role="group" aria-label={`重新命名 ${renamingFolder}`}>
          <Field label="資料夾新名稱"><input value={renameTarget} maxLength={64} onChange={(event) => setRenameTarget(event.target.value)} required /></Field>
          <button className="primary-button" type="button" disabled={busy !== '' || !renameTarget.trim()} onClick={() => void rename()}>確認重新命名</button>
          <button className="secondary-button" type="button" onClick={() => { setRenamingFolder(''); setRenameTarget('') }}>取消</button>
        </div>
      )}
      <div className="node-folder-list" aria-label="代理節點資料夾">
        {nodes.length === 0 ? <p className="empty-state">尚未建立節點</p> : groups.map((group) => {
          const collapsed = group.name ? collapsedFolders.has(group.name) : false
          return (
            <section className="node-folder" role="region" aria-label={`資料夾 ${group.label}`} key={group.name || '__unclassified'}>
              <div className="node-folder-heading">
                <div className="node-folder-title">
                  {group.name ? <IconButton label={`${collapsed ? '展開' : '收合'} ${group.name}`} icon={collapsed ? ChevronRight : ChevronDown} onClick={() => toggleFolder(group.name)} /> : <FolderPlus size={17} aria-hidden="true" />}
                  <h2>{group.label}</h2><span className="count-badge">{group.nodes.length}</span>
                </div>
                {group.name && <div className="row-actions">
                  <IconButton label={`複製 ${group.name} 全部連線資訊`} icon={Link2} onClick={() => void copyFolderValue(group.name, group.nodes, 'connection')} />
                  {group.nodes.some(hasCredentials) && <IconButton label={`複製 ${group.name} 全部帳密`} icon={Copy} onClick={() => void copyFolderValue(group.name, group.nodes, 'credentials')} />}
                  <IconButton label={`全部啟動 ${group.name}`} icon={Play} disabled={busy !== ''} onClick={() => void folderAction(group.name, 'start')} />
                  <IconButton label={`全部停止 ${group.name}`} icon={Square} disabled={busy !== ''} onClick={() => void folderAction(group.name, 'stop')} />
                  <IconButton label={`重新命名 ${group.name}`} icon={Pencil} disabled={busy !== ''} onClick={() => { setRenamingFolder(group.name); setRenameTarget(group.name) }} />
                  <IconButton label={`刪除資料夾 ${group.name}`} icon={Trash2} tone="danger" disabled={busy !== ''} onClick={() => setDeleteFolder(group.name)} />
                  {copyFeedback?.target === group.name && <span className="copy-feedback" role="status">已複製</span>}
                </div>}
              </div>
              {group.name && deleteFolder === group.name && <div className="confirm-row" role="alert"><span>將逐一刪除資料夾內全部節點，成功項目不會因其他節點失敗而還原。</span><button className="danger-button" type="button" onClick={() => void folderAction(group.name, 'delete', true)}>確認刪除資料夾</button><button className="secondary-button" type="button" onClick={() => setDeleteFolder('')}>取消</button></div>}
              {!collapsed && <div className="resource-table" role="table" aria-label={`${group.label} 節點`}>
                <div className="resource-table-head" role="row"><span>節點</span><span>入站</span><span>出站</span><span>狀態</span><span>操作</span></div>
                {group.nodes.map((node) => {
                  const visible = visibleSecrets.has(node.id)
                  const authenticated = hasCredentials(node)
                  return <div className="resource-table-row" role="row" key={node.id}>
                    <div role="cell"><strong>{node.name}</strong><span className="mono">{node.id}</span></div>
                    <div role="cell"><span>{node.protocol.toUpperCase()} · {node.port}</span><span>{inboundLabel(node.inbound_mode)}{node.inbound_resource ? ` · ${node.inbound_resource}` : ''}</span></div>
                    <div role="cell"><span className="mono">{node.outbound}</span><span>{authenticated ? '帳密認證' : '無認證'}</span></div>
                    <div role="cell"><span className={`status-badge status-${node.status === 'running' ? 'healthy' : 'unhealthy'}`}>{node.status === 'running' ? '運行中' : '已停止'}</span></div>
                    <div className="row-actions" role="cell">
                      {node.status === 'running' ? <IconButton label={`停止 ${node.id}`} icon={Square} disabled={busy !== ''} onClick={() => void action(node, 'stop')} /> : <IconButton label={`啟動 ${node.id}`} icon={Play} disabled={busy !== ''} onClick={() => void action(node, 'start')} />}
                      <IconButton label={`編輯 ${node.id}`} icon={Pencil} disabled={busy !== ''} onClick={() => beginEdit(node)} />
                      <IconButton label={`複製 ${node.id} 連線資訊`} icon={Link2} onClick={() => void copyNodeValue(node, 'connection')} />
                      {authenticated && <IconButton label={`${visible ? '隱藏' : '顯示'} ${node.id} 帳密`} icon={visible ? EyeOff : Eye} onClick={() => setVisibleSecrets(toggleSet(visibleSecrets, node.id))} />}
                      {authenticated && <IconButton label={`複製 ${node.id} 連線帳密`} icon={Copy} onClick={() => void copyNodeValue(node, 'credentials')} />}
                      {authenticated && <IconButton label={`重設 ${node.id} 帳密`} icon={RotateCcw} disabled={busy !== ''} onClick={() => void resetCredentials(node)} />}
                      <IconButton label={`刪除 ${node.id}`} icon={Trash2} tone="danger" disabled={busy !== ''} onClick={() => setDeleteID(node.id)} />
                      {copyFeedback?.target === node.id && <span className="copy-feedback" role="status">已複製</span>}
                    </div>
                    <div className="node-folder-move"><label><span>資料夾</span><select aria-label={`移動 ${node.id} 至資料夾`} value={node.folder ?? ''} disabled={busy !== ''} onChange={(event) => void moveNode(node, event.target.value)}><option value="">未分類</option>{folderNames.map((folder) => <option value={folder} key={folder}>{folder}</option>)}</select></label></div>
                    {visible && authenticated && <div className="secret-row"><span className="mono">{node.username}</span><span className="mono">{node.password}</span></div>}
                    {deleteID === node.id && <div className="confirm-row" role="alert"><span>刪除會立即中止所有連線。</span><button className="danger-button" type="button" onClick={() => void remove(node.id)}>確認刪除 {node.id}</button><button className="secondary-button" type="button" onClick={() => setDeleteID('')}>取消</button></div>}
                  </div>
                })}
              </div>}
            </section>
          )
        })}
      </div>
      {manualCopy && (
        <div className="copy-dialog-backdrop">
          <section className="copy-dialog" role="dialog" aria-modal="true" aria-labelledby="manual-copy-title">
            <div className="section-heading">
              <h2 id="manual-copy-title">{manualCopy.title}</h2>
              <IconButton label="關閉手動複製" icon={X} onClick={() => setManualCopy(null)} />
            </div>
            <p>瀏覽器禁止自動存取剪貼簿，請手動複製以下內容。</p>
            <label className="field">
              <span>手動複製內容</span>
              <textarea ref={manualCopyInput} value={manualCopy.value} readOnly rows={Math.min(8, manualCopy.value.split('\n').length + 1)} onFocus={(event) => event.currentTarget.select()} />
            </label>
            <div className="form-actions">
              <button className="secondary-button" type="button" onClick={() => { manualCopyInput.current?.focus(); manualCopyInput.current?.select() }}>選取全部</button>
              <button className="primary-button" type="button" onClick={() => setManualCopy(null)}>完成</button>
            </div>
          </section>
        </div>
      )}
    </section>
  )
}

function BatchNodeEditor({ mode, value, busy, resources, existingNodes, onChange, onSubmit, onCancel }: {
  mode: PanelMode
  value: BatchFormState
  busy: boolean
  resources: ResourceSnapshot
  existingNodes: NodeRecord[]
  onChange: (value: BatchFormState) => void
  onSubmit: (event: FormEvent<HTMLFormElement>) => void
  onCancel: () => void
}) {
  const setSetting = <K extends keyof NodeFormState>(key: K, setting: NodeFormState[K]) => onChange({ ...value, settings: { ...value.settings, [key]: setting } })
  const inboundResources = [...resources.fixed.map((item) => item.name), ...resources.pools.filter((item) => item.kind === 'inbound').map((item) => item.name)]
  const outboundResources = [...resources.fixed.map((item) => item.name), ...resources.pools.filter((item) => item.kind !== 'inbound').map((item) => item.name)]
  const regenerate = () => {
    const count = Math.min(100, Math.max(1, Number.parseInt(value.count, 10) || 1))
    const preview = nextNodeIdentities(existingNodes, count).map((identity) => ({ ...identity, port: '0' }))
    onChange({ ...value, count: String(count), preview })
  }
  const updatePreview = (index: number, key: keyof BatchPreviewRow, fieldValue: string) => {
    onChange({ ...value, preview: value.preview.map((row, rowIndex) => rowIndex === index ? { ...row, [key]: fieldValue } : row) })
  }
  return <form className="editor-panel batch-editor" onSubmit={onSubmit}>
    <div className="section-heading"><div><p className="eyebrow">交易式建立</p><h2>一鍵建立多節點</h2></div><IconButton label="關閉批次節點表單" icon={X} onClick={onCancel} /></div>
    <div className="form-grid">
      <Field label="資料夾名稱"><input value={value.folder} maxLength={64} onChange={(event) => onChange({ ...value, folder: event.target.value })} required /></Field>
      <Field label="節點數量"><input type="number" min="1" max="100" value={value.count} onChange={(event) => onChange({ ...value, count: event.target.value })} required /></Field>
      <Field label="批次協定"><select value={value.settings.protocol} onChange={(event) => setSetting('protocol', event.target.value as NodeProtocol)}><option value="mixed">SOCKS + HTTP</option><option value="socks">SOCKS5</option><option value="http">HTTP</option></select></Field>
      <Field label="批次代理認證"><select value={value.settings.authentication} onChange={(event) => setSetting('authentication', event.target.value as 'credentials' | 'none')}><option value="credentials">每節點自動產生不同帳密</option><option value="none">無認證</option></select></Field>
      {value.settings.authentication === 'none' && <div className="risk-field" role="alert"><span>整批節點將不使用認證，可能成為公開代理。</span><label><input type="checkbox" checked={value.settings.confirmUnauthenticated} onChange={(event) => setSetting('confirmUnauthenticated', event.target.checked)} />我確認整批公開代理風險</label></div>}
      <Field label="批次出站資源"><select value={value.settings.outbound} onChange={(event) => setSetting('outbound', event.target.value)} required><option value="">請選擇</option>{outboundResources.map((name) => <option key={name} value={name}>{name}</option>)}</select></Field>
      <Field label="批次入站位址族"><select value={value.settings.inboundMode} onChange={(event) => setSetting('inboundMode', event.target.value as InboundMode)}><option value="ipv6">僅 IPv6</option><option value="ipv4">僅 IPv4</option><option value="dual">雙棧</option></select></Field>
      {value.settings.inboundMode !== 'ipv4' && <Field label="批次 IPv6 入站資源"><select value={value.settings.inboundResource} onChange={(event) => setSetting('inboundResource', event.target.value)} required><option value="">請選擇</option>{inboundResources.map((name) => <option key={name} value={name}>{name}</option>)}</select></Field>}
      {mode === 'advanced' && <>
        <Field label="批次 TCP 上限"><input type="number" min="1" value={value.settings.maxTCP} onChange={(event) => setSetting('maxTCP', event.target.value)} required /></Field>
        <Field label="批次 UDP association 上限"><input type="number" min="1" value={value.settings.maxUDP} onChange={(event) => setSetting('maxUDP', event.target.value)} required /></Field>
        <Field label="批次 Dial timeout"><input value={value.settings.dialTimeout} onChange={(event) => setSetting('dialTimeout', event.target.value)} required /></Field>
        <Field label="批次握手 timeout"><input value={value.settings.handshakeTimeout} onChange={(event) => setSetting('handshakeTimeout', event.target.value)} required /></Field>
        <Field label="批次 Tunnel idle timeout"><input value={value.settings.tunnelIdleTimeout} onChange={(event) => setSetting('tunnelIdleTimeout', event.target.value)} required /></Field>
        <Field label="批次 UDP idle timeout"><input value={value.settings.udpIdleTimeout} onChange={(event) => setSetting('udpIdleTimeout', event.target.value)} required /></Field>
        <Field label="批次 ULA 政策"><select value={value.settings.ulaOverride} onChange={(event) => setSetting('ulaOverride', event.target.value as ULAOverride)}><option value="inherit">沿用全域</option><option value="allow">允許</option><option value="deny">拒絕</option></select></Field>
      </>}
    </div>
    <div className="form-actions"><button className="secondary-button" type="button" onClick={regenerate}>重新產生預覽</button></div>
    <div className="batch-preview" role="table" aria-label="批次節點預覽">
      <div className="batch-preview-head" role="row"><span>節點 ID</span><span>顯示名稱</span><span>代理埠</span></div>
      {value.preview.map((row, index) => <div className="batch-preview-row" role="row" key={index}>
        <label><span className="sr-only">預覽 {index + 1} 節點 ID</span><input aria-label={`預覽 ${index + 1} 節點 ID`} value={row.id} onChange={(event) => updatePreview(index, 'id', event.target.value)} required /></label>
        <label><span className="sr-only">預覽 {index + 1} 顯示名稱</span><input aria-label={`預覽 ${index + 1} 顯示名稱`} value={row.name} onChange={(event) => updatePreview(index, 'name', event.target.value)} required /></label>
        <label><span className="sr-only">預覽 {index + 1} 代理埠</span><input aria-label={`預覽 ${index + 1} 代理埠`} type="number" min="0" max="65535" value={row.port} onChange={(event) => updatePreview(index, 'port', event.target.value)} required /></label>
      </div>)}
    </div>
    <div className="form-actions"><button className="primary-button" type="submit" disabled={busy || !value.folder.trim() || value.preview.length === 0 || (value.settings.authentication === 'none' && !value.settings.confirmUnauthenticated)}>建立 {value.preview.length} 個節點</button><button className="secondary-button" type="button" onClick={onCancel}>取消</button></div>
  </form>
}

function NodeEditor({ mode, form, editing, busy, resources, folders, onChange, onSubmit, onCancel }: {
  mode: PanelMode
  form: NodeFormState
  editing: boolean
  busy: boolean
  resources: ResourceSnapshot
  folders: string[]
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
        <Field label="資料夾"><select value={form.folder} onChange={(event) => set('folder', event.target.value)}><option value="">未分類</option>{folders.map((folder) => <option value={folder} key={folder}>{folder}</option>)}</select></Field>
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
    id: form.id.trim(), name: form.name.trim(), folder: form.folder.trim(), protocol: form.protocol,
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

function defaultForm(resources: ResourceSnapshot, nodes: NodeRecord[]): NodeFormState {
  const inboundResource = resources.fixed[0]?.name ?? resources.pools.find((item) => item.kind === 'inbound')?.name ?? ''
  const outbound = resources.fixed[0]?.name ?? resources.pools.find((item) => item.kind !== 'inbound')?.name ?? ''
  const identity = nextNodeIdentity(nodes)
  return { ...emptyForm, ...identity, inboundResource, outbound }
}

function defaultBatch(resources: ResourceSnapshot, nodes: NodeRecord[]): BatchFormState {
  const settings = defaultForm(resources, nodes)
  const preview = nextNodeIdentities(nodes, 5).map((identity) => ({ ...identity, port: '0' }))
  return {
    folder: nextBatchFolderName(nodes.map((node) => node.folder ?? '').filter(Boolean)),
    count: '5',
    settings: { ...settings, id: '', name: '', folder: '', username: '', password: '' },
    preview,
  }
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

function withoutFolder(value: NodeMutation): Omit<NodeMutation, 'folder'> {
  const result = { ...value }
  delete result.folder
  return result
}

function hasCredentials(node: NodeRecord): boolean {
  return (node.authentication ?? (node.username ? 'credentials' : 'none')) === 'credentials'
}

function credentialText(node: NodeRecord): string {
  if (!node.username || !node.password) throw new Error('節點帳密尚未完整產生')
  return `${node.username}:${node.password}`
}

function messageFor(reason: unknown, fallback: string) {
  return reason instanceof APIError ? reason.message : fallback
}

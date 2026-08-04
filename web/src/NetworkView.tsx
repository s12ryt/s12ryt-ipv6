import { useEffect, useState } from 'react'
import { KeyRound, Play, Plus, Save, Trash2 } from 'lucide-react'
import { APIError, ApiClient, ConnectivityCheck, Overview, ResolverConfig } from './api'
import type { PanelMode } from './panelMode'
import { CheckboxField } from './CheckboxField'
import { ModalDialog } from './ModalDialog'

type NetworkClient = Pick<ApiClient, 'mutate'>
type NAT64Mode = 'automatic' | 'custom'

type ResolverPreset = ResolverConfig & {
  id: string
  label: string
}

const resolverPresets: ResolverPreset[] = [
  { id: 'cloudflare-primary', label: 'Cloudflare DNS64 主要', name: 'cloudflare-primary', address: '2606:4700:4700::64', port: 853, server_name: 'cloudflare-dns.com', enabled: true },
  { id: 'cloudflare-secondary', label: 'Cloudflare DNS64 次要', name: 'cloudflare-secondary', address: '2606:4700:4700::6400', port: 853, server_name: 'cloudflare-dns.com', enabled: true },
  { id: 'google-primary', label: 'Google DNS64 主要', name: 'google-primary', address: '2001:4860:4860::6464', port: 853, server_name: 'dns.google', enabled: true },
  { id: 'google-secondary', label: 'Google DNS64 次要', name: 'google-secondary', address: '2001:4860:4860::64', port: 853, server_name: 'dns.google', enabled: true },
]

export function NetworkView({ mode, client, overview, onChange, onPasswordChanged }: {
  mode: PanelMode
  client: NetworkClient
  overview: Overview
  onChange: (overview: Overview) => void
  onPasswordChanged: () => void
}) {
  const [prefix, setPrefix] = useState(overview.nat64.prefix ?? '')
  const [nat64Mode, setNAT64Mode] = useState<NAT64Mode>(overview.nat64.manual ? 'custom' : 'automatic')
  const [resolvers, setResolvers] = useState<ResolverConfig[]>(() => cloneResolvers(overview.resolvers))
  const [resolverPreset, setResolverPreset] = useState('')
  const [checks, setChecks] = useState<ConnectivityCheck[] | null>(null)
  const [busy, setBusy] = useState('')
  const [error, setError] = useState('')
  const [dialog, setDialog] = useState<'nat64' | 'resolvers' | 'password' | null>(null)
  const [passwordDirty, setPasswordDirty] = useState(false)
  const [currentPassword, setCurrentPassword] = useState('')
  const [replacementPassword, setReplacementPassword] = useState('')
  const [passwordConfirmation, setPasswordConfirmation] = useState('')

  useEffect(() => setResolvers(cloneResolvers(overview.resolvers)), [overview.resolvers])
  useEffect(() => setPrefix(overview.nat64.prefix ?? ''), [overview.nat64.prefix])
  useEffect(() => setNAT64Mode(overview.nat64.manual ? 'custom' : 'automatic'), [overview.nat64.manual])

  const run = async (id: string, operation: () => Promise<void>, fallback: string) => {
    setBusy(id)
    setError('')
    try {
      await operation()
    } catch (reason) {
      setError(reason instanceof APIError ? reason.message : fallback)
    } finally {
      setBusy('')
    }
  }

  const updateNAT64 = (nextPrefix: string) => run('nat64', async () => {
    const nat64 = await client.mutate<Overview['nat64']>('/api/network/nat64', 'PUT', { prefix: nextPrefix.trim() })
    setPrefix(nat64.prefix ?? '')
    setNAT64Mode(nat64.manual ? 'custom' : 'automatic')
    onChange({ ...overview, nat64 })
    setDialog(null)
  }, 'NAT64 設定更新失敗')

  const saveResolvers = () => run('resolvers', async () => {
    await client.mutate<void>('/api/network/resolvers', 'PUT', { resolvers })
    onChange({ ...overview, resolvers: cloneResolvers(resolvers) })
    setDialog(null)
  }, 'Resolver 設定更新失敗')

  const testConnectivity = () => run('connectivity', async () => {
    setChecks(await client.mutate<ConnectivityCheck[]>('/api/network/test', 'POST', {}))
  }, '連通性測試失敗')

  const updateResolver = (index: number, patch: Partial<ResolverConfig>) => {
    setResolvers((current) => current.map((resolver, itemIndex) => itemIndex === index ? { ...resolver, ...patch } : resolver))
  }

  const addResolverPreset = () => {
    const preset = resolverPresets.find((item) => item.id === resolverPreset)
    if (!preset || resolvers.some((resolver) => resolverMatchesPreset(resolver, preset))) return
    setResolvers((current) => [...current, {
      name: preset.name,
      address: preset.address,
      port: preset.port,
      server_name: preset.server_name,
      enabled: preset.enabled,
    }])
    setResolverPreset('')
  }

  const selectedPreset = resolverPresets.find((preset) => preset.id === resolverPreset)
  const selectedPresetAdded = selectedPreset ? resolvers.some((resolver) => resolverMatchesPreset(resolver, selectedPreset)) : false
  const nat64Dirty = nat64Mode !== (overview.nat64.manual ? 'custom' : 'automatic') || prefix !== (overview.nat64.prefix ?? '')
  const resolversDirty = JSON.stringify(resolvers) !== JSON.stringify(overview.resolvers)

  const openResolvers = () => {
    setResolvers(cloneResolvers(overview.resolvers))
    setResolverPreset('')
    setDialog('resolvers')
  }
  const openPassword = () => {
    setCurrentPassword('')
    setReplacementPassword('')
    setPasswordConfirmation('')
    setPasswordDirty(false)
    setDialog('password')
  }
  const passwordValid = currentPassword.length > 0 && replacementPassword.length >= 16 && replacementPassword === passwordConfirmation

  return (
    <section aria-labelledby="page-title">
      <div className="page-heading"><div><p className="eyebrow">IPv6-only 資料平面</p><h1 id="page-title">網路</h1></div><StatusBadge state={overview.health} /></div>
      {error && <div className="inline-error page-message" role="alert">{error}</div>}
      <div className="network-grid">
        <section className="data-section" aria-labelledby="nat64-config-title">
          <div className="section-heading"><h2 id="nat64-config-title">NAT64</h2><StatusBadge state={overview.nat64.state} /></div>
          <dl className="detail-list">
            <div><dt>模式</dt><dd>{overview.nat64.manual ? '手動前綴' : '自動探索'}</dd></div>
            <div><dt>來源</dt><dd>{overview.nat64.source || '尚未取得'}</dd></div>
            <div><dt>最近檢查</dt><dd>{formatTime(overview.nat64.last_checked)}</dd></div>
          </dl>
          {overview.nat64.conflict && <p className="warning-note">探測結果互相衝突，目前採用優先 Resolver 的結果。</p>}
          {mode === 'advanced' && <button className="secondary-button" type="button" disabled={busy !== ''} onClick={() => setDialog('nat64')}><Save size={16} aria-hidden="true" />設定 NAT64</button>}
        </section>
        <section className="data-section" aria-labelledby="firewall-title">
          <div className="section-heading"><h2 id="firewall-title">防火牆診斷</h2><StatusBadge state={overview.firewall.Degraded ? 'degraded' : 'healthy'} /></div>
          {overview.firewall.Blockers.length === 0
            ? <p className="empty-state">未偵測到外部 input chain 阻擋。</p>
            : <ul className="diagnostic-list">{overview.firewall.Blockers.map((blocker) => <li key={blocker}>{blocker}</li>)}</ul>}
        </section>
      </div>

      <section className="resource-section" aria-labelledby="resolver-title">
        <div className="section-heading"><h2 id="resolver-title">IPv6-only DoT Resolver</h2><div className="row-actions"><span className="quiet-count">{mode === 'basic' ? '基礎模式唯讀' : `${overview.resolvers.length} 個`}</span>{mode === 'advanced' && <button className="secondary-button" type="button" onClick={openResolvers}>編輯 Resolver</button>}</div></div>
        {overview.resolvers.map((resolver) => <div className="line-item" key={`${resolver.name}:${resolver.address}`}><div><strong>{resolver.name}</strong><span className="mono">{resolver.address}</span></div><div><span>{resolver.server_name}:{resolver.port}</span><span>{resolver.enabled ? '已啟用' : '已停用'}</span></div></div>)}
      </section>

      {dialog === 'nat64' && <ModalDialog title="設定 NAT64" dirty={nat64Dirty} onClose={() => setDialog(null)} size="medium" footer={(requestClose) => <><button className="secondary-button" type="button" onClick={requestClose}>取消</button><button className="primary-button" type="submit" form="nat64-form" disabled={busy !== ''}>套用 NAT64 設定</button></>}><form id="nat64-form" className="modal-form" onSubmit={(event) => { event.preventDefault(); void updateNAT64(nat64Mode === 'custom' ? prefix : '') }}><label className="field"><span>NAT64 模式</span><select data-autofocus value={nat64Mode} onChange={(event) => setNAT64Mode(event.target.value as NAT64Mode)}><option value="automatic">自動探索</option><option value="custom">自訂 /96 前綴</option></select></label>{nat64Mode === 'custom' && <label className="field"><span>NAT64 /96 前綴</span><input value={prefix} onChange={(event) => setPrefix(event.target.value)} placeholder="例如 64:ff9b::/96" required /></label>}</form></ModalDialog>}

      {dialog === 'resolvers' && <ModalDialog title="編輯 Resolver" dirty={resolversDirty} onClose={() => setDialog(null)} size="wide" footer={(requestClose) => <><button className="secondary-button" type="button" onClick={requestClose}>取消</button><button className="primary-button" type="button" disabled={busy !== '' || resolvers.length === 0 || !resolvers.some((resolver) => resolver.enabled)} onClick={() => void saveResolvers()}><Save size={16} aria-hidden="true" />儲存 Resolver 設定</button></>}><div className="resolver-list">{resolvers.map((resolver, index) => <fieldset className="resolver-row" aria-label={`Resolver ${resolver.name}`} key={`${resolver.name}:${index}`}><label className="field"><span>名稱</span><input value={resolver.name} onChange={(event) => updateResolver(index, { name: event.target.value })} required /></label><label className="field"><span>IPv6 位址</span><input className="mono" value={resolver.address} onChange={(event) => updateResolver(index, { address: event.target.value })} required /></label><label className="field"><span>連接埠</span><input type="number" min="1" max="65535" value={resolver.port} onChange={(event) => updateResolver(index, { port: Number(event.target.value) })} required /></label><label className="field"><span>TLS Server Name</span><input value={resolver.server_name} onChange={(event) => updateResolver(index, { server_name: event.target.value })} required /></label><CheckboxField checked={resolver.enabled} onChange={(checked) => updateResolver(index, { enabled: checked })}>啟用</CheckboxField><button className="icon-button danger-text" type="button" title={`移除 ${resolver.name}`} aria-label={`移除 ${resolver.name}`} onClick={() => setResolvers((current) => current.filter((_, itemIndex) => itemIndex !== index))}><Trash2 size={16} aria-hidden="true" /></button></fieldset>)}</div><div className="toolbar-row compact-toolbar"><label className="field resolver-preset-field"><span>Resolver 預設</span><select value={resolverPreset} onChange={(event) => setResolverPreset(event.target.value)}><option value="">選擇可信 DNS64 預設</option>{resolverPresets.map((preset) => { const added = resolvers.some((resolver) => resolverMatchesPreset(resolver, preset)); return <option key={preset.id} value={preset.id} disabled={added}>{preset.label}{added ? '（已加入）' : ''}</option> })}</select></label><button className="secondary-button" type="button" disabled={!selectedPreset || selectedPresetAdded} onClick={addResolverPreset}><Plus size={16} aria-hidden="true" />加入 Resolver 預設</button><button className="secondary-button" type="button" onClick={() => setResolvers((current) => [...current, emptyResolver(current.length + 1)])}><Plus size={16} aria-hidden="true" />新增自訂 Resolver</button></div></ModalDialog>}

      <section className="resource-section" aria-labelledby="connectivity-title">
        <div className="section-heading"><h2 id="connectivity-title">連通性測試</h2><button className="secondary-button" type="button" disabled={busy !== ''} onClick={() => void testConnectivity()}><Play size={16} aria-hidden="true" />執行連通性測試</button></div>
        {checks === null ? <p className="empty-state">尚未執行測試</p> : <div className="connectivity-list">{checks.map((check, index) => <div className="line-item" key={`${check.kind}:${check.name}:${index}`}><div><strong>{check.name}</strong><span>{check.kind}</span></div><div><span className="mono">{check.address || '無位址'}</span></div><StatusBadge state={check.success ? 'healthy' : 'unhealthy'} label={check.success ? '通過' : '失敗'} /></div>)}</div>}
      </section>

      <section className="resource-section" aria-labelledby="password-title"><div className="section-heading"><div><h2 id="password-title"><KeyRound size={17} aria-hidden="true" />管理員密碼</h2><p className="section-copy">變更後會立即撤銷所有管理工作階段。</p></div><button className="secondary-button" type="button" onClick={openPassword}>設定管理員密碼</button></div></section>
      {dialog === 'password' && <ModalDialog title="變更管理員密碼" dirty={passwordDirty} onClose={() => setDialog(null)} size="medium" footer={(requestClose) => <><button className="secondary-button" type="button" onClick={requestClose}>取消</button><button className="primary-button" type="submit" form="password-form" disabled={busy !== '' || !passwordValid}>變更密碼</button></>}><form id="password-form" className="modal-form" aria-label="變更管理員密碼" onChange={() => setPasswordDirty(true)} onSubmit={(event) => { event.preventDefault(); if (!passwordValid) return; void run('password', async () => { await client.mutate<void>('/api/admin/password', 'POST', { current_password: currentPassword, new_password: replacementPassword }); onPasswordChanged() }, '管理員密碼變更失敗') }}><div className="form-grid"><label className="field"><span>目前密碼</span><input data-autofocus type="password" autoComplete="current-password" value={currentPassword} onChange={(event) => setCurrentPassword(event.target.value)} required /></label><label className="field"><span>新密碼</span><input type="password" autoComplete="new-password" minLength={16} maxLength={256} value={replacementPassword} onChange={(event) => setReplacementPassword(event.target.value)} required /></label><label className="field"><span>確認新密碼</span><input type="password" autoComplete="new-password" value={passwordConfirmation} onChange={(event) => setPasswordConfirmation(event.target.value)} required /></label></div><p className="form-hint">成功後所有管理工作階段將立即失效。</p></form></ModalDialog>}
    </section>
  )
}

function StatusBadge({ state, label }: { state: 'healthy' | 'degraded' | 'unhealthy'; label?: string }) {
  const text = label ?? ({ healthy: '正常', degraded: '降級', unhealthy: '異常' })[state]
  return <span className={`status-badge status-${state}`}>{text}</span>
}

function cloneResolvers(resolvers: ResolverConfig[]) { return resolvers.map((resolver) => ({ ...resolver })) }
function emptyResolver(index: number): ResolverConfig { return { name: `resolver-${index}`, address: '', port: 853, server_name: '', enabled: true } }
function resolverMatchesPreset(resolver: ResolverConfig, preset: ResolverPreset) { return resolver.address.trim().toLowerCase() === preset.address && resolver.port === preset.port }
function formatTime(value?: string) { return value ? new Date(value).toLocaleString('zh-TW') : '尚未檢查' }

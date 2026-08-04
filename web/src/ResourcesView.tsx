import { FormEvent, useEffect, useState } from 'react'
import { Plus, RefreshCw, Trash2 } from 'lucide-react'
import { AddressPool, APIError, ApiClient, NetworkCandidateSnapshot, PoolKind, ResourceMode, ResourceSnapshot } from './api'
import { nextFixedAddressName, nextPoolName, nextPrefixTemplateName } from './automaticNames'
import type { PanelMode } from './panelMode'
import { CheckboxField } from './CheckboxField'
import { ModalDialog } from './ModalDialog'

type ResourceClient = Pick<ApiClient, 'get' | 'mutate'>
type Editor = 'template' | 'fixed' | 'pool' | null

interface ResourceConfirmation {
  title: string
  message: string
  confirmLabel: string
  tone?: 'danger'
  run: () => Promise<void>
}

export function ResourcesView({ mode, client, resources, onChange }: {
  mode: PanelMode
  client: ResourceClient
  resources: ResourceSnapshot
  onChange: (resources: ResourceSnapshot) => void
}) {
  const [editor, setEditor] = useState<Editor>(null)
  const [editorDirty, setEditorDirty] = useState(false)
  const [confirmation, setConfirmation] = useState<ResourceConfirmation | null>(null)
  const [busy, setBusy] = useState(false)
  const [error, setError] = useState('')
	const [candidates, setCandidates] = useState<NetworkCandidateSnapshot | null>(null)
	const [candidateBusy, setCandidateBusy] = useState(true)
	const [candidateError, setCandidateError] = useState('')

	useEffect(() => {
		let active = true
		client.get<NetworkCandidateSnapshot>('/api/discovery/network').then((value) => {
			if (!active) return
			setCandidates(value)
			setCandidateError('')
		}).catch(() => {
			if (active) setCandidateError('網路候選偵測失敗，可使用自訂值')
		}).finally(() => {
			if (active) setCandidateBusy(false)
		})
		return () => { active = false }
	}, [client])

	const refreshCandidates = async () => {
		setCandidateBusy(true)
		try {
			setCandidates(await client.get<NetworkCandidateSnapshot>('/api/discovery/network'))
			setCandidateError('')
		} catch {
			setCandidateError(candidates ? '網路候選重新偵測失敗，保留先前結果' : '網路候選偵測失敗，可使用自訂值')
		} finally {
			setCandidateBusy(false)
		}
	}

  const run = async (operation: () => Promise<void>, fallback: string) => {
    setBusy(true)
    setError('')
    try {
      await operation()
    } catch (reason) {
      setError(reason instanceof APIError ? reason.message : fallback)
    } finally {
      setBusy(false)
    }
  }

  const closeEditor = () => {
    setEditor(null)
    setEditorDirty(false)
  }

  const openEditor = (value: Exclude<Editor, null>) => {
    setEditor(value)
    setEditorDirty(false)
  }

  const runConfirmation = async () => {
    const current = confirmation
    if (!current) return
    await current.run()
    setConfirmation(null)
  }

  const refreshPool = (pool: AddressPool) => run(async () => {
    const updated = await client.mutate<AddressPool>(`/api/resources/pools/${encodeURIComponent(pool.name)}/refresh`, 'POST', {})
    onChange({ ...resources, pools: resources.pools.map((item) => item.name === pool.name ? updated : item) })
  }, '位址池刷新失敗')

  const forceDrain = (pool: AddressPool, batch: string) => run(async () => {
    await client.mutate<void>(`/api/resources/pools/${encodeURIComponent(pool.name)}/drains/${encodeURIComponent(batch)}/force`, 'POST', { confirm: true })
    onChange({ ...resources, pools: resources.pools.map((item) => item.name === pool.name ? { ...item, draining: item.draining.filter((drain) => drain.id !== batch) } : item) })
  }, '無法強制終止 draining 批次')

  const remove = (kind: 'templates' | 'fixed' | 'pools', name: string) => run(async () => {
    await client.mutate<void>(`/api/resources/${kind}/${encodeURIComponent(name)}`, 'DELETE', {})
    onChange({ ...resources, [kind]: resources[kind].filter((item) => item.name !== name) })
  }, '資源刪除失敗')

  return (
    <section aria-labelledby="page-title">
      <div className="page-heading"><div><p className="eyebrow">位址生命週期</p><h1 id="page-title">IPv6 資源</h1></div></div>
      <div className="toolbar-row">
        <button className="primary-button" type="button" onClick={() => openEditor('template')}><Plus size={16} aria-hidden="true" />新增前綴範本</button>
        <button className="secondary-button" type="button" onClick={() => openEditor('fixed')}><Plus size={16} aria-hidden="true" />新增固定位址</button>
        <button className="secondary-button" type="button" onClick={() => openEditor('pool')}><Plus size={16} aria-hidden="true" />新增位址池</button>
		<button className="secondary-button" type="button" disabled={candidateBusy} onClick={() => void refreshCandidates()}><RefreshCw size={16} aria-hidden="true" />重新偵測網路</button>
      </div>
	  {candidateError && <div className="inline-warning page-message" role="status">{candidateError}</div>}
      {error && <div className="inline-error page-message" role="alert">{error}</div>}
      {editor === 'template' && <ModalDialog title="新增前綴範本" dirty={editorDirty} onClose={closeEditor} size="wide" footer={(requestClose) => <><button className="secondary-button" type="button" onClick={requestClose}>取消</button><button className="primary-button" type="submit" form="template-form" disabled={busy}>建立範本</button></>}><TemplateForm id="template-form" mode={mode} candidates={candidates} existing={resources.templates.map((item) => item.name)} onDirty={() => setEditorDirty(true)} onCreate={(value) => run(async () => {
        const created = await client.mutate<ResourceSnapshot['templates'][number]>('/api/resources/templates', 'POST', value)
        onChange({ ...resources, templates: [...resources.templates, created] })
        closeEditor()
      }, '前綴範本建立失敗')} /></ModalDialog>}
      {editor === 'fixed' && <ModalDialog title="新增固定位址" dirty={editorDirty} onClose={closeEditor} size="medium" footer={(requestClose) => <><button className="secondary-button" type="button" onClick={requestClose}>取消</button><button className="primary-button" type="submit" form="fixed-form" disabled={busy}>建立固定位址</button></>}><FixedForm id="fixed-form" mode={mode} templates={resources.templates.map((item) => item.name)} existing={resources.fixed.map((item) => item.name)} onDirty={() => setEditorDirty(true)} onCreate={(value) => run(async () => {
        const created = await client.mutate<ResourceSnapshot['fixed'][number]>('/api/resources/fixed', 'POST', value)
        onChange({ ...resources, fixed: [...resources.fixed, created] })
        closeEditor()
      }, '固定位址建立失敗')} /></ModalDialog>}
      {editor === 'pool' && <ModalDialog title="新增位址池" dirty={editorDirty} onClose={closeEditor} size="wide" footer={(requestClose) => <><button className="secondary-button" type="button" onClick={requestClose}>取消</button><button className="primary-button" type="submit" form="pool-form" disabled={busy}>建立位址池</button></>}><PoolForm id="pool-form" mode={mode} templates={resources.templates.map((item) => item.name)} fixed={resources.fixed.map((item) => item.name)} existing={resources.pools.map((item) => item.name)} onDirty={() => setEditorDirty(true)} onCreate={(value) => run(async () => {
        const created = await client.mutate<AddressPool>('/api/resources/pools', 'POST', value)
        onChange({ ...resources, pools: [...resources.pools, created] })
        closeEditor()
      }, '位址池建立失敗')} /></ModalDialog>}
      {confirmation && <ModalDialog title={confirmation.title} onClose={() => setConfirmation(null)} size="medium" footer={(requestClose) => <><button className="secondary-button" type="button" onClick={requestClose}>取消</button><button className={confirmation.tone === 'danger' ? 'danger-button' : 'primary-button'} type="button" disabled={busy} onClick={() => void runConfirmation()}>{confirmation.confirmLabel}</button></>}><p>{confirmation.message}</p></ModalDialog>}

      <ResourceSection title="前綴範本" count={resources.templates.length}>
        {resources.templates.map((item) => <div className="line-item" key={item.name}><div><strong>{item.name}</strong><span className="mono">{item.prefix}</span></div><div><span>{item.interface}</span><span>{modeLabel(item.mode)}</span></div><DeleteControl label={item.name} disabled={busy} onClick={() => setConfirmation({ title: `刪除前綴範本 ${item.name}`, message: '刪除前綴範本前必須先解除所有資源引用。', confirmLabel: '確認刪除', tone: 'danger', run: async () => remove('templates', item.name) })} /></div>)}
      </ResourceSection>
      <ResourceSection title="固定位址" count={resources.fixed.length}>
        {resources.fixed.map((item) => <div className="line-item" key={item.name}><div><strong>{item.name}</strong><span className="mono">{item.address}</span></div><div><span>{item.template}</span><span>{modeLabel(item.ownership)}</span></div><DeleteControl label={item.name} disabled={busy} onClick={() => setConfirmation({ title: `刪除固定位址 ${item.name}`, message: '仍被節點或位址池引用的固定位址無法刪除。', confirmLabel: '確認刪除', tone: 'danger', run: async () => remove('fixed', item.name) })} /></div>)}
      </ResourceSection>
      <ResourceSection title="位址池" count={resources.pools.length}>
        {resources.pools.map((pool) => <div className="pool-item" key={pool.name}>
          <div className="pool-summary"><div><strong>{pool.name}</strong><span>{poolKindLabel(pool.kind)} · {pool.template}</span></div><div><span>{pool.active.length} / {pool.capacity} active</span><span>{pool.draining.length} draining</span></div><div className="row-actions"><button className="icon-button" type="button" title={`刷新 ${pool.name}`} aria-label={`刷新 ${pool.name}`} disabled={busy} onClick={() => setConfirmation({ title: `刷新位址池 ${pool.name}`, message: '新連線會立即改用新位址，舊位址則等待既有連線自然排空。', confirmLabel: '確認刷新', run: async () => refreshPool(pool) })}><RefreshCw size={16} aria-hidden="true" /></button><DeleteControl label={pool.name} disabled={busy} onClick={() => setConfirmation({ title: `刪除位址池 ${pool.name}`, message: '刪除位址池會停止使用其位址；仍被節點引用時操作會被拒絕。', confirmLabel: '確認刪除', tone: 'danger', run: async () => remove('pools', pool.name) })} /></div></div>
          <div className="address-strip">{pool.active.map((address) => <code key={address}>{address}</code>)}</div>
          {pool.draining.map((batch) => <div className="drain-row" key={batch.id}><div><strong>{batch.id}</strong><span>{batch.addresses.length} 個位址仍在排空</span></div><button className="danger-link" type="button" onClick={() => setConfirmation({ title: `強制終止 ${batch.id}`, message: '會立即中止仍使用這批位址的連線。', confirmLabel: '確認強制終止', tone: 'danger', run: async () => forceDrain(pool, batch.id) })}>強制終止 {batch.id}</button></div>)}
        </div>)}
      </ResourceSection>
    </section>
  )
}

function TemplateForm({ id, mode: panelMode, candidates, existing, onDirty, onCreate }: { id: string; mode: PanelMode; candidates: NetworkCandidateSnapshot | null; existing: string[]; onDirty: () => void; onCreate: (value: { name: string; prefix: string; interface: string; mode: ResourceMode }) => void }) {
	const firstDevice = candidates?.interfaces[0]?.name ?? '__custom'
	const firstPrefix = preferredPrefix(candidates, firstDevice)
	const [deviceChoice, setDeviceChoice] = useState(firstDevice)
	const [customDevice, setCustomDevice] = useState('')
	const [prefixChoice, setPrefixChoice] = useState(firstPrefix)
	const [customPrefix, setCustomPrefix] = useState('')
	const [name, setName] = useState(nextPrefixTemplateName(firstDevice === '__custom' ? '' : firstDevice, existing))
	const [nameEdited, setNameEdited] = useState(false)
	const [deviceEdited, setDeviceEdited] = useState(false)
	const [mode, setMode] = useState<ResourceMode>('address')
	useEffect(() => {
		if (!candidates || deviceEdited || deviceChoice !== '__custom' || customDevice || prefixChoice !== '__custom' || customPrefix || nameEdited) return
		const detectedDevice = candidates.interfaces[0]?.name
		if (!detectedDevice) return
		setDeviceChoice(detectedDevice)
		setPrefixChoice(preferredPrefix(candidates, detectedDevice))
		setName(nextPrefixTemplateName(detectedDevice, existing))
	}, [candidates, customDevice, customPrefix, deviceChoice, deviceEdited, existing, nameEdited, prefixChoice])
	const selectDevice = (value: string) => {
		setDeviceEdited(true)
		setDeviceChoice(value)
		setPrefixChoice(value === '__custom' ? '__custom' : preferredPrefix(candidates, value))
		if (!nameEdited) setName(nextPrefixTemplateName(value === '__custom' ? customDevice : value, existing))
	}
	const device = deviceChoice === '__custom' ? customDevice : deviceChoice
	const prefix = prefixChoice === '__custom' ? customPrefix : prefixChoice
	const prefixes = candidates?.prefixes.filter((item) => item.interface === deviceChoice) ?? []
	return <EditorForm id={id} name="新增前綴範本" onDirty={onDirty} onSubmit={() => onCreate({ name: name.trim(), prefix: prefix.trim(), interface: device.trim(), mode })}>
		<Field label="名稱"><input data-autofocus value={name} onChange={(event) => { setNameEdited(true); setName(event.target.value) }} required /></Field>
		<Field label="Linux 介面"><select value={deviceChoice} onChange={(event) => selectDevice(event.target.value)} required>{candidates?.interfaces.map((item) => <option key={item.index} value={item.name}>{item.name}</option>)}<option value="__custom">自訂</option></select></Field>
		{deviceChoice === '__custom' && <Field label="自訂 Linux 介面"><input value={customDevice} onChange={(event) => { setCustomDevice(event.target.value); if (!nameEdited) setName(nextPrefixTemplateName(event.target.value, existing)) }} required /></Field>}
		<Field label="IPv6 前綴"><select value={prefixChoice} onChange={(event) => setPrefixChoice(event.target.value)} required>{prefixes.map((item) => <option key={item.prefix} value={item.prefix} disabled={!item.available}>{prefixOptionLabel(item)}</option>)}<option value="__custom">自訂</option></select></Field>
		{prefixChoice === '__custom' && <Field label="自訂 IPv6 前綴"><input value={customPrefix} onChange={(event) => setCustomPrefix(event.target.value)} required /></Field>}
		{panelMode === 'advanced' && <Field label="配置模式"><select value={mode} onChange={(event) => setMode(event.target.value as ResourceMode)}><option value="address">逐址配置</option><option value="local-route-freebind">Local route + freebind</option><option value="external">外部預配置</option></select></Field>}
	</EditorForm>
}

function FixedForm({ id, mode, templates, existing, onDirty, onCreate }: { id: string; mode: PanelMode; templates: string[]; existing: string[]; onDirty: () => void; onCreate: (value: { name: string; template: string; address?: string }) => void }) {
  const [name, setName] = useState(nextFixedAddressName(existing)), [template, setTemplate] = useState(templates[0] ?? ''), [address, setAddress] = useState('')
  return <EditorForm id={id} name="新增固定位址" onDirty={onDirty} onSubmit={() => onCreate({ name: name.trim(), template, ...(address.trim() ? { address: address.trim() } : {}) })}><Field label="名稱"><input data-autofocus value={name} onChange={(event) => setName(event.target.value)} required /></Field><Field label="前綴範本"><select value={template} onChange={(event) => setTemplate(event.target.value)} required><option value="" disabled>選擇範本</option>{templates.map((item) => <option key={item}>{item}</option>)}</select></Field>{mode === 'advanced' && <Field label="IPv6 位址"><input value={address} onChange={(event) => setAddress(event.target.value)} placeholder="空白自動生成" /></Field>}</EditorForm>
}

function PoolForm({ id, mode, templates, fixed, existing, onDirty, onCreate }: { id: string; mode: PanelMode; templates: string[]; fixed: string[]; existing: string[]; onDirty: () => void; onCreate: (value: { name: string; kind: PoolKind; template: string; capacity: number; pinned: string[] }) => void }) {
  const [kind, setKind] = useState<PoolKind>('inbound'), [name, setName] = useState(nextPoolName('inbound', existing)), [nameEdited, setNameEdited] = useState(false), [template, setTemplate] = useState(templates[0] ?? ''), [capacity, setCapacity] = useState('10'), [pinned, setPinned] = useState<string[]>([])
  return <EditorForm id={id} name="新增位址池" onDirty={onDirty} onSubmit={() => onCreate({ name: name.trim(), kind, template, capacity: Number(capacity), pinned })}><Field label="名稱"><input data-autofocus value={name} onChange={(event) => { setNameEdited(true); setName(event.target.value) }} required /></Field><Field label="用途"><select value={kind} onChange={(event) => { const value = event.target.value as PoolKind; setKind(value); if (!nameEdited) setName(nextPoolName(value, existing)); if (value === 'inbound') setCapacity('10'); else if (value === 'shared-outbound') setCapacity('100'); else setCapacity('15') }}><option value="inbound">動態入站</option><option value="shared-outbound">共享出站</option><option value="dedicated-outbound">節點專用出站</option></select></Field><Field label="前綴範本"><select value={template} onChange={(event) => setTemplate(event.target.value)} required><option value="" disabled>選擇範本</option>{templates.map((item) => <option key={item}>{item}</option>)}</select></Field>{mode === 'advanced' && <Field label="容量"><input type="number" min="1" max="4096" value={capacity} onChange={(event) => setCapacity(event.target.value)} required /></Field>}{mode === 'advanced' && fixed.length > 0 && <fieldset className="pin-field"><legend>釘選固定位址</legend>{fixed.map((item) => <CheckboxField key={item} checked={pinned.includes(item)} onChange={(checked) => setPinned(checked ? [...pinned, item] : pinned.filter((value) => value !== item))}>{item}</CheckboxField>)}</fieldset>}</EditorForm>
}

function preferredPrefix(candidates: NetworkCandidateSnapshot | null, device: string) {
	return candidates?.prefixes.find((item) => item.interface === device && item.available)?.prefix ?? '__custom'
}

function prefixOptionLabel(candidate: NetworkCandidateSnapshot['prefixes'][number]) {
	if (candidate.available) return `${candidate.prefix}（${candidate.sources.join(' + ')}）`
	const conflict = candidate.conflicts[0]
	if (!conflict) return `${candidate.prefix}（不可使用）`
	return `${candidate.prefix}（與 ${conflict.template} ${conflict.reason === 'exact' ? '相同' : '重疊'}）`
}

function EditorForm({ id, name, onDirty, onSubmit, children }: { id: string; name: string; onDirty: () => void; onSubmit: () => void; children: React.ReactNode }) {
  const handle = (event: FormEvent<HTMLFormElement>) => { event.preventDefault(); onSubmit() }
  return <form id={id} className="modal-form" aria-label={name} onSubmit={handle} onChange={onDirty}><div className="form-grid">{children}</div></form>
}

function ResourceSection({ title, count, children }: { title: string; count: number; children: React.ReactNode }) {
  return <section className="resource-section"><div className="section-heading"><h2>{title}</h2><span className="quiet-count">{count} 個</span></div>{count === 0 ? <p className="empty-state">目前沒有資料</p> : children}</section>
}

function DeleteControl({ label, onClick, disabled }: { label: string; onClick: () => void; disabled: boolean }) {
  return <button className="icon-button danger-text" type="button" disabled={disabled} title={`刪除 ${label}`} aria-label={`刪除 ${label}`} onClick={onClick}><Trash2 size={16} aria-hidden="true" /></button>
}

function Field({ label, children }: { label: string; children: React.ReactNode }) { return <label className="field"><span>{label}</span>{children}</label> }
function modeLabel(mode: ResourceMode) { return ({ address: '逐址配置', 'local-route-freebind': 'Local route + freebind', external: '外部預配置' })[mode] }
function poolKindLabel(kind: PoolKind) { return ({ inbound: '動態入站', 'shared-outbound': '共享出站', 'dedicated-outbound': '節點專用' })[kind] }

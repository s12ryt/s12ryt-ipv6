import { act, cleanup, render, screen, waitFor, within } from '@testing-library/react'
import userEvent from '@testing-library/user-event'
import { afterEach, describe, expect, it, vi } from 'vitest'
import type { AddressPool, ApiClient, NetworkCandidateSnapshot, ResourceSnapshot } from './api'
import { ResourcesView } from './ResourcesView'

const snapshot: ResourceSnapshot = {
  templates: [{ name: 'wan', prefix: '2001:4860:1::/64', interface: 'eth0', mode: 'address' }],
  fixed: [{ name: 'fixed-main', template: 'wan', address: '2001:4860:1::10', ownership: 'address' }],
  addresses: [{ address: '2001:4860:1::10', template: 'wan', ownership: 'address', references: 2 }],
  pools: [{
    name: 'shared-main', kind: 'shared-outbound', template: 'wan', capacity: 2,
    pinned: ['2001:4860:1::10'], active: ['2001:4860:1::10', '2001:4860:1::11'],
    draining: [{ id: 'drain-1', addresses: ['2001:4860:1::12'] }],
  }],
}

const networkCandidates: NetworkCandidateSnapshot = {
  interfaces: [{ name: 'eth0', index: 2 }, { name: 'eth1', index: 3 }],
  prefixes: [
    {
      interface: 'eth0', prefix: '2001:4860:1::/64', sources: ['address'], available: false,
      conflicts: [{ template: 'wan', reason: 'exact' }],
    },
    { interface: 'eth0', prefix: '2001:4860:2::/64', sources: ['route'], available: true, conflicts: [] },
    { interface: 'eth1', prefix: '2001:4860:3::/56', sources: ['address', 'route'], available: true, conflicts: [] },
  ],
}

function resourceClient(mutate: ReturnType<typeof vi.fn>, get = vi.fn().mockResolvedValue(networkCandidates)) {
  return { mutate, get } as Pick<ApiClient, 'get' | 'mutate'>
}

afterEach(cleanup)

describe('ResourcesView', () => {
  it('shows resource ownership and refreshes a pool without hiding draining batches', async () => {
    const user = userEvent.setup()
    const refreshed: AddressPool = {
      ...snapshot.pools[0], active: ['2001:4860:1::10', '2001:4860:1::20'],
      draining: [...snapshot.pools[0].draining, { id: 'drain-2', addresses: ['2001:4860:1::11'] }],
    }
    const mutate = vi.fn().mockResolvedValue(refreshed)
    const onChange = vi.fn()
    render(<ResourcesView mode="advanced" client={resourceClient(mutate)} resources={snapshot} onChange={onChange} />)

    expect(screen.getByText('2001:4860:1::/64')).toBeInTheDocument()
    expect(screen.getByText('fixed-main')).toBeInTheDocument()
    expect(screen.getByText('drain-1')).toBeInTheDocument()
    await user.click(screen.getByRole('button', { name: '刷新 shared-main' }))
    await waitFor(() => expect(mutate).toHaveBeenCalledWith('/api/resources/pools/shared-main/refresh', 'POST', {}))
    expect(onChange).toHaveBeenCalledWith({ ...snapshot, pools: [refreshed] })
  })

  it('creates templates, fixed addresses, and pools with explicit typed payloads', async () => {
    const user = userEvent.setup()
    const template = { name: 'route', prefix: '2001:4860:2::/56', interface: 'eth1', mode: 'local-route-freebind' as const }
    const fixed = { name: 'fixed-auto', template: 'wan', address: '2001:4860:1::30', ownership: 'address' as const }
    const pool: AddressPool = { name: 'inbound-main', kind: 'inbound', template: 'wan', capacity: 10, pinned: [], active: [], draining: [] }
    const mutate = vi.fn()
      .mockResolvedValueOnce(template)
      .mockResolvedValueOnce(fixed)
      .mockResolvedValueOnce(pool)
    const onChange = vi.fn()
    render(<ResourcesView mode="advanced" client={resourceClient(mutate)} resources={{ ...snapshot, fixed: [], addresses: [], pools: [] }} onChange={onChange} />)

    await user.click(screen.getByRole('button', { name: '新增前綴範本' }))
    const templateForm = screen.getByRole('form', { name: '新增前綴範本' })
	await user.clear(within(templateForm).getByLabelText('名稱'))
    await user.type(within(templateForm).getByLabelText('名稱'), 'route')
	await user.selectOptions(within(templateForm).getByLabelText('Linux 介面'), '__custom')
	await user.type(within(templateForm).getByLabelText('自訂 Linux 介面'), 'eth1')
	await user.type(within(templateForm).getByLabelText('自訂 IPv6 前綴'), '2001:4860:2::/56')
    await user.selectOptions(within(templateForm).getByLabelText('配置模式'), 'local-route-freebind')
    await user.click(within(templateForm).getByRole('button', { name: '建立範本' }))
    await waitFor(() => expect(mutate).toHaveBeenNthCalledWith(1, '/api/resources/templates', 'POST', template))

    await user.click(screen.getByRole('button', { name: '新增固定位址' }))
    const fixedForm = screen.getByRole('form', { name: '新增固定位址' })
	await user.clear(within(fixedForm).getByLabelText('名稱'))
    await user.type(within(fixedForm).getByLabelText('名稱'), 'fixed-auto')
    await user.selectOptions(within(fixedForm).getByLabelText('前綴範本'), 'wan')
    await user.click(within(fixedForm).getByRole('button', { name: '建立固定位址' }))
    await waitFor(() => expect(mutate).toHaveBeenNthCalledWith(2, '/api/resources/fixed', 'POST', { name: 'fixed-auto', template: 'wan' }))

    await user.click(screen.getByRole('button', { name: '新增位址池' }))
    const poolForm = screen.getByRole('form', { name: '新增位址池' })
	await user.clear(within(poolForm).getByLabelText('名稱'))
    await user.type(within(poolForm).getByLabelText('名稱'), 'inbound-main')
    await user.selectOptions(within(poolForm).getByLabelText('用途'), 'inbound')
    await user.selectOptions(within(poolForm).getByLabelText('前綴範本'), 'wan')
    await user.click(within(poolForm).getByRole('button', { name: '建立位址池' }))
    await waitFor(() => expect(mutate).toHaveBeenNthCalledWith(3, '/api/resources/pools', 'POST', {
      name: 'inbound-main', kind: 'inbound', template: 'wan', capacity: 10, pinned: [],
    }))
  })

  it('requires a second explicit action before forcing a draining batch', async () => {
    const user = userEvent.setup()
    const mutate = vi.fn().mockResolvedValue(undefined)
    const onChange = vi.fn()
    render(<ResourcesView mode="advanced" client={resourceClient(mutate)} resources={snapshot} onChange={onChange} />)

    await user.click(screen.getByRole('button', { name: '強制終止 drain-1' }))
    expect(mutate).not.toHaveBeenCalled()
    expect(screen.getByText(/會立即中止仍使用這批位址的連線/)).toBeInTheDocument()
    await user.click(screen.getByRole('button', { name: '確認強制終止 drain-1' }))

    await waitFor(() => expect(mutate).toHaveBeenCalledWith('/api/resources/pools/shared-main/drains/drain-1/force', 'POST', { confirm: true }))
    expect(onChange).toHaveBeenCalledWith({
      ...snapshot,
      pools: [{ ...snapshot.pools[0], draining: [] }],
    })
  })

  it('uses safe resource defaults in basic mode while retaining refresh and force drain', async () => {
    const user = userEvent.setup()
    const template = { name: 'basic-wan', prefix: '2001:4860:2::/64', interface: 'eth1', mode: 'address' as const }
    const fixed = { name: 'basic-fixed', template: 'wan', address: '2001:4860:1::30', ownership: 'address' as const }
    const pool: AddressPool = { name: 'basic-pool', kind: 'shared-outbound', template: 'wan', capacity: 100, pinned: [], active: [], draining: [] }
    const mutate = vi.fn().mockResolvedValueOnce(template).mockResolvedValueOnce(fixed).mockResolvedValueOnce(pool)
    render(<ResourcesView mode="basic" client={resourceClient(mutate)} resources={snapshot} onChange={vi.fn()} />)

    await user.click(screen.getByRole('button', { name: '新增前綴範本' }))
    const templateForm = screen.getByRole('form', { name: '新增前綴範本' })
    expect(within(templateForm).queryByLabelText('配置模式')).not.toBeInTheDocument()
	await user.clear(within(templateForm).getByLabelText('名稱'))
    await user.type(within(templateForm).getByLabelText('名稱'), 'basic-wan')
	await user.selectOptions(within(templateForm).getByLabelText('Linux 介面'), '__custom')
	await user.type(within(templateForm).getByLabelText('自訂 Linux 介面'), 'eth1')
	await user.type(within(templateForm).getByLabelText('自訂 IPv6 前綴'), '2001:4860:2::/64')
    await user.click(within(templateForm).getByRole('button', { name: '建立範本' }))
    await waitFor(() => expect(mutate).toHaveBeenNthCalledWith(1, '/api/resources/templates', 'POST', template))

    await user.click(screen.getByRole('button', { name: '新增固定位址' }))
    const fixedForm = screen.getByRole('form', { name: '新增固定位址' })
    expect(within(fixedForm).queryByLabelText('IPv6 位址')).not.toBeInTheDocument()
	await user.clear(within(fixedForm).getByLabelText('名稱'))
    await user.type(within(fixedForm).getByLabelText('名稱'), 'basic-fixed')
    await user.selectOptions(within(fixedForm).getByLabelText('前綴範本'), 'wan')
    await user.click(within(fixedForm).getByRole('button', { name: '建立固定位址' }))
    await waitFor(() => expect(mutate).toHaveBeenNthCalledWith(2, '/api/resources/fixed', 'POST', { name: 'basic-fixed', template: 'wan' }))

    await user.click(screen.getByRole('button', { name: '新增位址池' }))
    const poolForm = screen.getByRole('form', { name: '新增位址池' })
    expect(within(poolForm).queryByLabelText('容量')).not.toBeInTheDocument()
    expect(within(poolForm).queryByRole('group', { name: '釘選固定位址' })).not.toBeInTheDocument()
	await user.clear(within(poolForm).getByLabelText('名稱'))
    await user.type(within(poolForm).getByLabelText('名稱'), 'basic-pool')
    await user.selectOptions(within(poolForm).getByLabelText('用途'), 'shared-outbound')
    await user.selectOptions(within(poolForm).getByLabelText('前綴範本'), 'wan')
    await user.click(within(poolForm).getByRole('button', { name: '建立位址池' }))
    await waitFor(() => expect(mutate).toHaveBeenNthCalledWith(3, '/api/resources/pools', 'POST', {
      name: 'basic-pool', kind: 'shared-outbound', template: 'wan', capacity: 100, pinned: [],
    }))

    expect(screen.getByRole('button', { name: '刷新 shared-main' })).toBeInTheDocument()
    expect(screen.getByRole('button', { name: '強制終止 drain-1' })).toBeInTheDocument()
  })

  it('loads interface and prefix candidates, disables conflicts, and keeps custom input available', async () => {
    const user = userEvent.setup()
    const get = vi.fn().mockResolvedValue(networkCandidates)
    const mutate = vi.fn().mockResolvedValue({ name: '前綴 eth1 1', prefix: '2001:4860:3::/56', interface: 'eth1', mode: 'address' })
    render(<ResourcesView mode="basic" client={resourceClient(mutate, get)} resources={snapshot} onChange={vi.fn()} />)

    await waitFor(() => expect(get).toHaveBeenCalledWith('/api/discovery/network'))
    await user.click(screen.getByRole('button', { name: '新增前綴範本' }))
    const form = screen.getByRole('form', { name: '新增前綴範本' })
    await user.selectOptions(within(form).getByLabelText('Linux 介面'), 'eth0')
    const prefix = within(form).getByLabelText('IPv6 前綴')
    expect(within(prefix).getByRole('option', { name: /2001:4860:1::\/64.*wan.*相同/ })).toBeDisabled()
    await user.selectOptions(prefix, '2001:4860:2::/64')
    expect(within(form).getByLabelText('名稱')).toHaveValue('前綴 eth0 1')

    await user.selectOptions(within(form).getByLabelText('Linux 介面'), '__custom')
    expect(within(form).getByLabelText('自訂 Linux 介面')).toBeInTheDocument()
    expect(within(form).getByLabelText('自訂 IPv6 前綴')).toBeInTheDocument()
  })

  it('adopts detected candidates that arrive after an untouched template form opens', async () => {
    const user = userEvent.setup()
    let resolveCandidates: (value: NetworkCandidateSnapshot) => void = () => undefined
    const pending = new Promise<NetworkCandidateSnapshot>((resolve) => { resolveCandidates = resolve })
    const get = vi.fn().mockReturnValue(pending)
    render(<ResourcesView mode="basic" client={resourceClient(vi.fn(), get)} resources={snapshot} onChange={vi.fn()} />)

    await user.click(screen.getByRole('button', { name: '新增前綴範本' }))
    const form = screen.getByRole('form', { name: '新增前綴範本' })
    expect(within(form).getByLabelText('Linux 介面')).toHaveValue('__custom')

    await act(async () => { resolveCandidates(networkCandidates) })

    await waitFor(() => expect(within(form).getByLabelText('Linux 介面')).toHaveValue('eth0'))
    expect(within(form).getByLabelText('IPv6 前綴')).toHaveValue('2001:4860:2::/64')
    expect(within(form).getByLabelText('名稱')).toHaveValue('前綴 eth0 1')
  })

  it('retains the previous candidates when a manual refresh fails', async () => {
    const user = userEvent.setup()
    const get = vi.fn().mockResolvedValueOnce(networkCandidates).mockRejectedValueOnce(new Error('secret netlink detail'))
    render(<ResourcesView mode="advanced" client={resourceClient(vi.fn(), get)} resources={snapshot} onChange={vi.fn()} />)

    await waitFor(() => expect(get).toHaveBeenCalledTimes(1))
    await user.click(screen.getByRole('button', { name: '重新偵測網路' }))
    await waitFor(() => expect(screen.getByText('網路候選重新偵測失敗，保留先前結果')).toBeInTheDocument())
    await user.click(screen.getByRole('button', { name: '新增前綴範本' }))
    expect(within(screen.getByRole('form', { name: '新增前綴範本' })).getByRole('option', { name: 'eth1' })).toBeInTheDocument()
  })
})

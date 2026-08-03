import { cleanup, render, screen, waitFor, within } from '@testing-library/react'
import userEvent from '@testing-library/user-event'
import { afterEach, describe, expect, it, vi } from 'vitest'
import type { AddressPool, ApiClient, ResourceSnapshot } from './api'
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
    render(<ResourcesView client={{ mutate } as Pick<ApiClient, 'mutate'>} resources={snapshot} onChange={onChange} />)

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
    render(<ResourcesView client={{ mutate } as Pick<ApiClient, 'mutate'>} resources={{ ...snapshot, fixed: [], addresses: [], pools: [] }} onChange={onChange} />)

    await user.click(screen.getByRole('button', { name: '新增前綴範本' }))
    const templateForm = screen.getByRole('form', { name: '新增前綴範本' })
    await user.type(within(templateForm).getByLabelText('名稱'), 'route')
    await user.type(within(templateForm).getByLabelText('IPv6 前綴'), '2001:4860:2::/56')
    await user.type(within(templateForm).getByLabelText('Linux 介面'), 'eth1')
    await user.selectOptions(within(templateForm).getByLabelText('配置模式'), 'local-route-freebind')
    await user.click(within(templateForm).getByRole('button', { name: '建立範本' }))
    await waitFor(() => expect(mutate).toHaveBeenNthCalledWith(1, '/api/resources/templates', 'POST', template))

    await user.click(screen.getByRole('button', { name: '新增固定位址' }))
    const fixedForm = screen.getByRole('form', { name: '新增固定位址' })
    await user.type(within(fixedForm).getByLabelText('名稱'), 'fixed-auto')
    await user.selectOptions(within(fixedForm).getByLabelText('前綴範本'), 'wan')
    await user.click(within(fixedForm).getByRole('button', { name: '建立固定位址' }))
    await waitFor(() => expect(mutate).toHaveBeenNthCalledWith(2, '/api/resources/fixed', 'POST', { name: 'fixed-auto', template: 'wan' }))

    await user.click(screen.getByRole('button', { name: '新增位址池' }))
    const poolForm = screen.getByRole('form', { name: '新增位址池' })
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
    render(<ResourcesView client={{ mutate } as Pick<ApiClient, 'mutate'>} resources={snapshot} onChange={onChange} />)

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
})

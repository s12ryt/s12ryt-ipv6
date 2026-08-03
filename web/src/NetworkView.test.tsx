import { cleanup, render, screen, waitFor, within } from '@testing-library/react'
import userEvent from '@testing-library/user-event'
import { afterEach, describe, expect, it, vi } from 'vitest'
import type { ApiClient, NAT64Status, Overview } from './api'
import { NetworkView } from './NetworkView'

const overview: Overview = {
  health: 'degraded',
  nat64: {
    state: 'degraded',
    prefix: '64:ff9b::/96',
    source: 'cloudflare-primary',
    conflict: true,
    manual: false,
    last_checked: '2026-08-03T12:00:00Z',
    error: 'probe failed',
  },
  firewall: { Degraded: true, Blockers: ['inet filter input policy drop'] },
  resolvers: [
    { name: 'cloudflare-primary', address: '2606:4700:4700::64', port: 853, server_name: 'cloudflare-dns.com', enabled: true },
    { name: 'google-primary', address: '2001:4860:4860::6464', port: 853, server_name: 'dns.google', enabled: true },
  ],
}

afterEach(cleanup)

describe('NetworkView', () => {
  it('shows diagnostics and updates or clears the manual NAT64 prefix', async () => {
    const user = userEvent.setup()
    const updated: NAT64Status = { ...overview.nat64, state: 'healthy', prefix: '2001:db8:64::/96', manual: true, conflict: false, error: undefined }
    const automatic: NAT64Status = { ...updated, prefix: '64:ff9b::/96', manual: false }
    const mutate = vi.fn().mockResolvedValueOnce(updated).mockResolvedValueOnce(automatic)
    const onChange = vi.fn()

    render(<NetworkView client={{ mutate } as Pick<ApiClient, 'mutate'>} overview={overview} onChange={onChange} onPasswordChanged={vi.fn()} />)

    expect(screen.getByText('inet filter input policy drop')).toBeInTheDocument()
    expect(screen.getByText(/探測結果互相衝突/)).toBeInTheDocument()
    await user.clear(screen.getByLabelText('NAT64 /96 前綴'))
    await user.type(screen.getByLabelText('NAT64 /96 前綴'), '2001:db8:64::/96')
    await user.click(screen.getByRole('button', { name: '套用 NAT64 設定' }))

    await waitFor(() => expect(mutate).toHaveBeenNthCalledWith(1, '/api/network/nat64', 'PUT', { prefix: '2001:db8:64::/96' }))
    expect(onChange).toHaveBeenNthCalledWith(1, { ...overview, nat64: updated })

    await user.click(screen.getByRole('button', { name: '改用自動探索' }))
    await waitFor(() => expect(mutate).toHaveBeenNthCalledWith(2, '/api/network/nat64', 'PUT', { prefix: '' }))
    expect(onChange).toHaveBeenNthCalledWith(2, { ...overview, nat64: automatic })
  })

  it('edits DoT resolvers and reports every connectivity check', async () => {
    const user = userEvent.setup()
    const checks = [
      { name: 'Cloudflare DoT', kind: 'dot', success: true, address: '2606:4700:4700::64' },
      { name: 'NAT64', kind: 'nat64', success: false, error: 'unavailable' },
    ]
    const mutate = vi.fn().mockResolvedValueOnce(undefined).mockResolvedValueOnce(checks)
    const onChange = vi.fn()
    render(<NetworkView client={{ mutate } as Pick<ApiClient, 'mutate'>} overview={overview} onChange={onChange} onPasswordChanged={vi.fn()} />)

    const resolver = screen.getByRole('group', { name: 'Resolver cloudflare-primary' })
    await user.clear(within(resolver).getByLabelText('IPv6 位址'))
    await user.type(within(resolver).getByLabelText('IPv6 位址'), '2606:4700:4700::6400')
    await user.click(screen.getByRole('button', { name: '儲存 Resolver 設定' }))

    await waitFor(() => expect(mutate).toHaveBeenNthCalledWith(1, '/api/network/resolvers', 'PUT', {
      resolvers: [
        { ...overview.resolvers[0], address: '2606:4700:4700::6400' },
        overview.resolvers[1],
      ],
    }))
    expect(onChange).toHaveBeenCalledWith({
      ...overview,
      resolvers: [{ ...overview.resolvers[0], address: '2606:4700:4700::6400' }, overview.resolvers[1]],
    })

    await user.click(screen.getByRole('button', { name: '執行連通性測試' }))
    await waitFor(() => expect(mutate).toHaveBeenNthCalledWith(2, '/api/network/test', 'POST', {}))
    const results = screen.getByRole('heading', { name: '連通性測試' }).closest('section')
    expect(results).not.toBeNull()
    expect(within(results!).getByText('Cloudflare DoT')).toBeInTheDocument()
    expect(within(results!).getByText('2606:4700:4700::64')).toBeInTheDocument()
    expect(within(results!).getByText('NAT64')).toBeInTheDocument()
    expect(within(results!).getByText('失敗')).toBeInTheDocument()
    expect(screen.queryByText('unavailable')).not.toBeInTheDocument()
  })

  it('validates the replacement password and returns to login after a successful change', async () => {
    const user = userEvent.setup()
    const mutate = vi.fn().mockResolvedValue(undefined)
    const onPasswordChanged = vi.fn()
    render(<NetworkView client={{ mutate } as Pick<ApiClient, 'mutate'>} overview={overview} onChange={vi.fn()} onPasswordChanged={onPasswordChanged} />)

    const form = screen.getByRole('form', { name: '變更管理員密碼' })
    await user.type(within(form).getByLabelText('目前密碼'), 'current-password-value')
    await user.type(within(form).getByLabelText('新密碼'), 'replacement-password')
    await user.type(within(form).getByLabelText('確認新密碼'), 'different-password')
    expect(within(form).getByRole('button', { name: '變更密碼' })).toBeDisabled()

    await user.clear(within(form).getByLabelText('確認新密碼'))
    await user.type(within(form).getByLabelText('確認新密碼'), 'replacement-password')
    await user.click(within(form).getByRole('button', { name: '變更密碼' }))

    await waitFor(() => expect(mutate).toHaveBeenCalledWith('/api/admin/password', 'POST', {
      current_password: 'current-password-value',
      new_password: 'replacement-password',
    }))
    expect(onPasswordChanged).toHaveBeenCalledTimes(1)
  })
})

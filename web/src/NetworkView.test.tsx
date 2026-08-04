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

    render(<NetworkView mode="advanced" client={{ mutate } as Pick<ApiClient, 'mutate'>} overview={overview} onChange={onChange} onPasswordChanged={vi.fn()} />)

    expect(screen.getByText('inet filter input policy drop')).toBeInTheDocument()
    expect(screen.getByText(/探測結果互相衝突/)).toBeInTheDocument()
	await user.selectOptions(screen.getByLabelText('NAT64 模式'), 'custom')
    await user.clear(screen.getByLabelText('NAT64 /96 前綴'))
    await user.type(screen.getByLabelText('NAT64 /96 前綴'), '2001:db8:64::/96')
    await user.click(screen.getByRole('button', { name: '套用 NAT64 設定' }))

    await waitFor(() => expect(mutate).toHaveBeenNthCalledWith(1, '/api/network/nat64', 'PUT', { prefix: '2001:db8:64::/96' }))
    expect(onChange).toHaveBeenNthCalledWith(1, { ...overview, nat64: updated })

	await user.selectOptions(screen.getByLabelText('NAT64 模式'), 'automatic')
	expect(screen.queryByLabelText('NAT64 /96 前綴')).not.toBeInTheDocument()
	await user.click(screen.getByRole('button', { name: '套用 NAT64 設定' }))
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
    render(<NetworkView mode="advanced" client={{ mutate } as Pick<ApiClient, 'mutate'>} overview={overview} onChange={onChange} onPasswordChanged={vi.fn()} />)

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

	it('adds a trusted DNS64 preset without allowing duplicate endpoints', async () => {
		const user = userEvent.setup()
		const mutate = vi.fn().mockResolvedValue(undefined)
		render(<NetworkView mode="advanced" client={{ mutate } as Pick<ApiClient, 'mutate'>} overview={overview} onChange={vi.fn()} onPasswordChanged={vi.fn()} />)

		const presets = screen.getByLabelText('Resolver 預設')
		expect(within(presets).getByRole('option', { name: /Cloudflare DNS64 主要.*已加入/ })).toBeDisabled()
		await user.selectOptions(presets, 'cloudflare-secondary')
		await user.click(screen.getByRole('button', { name: '加入 Resolver 預設' }))

		const added = screen.getByRole('group', { name: 'Resolver cloudflare-secondary' })
		expect(within(added).getByLabelText('IPv6 位址')).toHaveValue('2606:4700:4700::6400')
		expect(within(added).getByLabelText('連接埠')).toHaveValue(853)
		expect(within(added).getByLabelText('TLS Server Name')).toHaveValue('cloudflare-dns.com')
		expect(within(presets).getByRole('option', { name: /Cloudflare DNS64 次要.*已加入/ })).toBeDisabled()
		expect(screen.getByRole('button', { name: '新增自訂 Resolver' })).toBeInTheDocument()
	})

  it('validates the replacement password and returns to login after a successful change', async () => {
    const user = userEvent.setup()
    const mutate = vi.fn().mockResolvedValue(undefined)
    const onPasswordChanged = vi.fn()
    render(<NetworkView mode="advanced" client={{ mutate } as Pick<ApiClient, 'mutate'>} overview={overview} onChange={vi.fn()} onPasswordChanged={onPasswordChanged} />)

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

  it('keeps diagnostics, connectivity, and password controls but hides network mutation in basic mode', async () => {
    render(<NetworkView mode="basic" client={{ mutate: vi.fn() } as Pick<ApiClient, 'mutate'>} overview={overview} onChange={vi.fn()} onPasswordChanged={vi.fn()} />)

    expect(screen.getByText('inet filter input policy drop')).toBeInTheDocument()
    expect(screen.getByRole('button', { name: '執行連通性測試' })).toBeInTheDocument()
    expect(screen.getByRole('form', { name: '變更管理員密碼' })).toBeInTheDocument()
    expect(screen.queryByLabelText('NAT64 /96 前綴')).not.toBeInTheDocument()
	expect(screen.queryByLabelText('NAT64 模式')).not.toBeInTheDocument()
    expect(screen.queryByRole('button', { name: '儲存 Resolver 設定' })).not.toBeInTheDocument()
    expect(screen.queryByRole('button', { name: '新增 Resolver' })).not.toBeInTheDocument()
  })
})

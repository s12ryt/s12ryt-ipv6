import { cleanup, render, screen, waitFor } from '@testing-library/react'
import userEvent from '@testing-library/user-event'
import { afterEach, describe, expect, it, vi } from 'vitest'
import type { ApiClient, NodeRecord, ResourceSnapshot } from './api'
import { NodesView } from './NodesView'

const runningNode: NodeRecord = {
  id: 'edge-1',
  name: '東京出口',
  protocol: 'mixed',
  authentication: 'credentials',
  username: 'proxy-user',
  password: 'proxy-password-value',
  max_tcp: 4096,
  max_udp: 1024,
  dial_timeout: '10s',
  handshake_timeout: '30s',
  tunnel_idle_timeout: '0s',
  udp_idle_timeout: '5m',
  ula_override: 'inherit',
  outbound: 'shared-main',
  port: 52000,
  inbound_mode: 'ipv6',
  inbound_resource: 'inbound-main',
  status: 'running',
}

const resources: ResourceSnapshot = {
  templates: [],
  fixed: [{ name: 'fixed-main', template: 'wan', address: '2001:4860::10', ownership: 'address' }],
  addresses: [],
  pools: [
    { name: 'inbound-main', kind: 'inbound', template: 'wan', capacity: 10, pinned: [], active: [], draining: [] },
    { name: 'shared-main', kind: 'shared-outbound', template: 'wan', capacity: 100, pinned: [], active: [], draining: [] },
  ],
}

afterEach(cleanup)

describe('NodesView', () => {
  it('reveals and copies credentials and changes the running state', async () => {
    const user = userEvent.setup()
    const writeText = vi.fn().mockResolvedValue(undefined)
    Object.defineProperty(navigator, 'clipboard', { configurable: true, value: { writeText } })
    const stopped = { ...runningNode, status: 'stopped' as const }
    const mutate = vi.fn().mockResolvedValue(stopped)
    const onChange = vi.fn()

    render(<NodesView client={{ mutate } as Pick<ApiClient, 'mutate'>} nodes={[runningNode]} resources={resources} onChange={onChange} />)

    expect(screen.queryByText('proxy-password-value')).not.toBeInTheDocument()
    await user.click(screen.getByRole('button', { name: '顯示 edge-1 帳密' }))
    expect(screen.getByText('proxy-password-value')).toBeInTheDocument()
    await user.click(screen.getByRole('button', { name: '複製 edge-1 連線帳密' }))
    expect(writeText).toHaveBeenCalledWith('proxy-user:proxy-password-value')

    await user.click(screen.getByRole('button', { name: '停止 edge-1' }))
    await waitFor(() => expect(mutate).toHaveBeenCalledWith('/api/nodes/edge-1/stop', 'POST', {}))
    expect(onChange).toHaveBeenCalledWith([stopped])
  })

  it('creates a credential-protected node with secure defaults', async () => {
    const user = userEvent.setup()
    const created: NodeRecord = {
      ...runningNode,
      id: 'edge-2',
      name: '新加坡出口',
      username: 'generated-user',
      password: 'generated-password-value',
    }
    const mutate = vi.fn().mockResolvedValue(created)
    const onChange = vi.fn()
    render(<NodesView client={{ mutate } as Pick<ApiClient, 'mutate'>} nodes={[]} resources={resources} onChange={onChange} />)

    await user.click(screen.getByRole('button', { name: '新增節點' }))
    await user.type(screen.getByLabelText('節點 ID'), 'edge-2')
    await user.type(screen.getByLabelText('顯示名稱'), '新加坡出口')
    await user.selectOptions(screen.getByLabelText('出站資源'), 'shared-main')
	await user.selectOptions(screen.getByLabelText('IPv6 入站資源'), 'inbound-main')
    await user.click(screen.getByRole('button', { name: '建立並啟動' }))

    await waitFor(() => expect(mutate).toHaveBeenCalledTimes(1))
    const [path, method, body] = mutate.mock.calls[0]
    expect([path, method]).toEqual(['/api/nodes', 'POST'])
    expect(body).toEqual(expect.objectContaining({
      id: 'edge-2',
      authentication: 'credentials',
      username: '',
      password: '',
      confirm_unauthenticated: false,
      max_tcp: 4096,
      max_udp: 1024,
      port: 0,
	  inbound_mode: 'ipv6',
	  inbound_resource: 'inbound-main',
    }))
    expect(onChange).toHaveBeenCalledWith([created])
  })

  it('requires explicit confirmation for an unauthenticated node and for deletion', async () => {
    const user = userEvent.setup()
    const mutate = vi.fn().mockResolvedValue(undefined)
    const onChange = vi.fn()
    render(<NodesView client={{ mutate } as Pick<ApiClient, 'mutate'>} nodes={[runningNode]} resources={resources} onChange={onChange} />)

    await user.click(screen.getByRole('button', { name: '新增節點' }))
    await user.selectOptions(screen.getByLabelText('代理認證'), 'none')
    expect(screen.getByRole('alert')).toHaveTextContent('公開代理')
    expect(screen.getByRole('button', { name: '建立並啟動' })).toBeDisabled()

    await user.click(screen.getByRole('button', { name: '取消' }))
    await user.click(screen.getByRole('button', { name: '刪除 edge-1' }))
    expect(screen.getByText(/刪除會立即中止所有連線/)).toBeInTheDocument()
    await user.click(screen.getByRole('button', { name: '確認刪除 edge-1' }))
    await waitFor(() => expect(mutate).toHaveBeenCalledWith('/api/nodes/edge-1', 'DELETE', {}))
    expect(onChange).toHaveBeenCalledWith([])
  })
})

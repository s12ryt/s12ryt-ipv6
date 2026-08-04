import { cleanup, render, screen, waitFor, within } from '@testing-library/react'
import userEvent from '@testing-library/user-event'
import { afterEach, describe, expect, it, vi } from 'vitest'
import type { ApiClient, LogEvent, StatisticsSnapshot } from './api'
import { LogsView } from './LogsView'

const events: LogEvent[] = [
  {
    time: '2026-08-03T12:00:00Z', kind: 'proxy', action: 'connect', node: 'edge-1', protocol: 'socks',
    success: true, source_ip: '2001:db8::20', destination_host: 'example.com', destination_port: 443,
    outbound_ip: '2001:4860:1::10',
  },
  { time: '2026-08-03T12:01:00Z', kind: 'audit', action: 'statistics.reset', actor: 'admin', success: true },
]

const statistics: StatisticsSnapshot = {
  nodes: {
    'edge-1': { active_tcp: 2, active_udp: 1, total_connections: 18, bytes_up: 1024, bytes_down: 2048, errors: 1 },
  },
}

afterEach(cleanup)

describe('LogsView', () => {
  it('loads metadata-only logs and applies encoded filters', async () => {
    const user = userEvent.setup()
    const get = vi.fn().mockResolvedValue(events)
    render(<LogsView mode="advanced" client={{ get, mutate: vi.fn() } as Pick<ApiClient, 'get' | 'mutate'>} statistics={statistics} onStatisticsChange={vi.fn()} />)

    await waitFor(() => expect(get).toHaveBeenCalledWith('/api/logs?limit=200'))
    expect(await screen.findByText('example.com:443')).toBeInTheDocument()
    expect(screen.getByText('2001:4860:1::10')).toBeInTheDocument()
    expect(screen.queryByText('/private/path')).not.toBeInTheDocument()

    await user.selectOptions(screen.getByLabelText('事件類型'), 'proxy')
    await user.type(screen.getByLabelText('節點篩選'), 'edge 1')
    await user.type(screen.getByLabelText('動作篩選'), 'connect')
    await user.selectOptions(screen.getByLabelText('結果'), 'false')
    await user.clear(screen.getByLabelText('筆數'))
    await user.type(screen.getByLabelText('筆數'), '50')
    await user.click(screen.getByRole('button', { name: '套用篩選' }))

    await waitFor(() => expect(get).toHaveBeenLastCalledWith('/api/logs?kind=proxy&node=edge+1&action=connect&success=false&limit=50'))
  })

  it('requires explicit confirmation before clearing all log rotations', async () => {
    const user = userEvent.setup()
    const get = vi.fn().mockResolvedValueOnce(events).mockResolvedValueOnce([events[1]])
    const mutate = vi.fn().mockResolvedValue(undefined)
    render(<LogsView mode="advanced" client={{ get, mutate } as Pick<ApiClient, 'get' | 'mutate'>} statistics={statistics} onStatisticsChange={vi.fn()} />)
    await screen.findByText('example.com:443')

    await user.click(screen.getByRole('button', { name: '清除全部日誌' }))
    expect(mutate).not.toHaveBeenCalled()
    const clearDialog = screen.getByRole('dialog', { name: '清除全部日誌' })
    expect(clearDialog).toHaveTextContent('所有輪替檔')
    await user.click(within(clearDialog).getByRole('button', { name: '確認清除全部日誌' }))

    await waitFor(() => expect(mutate).toHaveBeenCalledWith('/api/logs/clear', 'POST', { confirm: true }))
    await waitFor(() => expect(get).toHaveBeenCalledTimes(2))
    expect(screen.queryByText('example.com:443')).not.toBeInTheDocument()
    expect(screen.getByText('statistics.reset')).toBeInTheDocument()
  })

  it('resets one node or all cumulative statistics while preserving refreshed active counts', async () => {
    const user = userEvent.setup()
    const refreshed: StatisticsSnapshot = {
      nodes: { 'edge-1': { active_tcp: 2, active_udp: 1, total_connections: 0, bytes_up: 0, bytes_down: 0, errors: 0 } },
    }
    const get = vi.fn().mockResolvedValueOnce([]).mockResolvedValue(refreshed)
    const mutate = vi.fn().mockResolvedValue(undefined)
    const onStatisticsChange = vi.fn()
    render(<LogsView mode="advanced" client={{ get, mutate } as Pick<ApiClient, 'get' | 'mutate'>} statistics={statistics} onStatisticsChange={onStatisticsChange} />)
    await waitFor(() => expect(get).toHaveBeenCalledWith('/api/logs?limit=200'))

    const row = screen.getByRole('row', { name: /edge-1/ })
    expect(within(row).getByText('3')).toBeInTheDocument()
    await user.click(within(row).getByRole('button', { name: '歸零 edge-1 統計' }))
    expect(mutate).not.toHaveBeenCalled()
    const nodeDialog = screen.getByRole('dialog', { name: '歸零 edge-1 統計' })
    await user.click(within(nodeDialog).getByRole('button', { name: '確認歸零' }))

    await waitFor(() => expect(mutate).toHaveBeenCalledWith('/api/stats/reset', 'POST', { node: 'edge-1', confirm: true }))
    await waitFor(() => expect(get).toHaveBeenCalledWith('/api/stats'))
    expect(onStatisticsChange).toHaveBeenCalledWith(refreshed)

    await user.click(screen.getByRole('button', { name: '歸零全部統計' }))
    const allDialog = screen.getByRole('dialog', { name: '歸零全部統計' })
    await user.click(within(allDialog).getByRole('button', { name: '確認歸零' }))
    await waitFor(() => expect(mutate).toHaveBeenCalledWith('/api/stats/reset', 'POST', { node: '', confirm: true }))
  })

  it('retains all log filters but hides destructive maintenance in basic mode', async () => {
    const get = vi.fn().mockResolvedValue(events)
    render(<LogsView mode="basic" client={{ get, mutate: vi.fn() } as Pick<ApiClient, 'get' | 'mutate'>} statistics={statistics} onStatisticsChange={vi.fn()} />)

    await screen.findByText('example.com:443')
    for (const label of ['事件類型', '節點篩選', '動作篩選', '結果', '筆數']) {
      expect(screen.getByLabelText(label)).toBeInTheDocument()
    }
    expect(screen.getByRole('button', { name: '套用篩選' })).toBeInTheDocument()
    expect(screen.queryByRole('button', { name: '清除全部日誌' })).not.toBeInTheDocument()
    expect(screen.queryByRole('button', { name: '歸零 edge-1 統計' })).not.toBeInTheDocument()
    expect(screen.queryByRole('button', { name: '歸零全部統計' })).not.toBeInTheDocument()
    expect(screen.queryByText('操作')).not.toBeInTheDocument()
  })
})

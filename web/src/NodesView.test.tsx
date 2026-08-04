import { cleanup, render, screen, waitFor, within } from '@testing-library/react'
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
    { name: 'inbound-main', kind: 'inbound', template: 'wan', capacity: 10, pinned: [], active: ['2001:4860::20', '2001:4860::21'], draining: [] },
    { name: 'shared-main', kind: 'shared-outbound', template: 'wan', capacity: 100, pinned: [], active: [], draining: [] },
  ],
}

afterEach(() => {
  cleanup()
  localStorage.clear()
  vi.restoreAllMocks()
})

describe('NodesView', () => {
  it('previews and creates multiple nodes with shared settings and distinct generated credentials', async () => {
    const user = userEvent.setup()
    const created = [
      { ...runningNode, id: 'node-001', name: '節點 1', folder: '批次 1', username: 'generated-1', password: 'generated-password-1' },
      { ...runningNode, id: 'node-002', name: '節點 2', folder: '批次 1', username: 'generated-2', password: 'generated-password-2' },
    ]
    const mutate = vi.fn().mockResolvedValue(created)
    const onChange = vi.fn()
    render(<NodesView mode="advanced" client={{ mutate } as Pick<ApiClient, 'mutate'>} nodes={[]} resources={resources} onChange={onChange} />)

    await user.click(screen.getByRole('button', { name: '一鍵建立多節點' }))
    expect(screen.getByRole('dialog', { name: '一鍵建立多節點' })).toBeInTheDocument()
    expect(screen.getByText('步驟 1 / 3')).toBeInTheDocument()
    expect(screen.getByLabelText('資料夾名稱')).toHaveValue('批次 1')
    expect(screen.getByLabelText('節點數量')).toHaveValue(5)
    await user.clear(screen.getByLabelText('節點數量'))
    await user.type(screen.getByLabelText('節點數量'), '2')
    await user.selectOptions(screen.getByLabelText('批次出站資源'), 'shared-main')
    await user.selectOptions(screen.getByLabelText('批次 IPv6 入站資源'), 'inbound-main')
    await user.click(screen.getByRole('button', { name: '下一步：預覽' }))
    expect(screen.getByText('步驟 2 / 3')).toBeInTheDocument()
    expect(screen.getByLabelText('預覽 1 節點 ID')).toHaveValue('node-001')
    expect(screen.getByLabelText('預覽 2 顯示名稱')).toHaveValue('節點 2')
    expect(screen.getByLabelText('預覽 2 代理埠')).toHaveValue(0)

    await user.click(screen.getByRole('button', { name: '下一步：確認' }))
    expect(screen.getByText('步驟 3 / 3')).toBeInTheDocument()
    await user.click(screen.getByRole('button', { name: '建立 2 個節點' }))

    await waitFor(() => expect(mutate).toHaveBeenCalledTimes(1))
    const [path, method, body] = mutate.mock.calls[0]
    expect([path, method]).toEqual(['/api/nodes/batch', 'POST'])
    expect(body).toEqual(expect.objectContaining({
      folder: '批次 1',
      confirm_unauthenticated: false,
      nodes: [
        expect.objectContaining({ id: 'node-001', name: '節點 1', port: 0, username: '', password: '' }),
        expect.objectContaining({ id: 'node-002', name: '節點 2', port: 0, username: '', password: '' }),
      ],
    }))
    expect(body.nodes[0]).not.toHaveProperty('folder')
    expect(body.nodes[0]).toEqual(expect.objectContaining({ max_tcp: 4096, max_udp: 1024 }))
    expect(onChange).toHaveBeenCalledWith(created)
  })

  it('groups, collapses, renames, and moves nodes without restarting them', async () => {
    const user = userEvent.setup()
    const members = [
      { ...runningNode, id: 'edge-2', name: '節點二', folder: '批次 1' },
      { ...runningNode, id: 'edge-1', name: '節點一', folder: '批次 1' },
      { ...runningNode, id: 'loose', name: '未分類節點', folder: '' },
    ]
    const renamed = members.slice(0, 2).map((node) => ({ ...node, folder: '東京群組' }))
    const renamedByID = [renamed[1], renamed[0]]
    const moved = { ...members[2], folder: '東京群組' }
    const mutate = vi.fn()
      .mockResolvedValueOnce(renamed)
      .mockResolvedValueOnce(moved)
    const onChange = vi.fn()
    const view = render(<NodesView mode="advanced" client={{ mutate } as Pick<ApiClient, 'mutate'>} nodes={members} resources={resources} onChange={onChange} />)

    const folder = screen.getByRole('region', { name: '資料夾 批次 1' })
    expect(within(folder).getAllByRole('row').map((row) => row.textContent)).toEqual(expect.arrayContaining([
      expect.stringContaining('edge-1'),
      expect.stringContaining('edge-2'),
    ]))
    await user.click(within(folder).getByRole('button', { name: '收合 批次 1' }))
    expect(within(folder).queryByText('edge-1')).not.toBeInTheDocument()
    expect(localStorage.getItem('s12ryt_node_folders_collapsed')).toContain('批次 1')
    await user.click(within(folder).getByRole('button', { name: '展開 批次 1' }))

    await user.click(within(folder).getByRole('button', { name: '重新命名 批次 1' }))
    expect(screen.getByRole('dialog', { name: '重新命名資料夾 批次 1' })).toBeInTheDocument()
    await user.clear(screen.getByLabelText('資料夾新名稱'))
    await user.type(screen.getByLabelText('資料夾新名稱'), '東京群組')
    await user.click(screen.getByRole('button', { name: '確認重新命名' }))
    await waitFor(() => expect(mutate).toHaveBeenCalledWith('/api/node-folders/rename', 'PUT', { source: '批次 1', target: '東京群組' }))
    expect(onChange).toHaveBeenCalledWith([...renamedByID, members[2]])

    view.rerender(<NodesView mode="advanced" client={{ mutate } as Pick<ApiClient, 'mutate'>} nodes={[...renamedByID, members[2]]} resources={resources} onChange={onChange} />)
    await user.click(screen.getByRole('button', { name: '移動 loose' }))
    expect(screen.getByRole('dialog', { name: '移動節點 loose' })).toBeInTheDocument()
    await user.selectOptions(screen.getByLabelText('目標資料夾'), '東京群組')
    await user.click(screen.getByRole('button', { name: '確認移動' }))
    await waitFor(() => expect(mutate).toHaveBeenCalledWith('/api/nodes/loose/folder', 'PUT', { folder: '東京群組' }))
    expect(onChange).toHaveBeenCalledWith([...renamedByID, moved])
  })

  it('copies a folder and preserves successful bulk actions while reporting failures', async () => {
    const user = userEvent.setup()
    const writeText = vi.fn().mockResolvedValue(undefined)
    Object.defineProperty(navigator, 'clipboard', { configurable: true, value: { writeText } })
    vi.spyOn(Math, 'random').mockReturnValue(0)
    const members = [
      { ...runningNode, id: 'edge-1', folder: '批次 1' },
      { ...runningNode, id: 'edge-2', folder: '批次 1', username: 'second-user', password: 'second-password' },
    ]
    const mutate = vi.fn().mockResolvedValue({ succeeded: ['edge-1'], failed: [{ id: 'edge-2', error: 'node operation failed' }] })
    const onChange = vi.fn()
    render(<NodesView mode="advanced" client={{ mutate } as Pick<ApiClient, 'mutate'>} nodes={members} resources={resources} onChange={onChange} />)
    const folder = screen.getByRole('region', { name: '資料夾 批次 1' })

    await user.click(within(folder).getByRole('button', { name: '複製 批次 1 全部帳密' }))
    expect(writeText).toHaveBeenCalledWith('edge-1\tproxy-user:proxy-password-value\nedge-2\tsecond-user:second-password')
    await user.click(within(folder).getByRole('button', { name: '全部停止 批次 1' }))
    expect(mutate).not.toHaveBeenCalled()
    const stopDialog = screen.getByRole('dialog', { name: '停止 批次 1 全部節點' })
    await user.click(within(stopDialog).getByRole('button', { name: '確認全部停止' }))
    await waitFor(() => expect(mutate).toHaveBeenCalledWith('/api/node-folders/action', 'POST', { folder: '批次 1', action: 'stop' }))
    expect(onChange).toHaveBeenCalledWith([
      { ...members[0], status: 'stopped' },
      members[1],
    ])
    expect(screen.getByRole('alert')).toHaveTextContent('edge-2：node operation failed')

    await user.click(within(folder).getByRole('button', { name: '刪除資料夾 批次 1' }))
    expect(screen.getByRole('dialog', { name: '刪除資料夾 批次 1' })).toHaveTextContent('將逐一刪除資料夾內全部節點')
    expect(mutate).toHaveBeenCalledTimes(1)
  })

  it('reveals and copies credentials and changes the running state', async () => {
    const user = userEvent.setup()
    const writeText = vi.fn().mockResolvedValue(undefined)
    Object.defineProperty(navigator, 'clipboard', { configurable: true, value: { writeText } })
    const stopped = { ...runningNode, status: 'stopped' as const }
    const mutate = vi.fn().mockResolvedValue(stopped)
    const onChange = vi.fn()

    render(<NodesView mode="advanced" client={{ mutate } as Pick<ApiClient, 'mutate'>} nodes={[runningNode]} resources={resources} onChange={onChange} />)

    expect(screen.queryByText('proxy-password-value')).not.toBeInTheDocument()
    await user.click(screen.getByRole('button', { name: '顯示 edge-1 帳密' }))
    expect(screen.getByText('proxy-password-value')).toBeInTheDocument()
    await user.click(screen.getByRole('button', { name: '複製 edge-1 連線帳密' }))
    expect(writeText).toHaveBeenCalledWith('proxy-user:proxy-password-value')

    await user.click(screen.getByRole('button', { name: '停止 edge-1' }))
    expect(mutate).not.toHaveBeenCalled()
    await user.click(within(screen.getByRole('dialog', { name: '停止節點 edge-1' })).getByRole('button', { name: '確認停止' }))
    await waitFor(() => expect(mutate).toHaveBeenCalledWith('/api/nodes/edge-1/stop', 'POST', {}))
    expect(onChange).toHaveBeenCalledWith([stopped])
  })

  it('copies standard connection URIs and reports success beside the node actions', async () => {
    const user = userEvent.setup()
    const writeText = vi.fn().mockResolvedValue(undefined)
    Object.defineProperty(navigator, 'clipboard', { configurable: true, value: { writeText } })
    vi.spyOn(Math, 'random').mockReturnValue(0)

    render(<NodesView mode="advanced" client={{ mutate: vi.fn() } as Pick<ApiClient, 'mutate'>} nodes={[runningNode]} resources={resources} onChange={vi.fn()} />)

    await user.click(screen.getByRole('button', { name: '複製 edge-1 連線資訊' }))
    expect(writeText).toHaveBeenCalledWith([
      'socks5://proxy-user:proxy-password-value@[2001:4860::20]:52000',
      'http://proxy-user:proxy-password-value@[2001:4860::20]:52000',
    ].join('\n'))
    expect(screen.getByRole('status')).toHaveTextContent('已複製')
  })

  it('falls back on public HTTP and presents a manual copy dialog when every mechanism fails', async () => {
    const user = userEvent.setup()
    const execCommand = vi.fn().mockReturnValue(true)
    vi.spyOn(Math, 'random').mockReturnValue(0)
    Object.defineProperty(navigator, 'clipboard', { configurable: true, value: undefined })
    Object.defineProperty(document, 'execCommand', { configurable: true, value: execCommand })

    const view = render(<NodesView mode="advanced" client={{ mutate: vi.fn() } as Pick<ApiClient, 'mutate'>} nodes={[runningNode]} resources={resources} onChange={vi.fn()} />)
    await user.click(screen.getByRole('button', { name: '複製 edge-1 連線帳密' }))
    expect(execCommand).toHaveBeenCalledWith('copy')
    expect(screen.getByRole('status')).toHaveTextContent('已複製')

    view.unmount()
    Object.defineProperty(navigator, 'clipboard', { configurable: true, value: { writeText: vi.fn().mockRejectedValue(new Error('denied')) } })
    Object.defineProperty(document, 'execCommand', { configurable: true, value: vi.fn().mockReturnValue(false) })
    render(<NodesView mode="advanced" client={{ mutate: vi.fn() } as Pick<ApiClient, 'mutate'>} nodes={[runningNode]} resources={resources} onChange={vi.fn()} />)

    await user.click(screen.getByRole('button', { name: '複製 edge-1 連線資訊' }))
    const dialog = screen.getByRole('dialog', { name: '手動複製連線資訊' })
    expect(within(dialog).getByText('瀏覽器禁止自動存取剪貼簿，請手動複製以下內容。')).toBeInTheDocument()
    expect(within(dialog).getByLabelText('手動複製內容')).toHaveValue([
      'socks5://proxy-user:proxy-password-value@[2001:4860::20]:52000',
      'http://proxy-user:proxy-password-value@[2001:4860::20]:52000',
    ].join('\n'))
    await user.click(within(dialog).getByRole('button', { name: '完成' }))
    expect(screen.queryByRole('dialog')).not.toBeInTheDocument()
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
    render(<NodesView mode="advanced" client={{ mutate } as Pick<ApiClient, 'mutate'>} nodes={[]} resources={resources} onChange={onChange} />)

    await user.click(screen.getByRole('button', { name: '新增節點' }))
	expect(screen.getByRole('dialog', { name: '新增節點' })).toBeInTheDocument()
	expect(screen.getByLabelText('節點 ID')).toHaveValue('node-001')
	expect(screen.getByLabelText('顯示名稱')).toHaveValue('節點 1')
	await user.clear(screen.getByLabelText('節點 ID'))
    await user.type(screen.getByLabelText('節點 ID'), 'edge-2')
	await user.clear(screen.getByLabelText('顯示名稱'))
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
    render(<NodesView mode="advanced" client={{ mutate } as Pick<ApiClient, 'mutate'>} nodes={[runningNode]} resources={resources} onChange={onChange} />)

    await user.click(screen.getByRole('button', { name: '新增節點' }))
    await user.selectOptions(screen.getByLabelText('代理認證'), 'none')
    expect(screen.getByText(/無認證可能使此節點成為公開代理/)).toBeInTheDocument()
    expect(screen.getByRole('button', { name: '建立並啟動' })).toBeDisabled()

    const nodeForm = screen.getByRole('dialog', { name: '新增節點' })
    expect(nodeForm).not.toBeNull()
    await user.click(within(nodeForm).getByRole('button', { name: '取消' }))
    await user.click(screen.getByRole('button', { name: '放棄變更' }))
    await user.click(screen.getByRole('button', { name: '刪除 edge-1' }))
    const deleteDialog = screen.getByRole('dialog', { name: '刪除節點 edge-1' })
    expect(deleteDialog).toHaveTextContent('刪除會立即中止所有連線')
    await user.click(within(deleteDialog).getByRole('button', { name: '確認刪除' }))
    await waitFor(() => expect(mutate).toHaveBeenCalledWith('/api/nodes/edge-1', 'DELETE', {}))
    expect(onChange).toHaveBeenCalledWith([])
  })

  it('never presents named-folder deletion controls for unclassified nodes', () => {
    render(<NodesView mode="advanced" client={{ mutate: vi.fn() } as Pick<ApiClient, 'mutate'>} nodes={[runningNode]} resources={resources} onChange={vi.fn()} />)

    expect(screen.getByRole('region', { name: '資料夾 未分類' })).toBeInTheDocument()
    expect(screen.queryByText(/將逐一刪除資料夾內全部節點/)).not.toBeInTheDocument()
    expect(screen.queryByRole('button', { name: '確認刪除資料夾' })).not.toBeInTheDocument()
  })

  it('keeps common controls visible in basic mode and preserves hidden advanced values', async () => {
    const user = userEvent.setup()
    const customized: NodeRecord = {
      ...runningNode,
      max_tcp: 777,
      max_udp: 333,
      dial_timeout: '17s',
      handshake_timeout: '41s',
      tunnel_idle_timeout: '2m',
      udp_idle_timeout: '9m',
      ula_override: 'allow',
    }
    const updated = { ...customized, name: '更新名稱' }
    const mutate = vi.fn().mockResolvedValue(updated)

    render(<NodesView mode="basic" client={{ mutate } as Pick<ApiClient, 'mutate'>} nodes={[customized]} resources={resources} onChange={vi.fn()} />)

    await user.click(screen.getByRole('button', { name: '編輯 edge-1' }))
    expect(screen.getByLabelText('代理埠')).toBeInTheDocument()
    expect(screen.getByLabelText('代理帳號')).toBeInTheDocument()
    expect(screen.queryByLabelText('TCP 上限')).not.toBeInTheDocument()
    expect(screen.queryByLabelText('UDP association 上限')).not.toBeInTheDocument()
    expect(screen.queryByLabelText('Dial timeout')).not.toBeInTheDocument()
    expect(screen.queryByLabelText('ULA 政策')).not.toBeInTheDocument()

    await user.clear(screen.getByLabelText('顯示名稱'))
    await user.type(screen.getByLabelText('顯示名稱'), '更新名稱')
    await user.click(screen.getByRole('button', { name: '儲存並切換' }))

    await waitFor(() => expect(mutate).toHaveBeenCalledWith('/api/nodes/edge-1', 'PUT', expect.objectContaining({
      name: '更新名稱',
      max_tcp: 777,
      max_udp: 333,
      dial_timeout: '17s',
      handshake_timeout: '41s',
      tunnel_idle_timeout: '2m',
      udp_idle_timeout: '9m',
      ula_override: 'allow',
    })))
  })

  it('preserves the batch preview while switching between basic and advanced modes', async () => {
    const user = userEvent.setup()
    const view = render(<NodesView mode="basic" client={{ mutate: vi.fn() } as Pick<ApiClient, 'mutate'>} nodes={[]} resources={resources} onChange={vi.fn()} />)

    await user.click(screen.getByRole('button', { name: '一鍵建立多節點' }))
    await user.clear(screen.getByLabelText('節點數量'))
    await user.type(screen.getByLabelText('節點數量'), '2')
    await user.click(screen.getByRole('button', { name: '下一步：預覽' }))
    await user.clear(screen.getByLabelText('預覽 1 顯示名稱'))
    await user.type(screen.getByLabelText('預覽 1 顯示名稱'), '尚未送出的批次節點')
    expect(screen.queryByLabelText('批次 TCP 上限')).not.toBeInTheDocument()

    view.rerender(<NodesView mode="advanced" client={{ mutate: vi.fn() } as Pick<ApiClient, 'mutate'>} nodes={[]} resources={resources} onChange={vi.fn()} />)

    expect(screen.getByLabelText('預覽 1 顯示名稱')).toHaveValue('尚未送出的批次節點')
    await user.click(screen.getByRole('button', { name: '上一步' }))
    expect(screen.getByLabelText('批次 TCP 上限')).toHaveValue(4096)
  })

  it('requires one explicit risk confirmation for an unauthenticated batch', async () => {
    const user = userEvent.setup()
    render(<NodesView mode="advanced" client={{ mutate: vi.fn() } as Pick<ApiClient, 'mutate'>} nodes={[]} resources={resources} onChange={vi.fn()} />)

    await user.click(screen.getByRole('button', { name: '一鍵建立多節點' }))
    await user.selectOptions(screen.getByLabelText('批次代理認證'), 'none')
    expect(screen.getByText(/整批節點將不使用認證/)).toBeInTheDocument()
    expect(screen.getByRole('button', { name: '下一步：預覽' })).toBeDisabled()

    await user.click(screen.getByLabelText('我確認整批公開代理風險'))
    expect(screen.getByRole('button', { name: '下一步：預覽' })).toBeEnabled()
  })
})

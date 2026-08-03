import { cleanup, render, screen, waitFor } from '@testing-library/react'
import userEvent from '@testing-library/user-event'
import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest'
import { App } from './App'

const overview = {
  health: 'healthy',
  nat64: {
    state: 'healthy',
    prefix: '64:ff9b::/96',
    source: 'cloudflare-primary',
    conflict: false,
    manual: false,
    last_checked: '2026-08-03T10:00:00Z',
  },
  firewall: { Degraded: false, Blockers: [] },
  resolvers: [],
}

const nodes = [
  {
    id: 'edge-1',
    name: '東京出口',
    protocol: 'mixed',
    max_tcp: 4096,
    max_udp: 1024,
    dial_timeout: '10s',
    handshake_timeout: '30s',
    tunnel_idle_timeout: '0s',
    udp_idle_timeout: '5m0s',
    ula_override: 'inherit',
    outbound: 'shared-main',
    port: 52000,
	inbound_mode: 'ipv6',
	inbound_resource: 'inbound-main',
    status: 'running',
  },
]

const resources = {
  templates: [
    { name: 'wan', prefix: '2001:4860:1::/64', interface: 'eth0', mode: 'address' },
  ],
  fixed: [],
  addresses: [],
  pools: [],
}

const statistics = {
  nodes: {
    'edge-1': {
      active_tcp: 2,
      active_udp: 1,
      total_connections: 18,
      bytes_up: 1024,
      bytes_down: 2048,
      errors: 0,
    },
  },
}

function jsonResponse(body: unknown, status = 200) {
  return new Response(JSON.stringify(body), {
    status,
    headers: { 'Content-Type': 'application/json' },
  })
}

function installFetch(initiallyAuthenticated = false) {
  let authenticated = initiallyAuthenticated
  const fetchMock = vi.fn(async (input: RequestInfo | URL, init?: RequestInit) => {
    const path = typeof input === 'string' ? input : input.toString()
    const method = init?.method ?? 'GET'
    if (path === '/api/session' && method === 'GET') {
      return authenticated
        ? jsonResponse({ authenticated: true, csrf_token: 'session-csrf' })
        : jsonResponse({ error: 'authentication required' }, 401)
    }
    if (path === '/api/session' && method === 'POST') {
      authenticated = true
      return jsonResponse({ csrf_token: 'csrf-only-in-memory' })
    }
    if (path === '/api/session/logout' && method === 'POST') {
      authenticated = false
      return new Response(null, { status: 204 })
    }
    if (path === '/api/overview') return jsonResponse(overview)
    if (path === '/api/nodes') return jsonResponse(nodes)
    if (path === '/api/resources') return jsonResponse(resources)
    if (path === '/api/stats') return jsonResponse(statistics)
    return jsonResponse({ error: 'not found' }, 404)
  })
  vi.stubGlobal('fetch', fetchMock)
  return fetchMock
}

describe('App', () => {
  beforeEach(() => {
    localStorage.clear()
    document.documentElement.removeAttribute('data-theme')
  })

  afterEach(() => {
    cleanup()
    vi.unstubAllGlobals()
  })

  it('checks the current session and shows the HTTP risk before authentication', async () => {
    const fetchMock = installFetch()

    render(<App />)

    expect(await screen.findByRole('heading', { name: 's12ryt IPv6 管理中心' })).toBeInTheDocument()
    expect(screen.getByRole('alert')).toHaveTextContent('此管理介面使用未加密 HTTP')
    expect(screen.getByLabelText('管理員密碼')).toHaveAttribute('type', 'password')
    expect(fetchMock).toHaveBeenCalledWith('/api/session', expect.objectContaining({ credentials: 'same-origin' }))
  })

  it('reports an invalid login without describing it as an expired session', async () => {
    const user = userEvent.setup()
    vi.stubGlobal('fetch', vi.fn(async (_input: RequestInfo | URL, init?: RequestInit) => {
      if ((init?.method ?? 'GET') === 'POST') return jsonResponse({ error: 'invalid credentials' }, 401)
      return jsonResponse({ error: 'authentication required' }, 401)
    }))
    render(<App />)

    await user.type(await screen.findByLabelText('管理員密碼'), 'wrong-password')
    await user.click(screen.getByRole('button', { name: '登入' }))

    expect(await screen.findByText('登入失敗，請檢查管理員密碼')).toHaveAttribute('role', 'alert')
    expect(screen.queryByText('管理工作階段已失效')).not.toBeInTheDocument()
  })

  it('logs in, keeps CSRF in memory, loads the operator shell, and logs out', async () => {
    const user = userEvent.setup()
    const fetchMock = installFetch()
    render(<App />)

    await user.type(await screen.findByLabelText('管理員密碼'), 'correct horse battery staple')
    await user.click(screen.getByRole('button', { name: '登入' }))

    expect(await screen.findByRole('heading', { name: '總覽' })).toBeInTheDocument()
    for (const label of ['總覽', '節點', 'IPv6 資源', '網路', '日誌']) {
      expect(screen.getByRole('button', { name: label })).toBeInTheDocument()
    }
    expect(screen.getByText('東京出口')).toBeInTheDocument()
    expect(screen.getByText('64:ff9b::/96')).toBeInTheDocument()
    expect(screen.getByText('3')).toBeInTheDocument()
    expect(localStorage.getItem('s12ryt_csrf')).toBeNull()

    expect(fetchMock).toHaveBeenCalledWith(
      '/api/session',
      expect.objectContaining({
        method: 'POST',
        credentials: 'same-origin',
        body: JSON.stringify({ password: 'correct horse battery staple' }),
      }),
    )
    for (const path of ['/api/overview', '/api/nodes', '/api/resources', '/api/stats']) {
      expect(fetchMock).toHaveBeenCalledWith(path, expect.objectContaining({ credentials: 'same-origin' }))
    }

    await user.click(screen.getByRole('button', { name: '登出' }))
    expect(await screen.findByLabelText('管理員密碼')).toBeInTheDocument()
    const logoutCall = fetchMock.mock.calls.find(([path]) => path === '/api/session/logout')
    expect(logoutCall?.[1]).toEqual(expect.objectContaining({ method: 'POST' }))
    expect(new Headers(logoutCall?.[1]?.headers).get('X-CSRF-Token')).toBe('csrf-only-in-memory')
  })

  it('loads an existing session and persists an explicit theme choice', async () => {
    localStorage.setItem('s12ryt_theme', 'dark')
    const user = userEvent.setup()
    installFetch(true)

    render(<App />)

    expect(await screen.findByRole('heading', { name: '總覽' })).toBeInTheDocument()
    expect(document.documentElement).toHaveAttribute('data-theme', 'dark')
    expect(screen.getByRole('button', { name: '使用深色主題' })).toHaveAttribute('aria-pressed', 'true')

    await user.click(screen.getByRole('button', { name: '跟隨系統主題' }))
    await waitFor(() => expect(document.documentElement).not.toHaveAttribute('data-theme'))
    expect(localStorage.getItem('s12ryt_theme')).toBe('system')

    await user.click(screen.getByRole('button', { name: '使用亮色主題' }))
    expect(document.documentElement).toHaveAttribute('data-theme', 'light')
    expect(localStorage.getItem('s12ryt_theme')).toBe('light')
  })

  it('refreshes only affected data from SSE events and closes the stream on logout', async () => {
    const user = userEvent.setup()
    const listeners = new Map<string, (event: MessageEvent<string>) => void>()
    const close = vi.fn()
    class FakeEventSource {
      constructor(path: string) { expect(path).toBe('/api/events') }
      addEventListener(type: string, listener: (event: MessageEvent<string>) => void) { listeners.set(type, listener) }
      close() { close() }
    }
    vi.stubGlobal('EventSource', FakeEventSource)
    const fetchMock = installFetch(true)
    render(<App />)
    await screen.findByRole('heading', { name: '總覽' })

    listeners.get('node.changed')?.(new MessageEvent('node.changed', { data: JSON.stringify({
      type: 'node.changed', resource: 'node', id: 'edge-1', action: 'updated', state: 'running', time: '2026-08-03T12:00:00Z',
    }) }))
    await waitFor(() => expect(fetchMock.mock.calls.filter(([path]) => path === '/api/nodes')).toHaveLength(2))
    expect(fetchMock.mock.calls.filter(([path]) => path === '/api/resources')).toHaveLength(1)

    listeners.get('operations.changed')?.(new MessageEvent('operations.changed', { data: JSON.stringify({
      type: 'operations.changed', resource: 'statistics', id: 'all', action: 'reset', state: 'updated', time: '2026-08-03T12:01:00Z',
    }) }))
    await waitFor(() => expect(fetchMock.mock.calls.filter(([path]) => path === '/api/stats')).toHaveLength(2))

    await user.click(screen.getByRole('button', { name: '登出' }))
    await screen.findByLabelText('管理員密碼')
    expect(close).toHaveBeenCalledTimes(1)
  })
})

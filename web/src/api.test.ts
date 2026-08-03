import { describe, expect, it, vi } from 'vitest'
import { APIError, ApiClient } from './api'

function jsonResponse(body: unknown, status = 200) {
  return new Response(JSON.stringify(body), {
    status,
    headers: { 'Content-Type': 'application/json' },
  })
}

describe('ApiClient', () => {
  it('keeps the CSRF token in memory and adds it only to mutations', async () => {
    const fetcher = vi
      .fn<typeof fetch>()
      .mockResolvedValueOnce(jsonResponse({ authenticated: true }))
      .mockResolvedValueOnce(jsonResponse({ csrf_token: 'memory-token' }))
      .mockResolvedValueOnce(new Response(null, { status: 204 }))
    const client = new ApiClient({ fetcher, origin: 'http://localhost:34466' })

    await expect(client.currentSession()).resolves.toBe(true)
    await client.login('secret-password')
    await client.logout()

    expect(fetcher).toHaveBeenNthCalledWith(1, '/api/session', expect.objectContaining({ credentials: 'same-origin' }))
    expect(fetcher).toHaveBeenNthCalledWith(
      2,
      '/api/session',
      expect.objectContaining({ method: 'POST', body: JSON.stringify({ password: 'secret-password' }) }),
    )
    expect(fetcher).toHaveBeenNthCalledWith(3, '/api/session/logout', expect.objectContaining({ method: 'POST' }))
    const logoutHeaders = new Headers(fetcher.mock.calls[2][1]?.headers)
    expect(logoutHeaders.get('Content-Type')).toBe('application/json')
    expect(logoutHeaders.get('Origin')).toBe('http://localhost:34466')
    expect(logoutHeaders.get('X-CSRF-Token')).toBe('memory-token')
  })

  it('loads initial operator data in parallel', async () => {
    const payloads: Record<string, unknown> = {
      '/api/overview': { health: 'healthy', nat64: {}, firewall: { Degraded: false, Blockers: [] }, resolvers: [] },
      '/api/nodes': [],
      '/api/resources': { templates: [], fixed: [], addresses: [], pools: [] },
      '/api/stats': { nodes: {} },
    }
    const fetcher = vi.fn<typeof fetch>(async (input) => jsonResponse(payloads[input.toString()]))
    const client = new ApiClient({ fetcher, origin: 'http://localhost:34466' })

    await expect(client.loadInitial()).resolves.toEqual({
      overview: payloads['/api/overview'],
      nodes: [],
      resources: payloads['/api/resources'],
      statistics: payloads['/api/stats'],
    })
    expect(fetcher).toHaveBeenCalledTimes(4)
  })

  it('maps an unauthenticated session probe to false and sanitizes API failures', async () => {
    const fetcher = vi
      .fn<typeof fetch>()
      .mockResolvedValueOnce(jsonResponse({ error: 'authentication required' }, 401))
      .mockResolvedValueOnce(jsonResponse({ error: 'internal secret detail' }, 500))
    const client = new ApiClient({ fetcher, origin: 'http://localhost:34466' })

    await expect(client.currentSession()).resolves.toBe(false)
    await expect(client.login('bad-password')).rejects.toEqual(
      expect.objectContaining<Partial<APIError>>({ status: 500, message: '伺服器暫時無法完成要求' }),
    )
  })

  it('uses the rotated CSRF token returned after a page refresh', async () => {
    const fetcher = vi
      .fn<typeof fetch>()
      .mockResolvedValueOnce(jsonResponse({ authenticated: true, csrf_token: 'rotated-token' }))
      .mockResolvedValueOnce(new Response(null, { status: 204 }))
    const client = new ApiClient({ fetcher, origin: 'http://localhost:34466' })

    await expect(client.currentSession()).resolves.toBe(true)
    await client.logout()

    const headers = new Headers(fetcher.mock.calls[1][1]?.headers)
    expect(headers.get('X-CSRF-Token')).toBe('rotated-token')
  })

  it('subscribes to fixed SSE event types, ignores malformed data, and closes cleanly', () => {
    const listeners = new Map<string, (event: MessageEvent<string>) => void>()
    const source = {
      addEventListener: vi.fn((type: string, listener: (event: MessageEvent<string>) => void) => listeners.set(type, listener)),
      close: vi.fn(),
    }
    const onEvent = vi.fn()
    const onError = vi.fn()
    const client = new ApiClient({
      fetcher: vi.fn<typeof fetch>(),
      origin: 'http://localhost:34466',
      eventSource: (path) => {
        expect(path).toBe('/api/events')
        return source
      },
    })

    const close = client.subscribe(onEvent, onError)
    listeners.get('node.changed')?.(new MessageEvent('node.changed', { data: JSON.stringify({
      type: 'node.changed', resource: 'node', id: 'edge-1', action: 'updated', state: 'running', time: '2026-08-03T12:00:00Z',
    }) }))
    listeners.get('resource.changed')?.(new MessageEvent('resource.changed', { data: '{broken' }))
    listeners.get('operations.changed')?.(new MessageEvent('operations.changed', { data: JSON.stringify({ type: 'unexpected', resource: 'log' }) }))
    listeners.get('error')?.(new MessageEvent('error'))

    expect(onEvent).toHaveBeenCalledTimes(1)
    expect(onEvent).toHaveBeenCalledWith(expect.objectContaining({ resource: 'node', id: 'edge-1' }))
    expect(onError).toHaveBeenCalledTimes(1)
    close()
    close()
    expect(source.close).toHaveBeenCalledTimes(1)
  })
})

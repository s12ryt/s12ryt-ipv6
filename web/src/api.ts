export type HealthState = 'healthy' | 'degraded' | 'unhealthy'
export type NodeProtocol = 'socks' | 'http' | 'mixed'
export type NodeStatus = 'running' | 'stopped'
export type ULAOverride = 'inherit' | 'allow' | 'deny'
export type InboundMode = 'ipv4' | 'ipv6' | 'dual'
export type BindProtocol = 'tcp' | 'udp'
export type BindFamily = 'ipv4' | 'ipv6'
export type ResourceMode = 'address' | 'local-route-freebind' | 'external'
export type PoolKind = 'inbound' | 'shared-outbound' | 'dedicated-outbound'
export type PrefixSource = 'address' | 'route'
export type PrefixConflictReason = 'exact' | 'overlap'

export interface NetworkCandidateSnapshot {
  interfaces: Array<{ name: string; index: number }>
  prefixes: Array<{
    interface: string
    prefix: string
    sources: PrefixSource[]
    available: boolean
    conflicts: Array<{ template: string; reason: PrefixConflictReason }>
  }>
}

export interface ResolverConfig {
  name: string
  address: string
  port: number
  server_name: string
  enabled: boolean
}

export interface NAT64Status {
  state: HealthState
  prefix?: string
  source?: string
  conflict: boolean
  manual: boolean
  last_checked?: string
  error?: string
}

export interface Overview {
  health: HealthState
  nat64: NAT64Status
  firewall: {
    Degraded: boolean
    Blockers: string[]
  }
  resolvers: ResolverConfig[]
}

export interface ConnectivityCheck {
  name: string
  kind: string
  success: boolean
  address?: string
  error?: string
}

export interface BindSpec {
  protocol: BindProtocol
  family: BindFamily
  address?: string
  interface?: string
  freebind: boolean
}

export interface NodeRecord {
  id: string
  name: string
  folder?: string
  protocol: NodeProtocol
  authentication?: 'credentials' | 'none'
  username?: string
  password?: string
  max_tcp: number
  max_udp: number
  dial_timeout: string
  handshake_timeout: string
  tunnel_idle_timeout: string
  udp_idle_timeout: string
  ula_override: ULAOverride
  outbound: string
  dedicated_pool?: string
  port: number
  inbound_mode: InboundMode
  inbound_resource?: string
  status: NodeStatus
  warning?: string
  confirm_unauthenticated?: boolean
}

export interface NodeMutation extends Omit<NodeRecord, 'status' | 'warning'> {
  authentication: 'credentials' | 'none'
  confirm_unauthenticated: boolean
}

export interface PrefixTemplate {
  name: string
  prefix: string
  interface: string
  mode: ResourceMode
}

export interface FixedAddress {
  name: string
  template: string
  address: string
  ownership: ResourceMode
}

export interface CanonicalAddress {
  address: string
  template: string
  ownership: ResourceMode
  references: number
}

export interface DrainBatch {
  id: string
  addresses: string[]
}

export interface AddressPool {
  name: string
  kind: PoolKind
  template: string
  capacity: number
  pinned: string[]
  active: string[]
  draining: DrainBatch[]
}

export interface ResourceSnapshot {
  templates: PrefixTemplate[]
  fixed: FixedAddress[]
  addresses: CanonicalAddress[]
  pools: AddressPool[]
}

export interface NodeCounters {
  active_tcp: number
  active_udp: number
  total_connections: number
  bytes_up: number
  bytes_down: number
  errors: number
}

export interface StatisticsSnapshot {
  nodes: Record<string, NodeCounters>
}

export type LogKind = 'proxy' | 'system' | 'audit'

export interface LogEvent {
  time: string
  kind: LogKind
  action: string
  actor?: string
  node?: string
  protocol?: string
  success: boolean
  source_ip?: string
  destination_host?: string
  destination_port?: number
  outbound_ip?: string
  error?: string
}

export interface AdminEvent {
  type: 'node.changed' | 'resource.changed' | 'operations.changed'
  resource: string
  id?: string
  action: string
  state: string
  time: string
}

interface EventSourceConnection {
  addEventListener(type: string, listener: (event: MessageEvent<string>) => void): void
  close(): void
}

type EventSourceFactory = (path: string) => EventSourceConnection

export interface InitialData {
  overview: Overview
  nodes: NodeRecord[]
  resources: ResourceSnapshot
  statistics: StatisticsSnapshot
}

interface ApiClientOptions {
  fetcher?: typeof fetch
  origin?: string
  eventSource?: EventSourceFactory
}

export class APIError extends Error {
  readonly status: number

  constructor(status: number, message: string) {
    super(message)
    this.name = 'APIError'
    this.status = status
  }
}

export class ApiClient {
  private readonly fetcher: typeof fetch
  private readonly origin: string
  private readonly eventSource?: EventSourceFactory
  private csrfToken = ''

  constructor(options: ApiClientOptions = {}) {
    this.fetcher = options.fetcher ?? fetch.bind(globalThis)
    this.origin = options.origin ?? window.location.origin
    this.eventSource = options.eventSource ?? (typeof EventSource === 'undefined'
      ? undefined
      : (path) => new EventSource(path) as unknown as EventSourceConnection)
  }

  async currentSession(): Promise<boolean> {
    const response = await this.fetcher('/api/session', { credentials: 'same-origin' })
    if (response.status === 401) return false
    const session = await this.readJSON<{ authenticated: boolean; csrf_token?: string }>(response)
    this.csrfToken = session.csrf_token ?? ''
    return session.authenticated === true
  }

  async login(password: string): Promise<void> {
    const session = await this.requestJSON<{ csrf_token: string }>('/api/session', {
      method: 'POST',
      body: JSON.stringify({ password }),
    })
    if (!session.csrf_token) throw new APIError(500, '伺服器回應格式無效')
    this.csrfToken = session.csrf_token
  }

  async logout(): Promise<void> {
    try {
      await this.mutate<void>('/api/session/logout', 'POST', {})
    } finally {
      this.csrfToken = ''
    }
  }

  async loadInitial(): Promise<InitialData> {
    const [overview, nodes, resources, statistics] = await Promise.all([
      this.get<Overview>('/api/overview'),
      this.get<NodeRecord[]>('/api/nodes'),
      this.get<ResourceSnapshot>('/api/resources'),
      this.get<StatisticsSnapshot>('/api/stats'),
    ])
    return { overview, nodes, resources, statistics }
  }

  async get<T>(path: string): Promise<T> {
    return this.requestJSON<T>(path)
  }

  async mutate<T>(path: string, method: 'POST' | 'PUT' | 'DELETE', body: unknown): Promise<T> {
    if (!this.csrfToken) throw new APIError(401, '目前工作階段無法執行變更，請重新登入')
    return this.requestJSON<T>(path, {
      method,
      headers: {
        Origin: this.origin,
        'X-CSRF-Token': this.csrfToken,
      },
      body: JSON.stringify(body),
    })
  }

  subscribe(onEvent: (event: AdminEvent) => void, onError: () => void = () => undefined): () => void {
    if (!this.eventSource) return () => undefined
    const source = this.eventSource('/api/events')
    const types: AdminEvent['type'][] = ['node.changed', 'resource.changed', 'operations.changed']
    for (const expectedType of types) {
      source.addEventListener(expectedType, (message) => {
        try {
          const value = JSON.parse(message.data) as unknown
          if (isAdminEvent(value, expectedType)) onEvent(value)
        } catch {
          // Ignore malformed stream data and wait for the next fixed-schema event.
        }
      })
    }
    source.addEventListener('error', onError)
    let closed = false
    return () => {
      if (closed) return
      closed = true
      source.close()
    }
  }

  private async requestJSON<T>(path: string, init: RequestInit = {}): Promise<T> {
    const headers = new Headers(init.headers)
    if (init.body !== undefined) headers.set('Content-Type', 'application/json')
    const response = await this.fetcher(path, {
      ...init,
      headers,
      credentials: 'same-origin',
    })
    if (!response.ok) throw new APIError(response.status, apiErrorMessage(response.status))
    if (response.status === 204) return undefined as T
    return this.readJSON<T>(response)
  }

  private async readJSON<T>(response: Response): Promise<T> {
    if (!response.ok) throw new APIError(response.status, apiErrorMessage(response.status))
    try {
      return (await response.json()) as T
    } catch {
      throw new APIError(response.status, '伺服器回應格式無效')
    }
  }
}

function isAdminEvent(value: unknown, expectedType: AdminEvent['type']): value is AdminEvent {
  if (typeof value !== 'object' || value === null) return false
  const event = value as Record<string, unknown>
  if (event.type !== expectedType) return false
  for (const field of ['resource', 'action', 'state', 'time'] as const) {
    if (typeof event[field] !== 'string' || event[field].length === 0 || event[field].length > 256) return false
  }
  if (event.id !== undefined && (typeof event.id !== 'string' || event.id.length > 256)) return false
  return !Number.isNaN(Date.parse(event.time as string))
}

function apiErrorMessage(status: number): string {
  switch (status) {
    case 400:
    case 422:
      return '要求內容無效'
    case 401:
      return '管理工作階段已失效'
    case 403:
      return '此操作已被拒絕'
    case 404:
      return '找不到指定資源'
    case 409:
      return '目前狀態與操作衝突'
    case 429:
      return '嘗試次數過多，請稍後再試'
    default:
      return '伺服器暫時無法完成要求'
  }
}

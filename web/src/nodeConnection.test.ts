import { describe, expect, it } from 'vitest'
import type { NodeRecord, ResourceSnapshot } from './api'
import { buildNodeConnectionInfo } from './nodeConnection'

const baseNode: NodeRecord = {
  id: 'edge-1',
  name: '東京出口',
  protocol: 'mixed',
  authentication: 'credentials',
  username: 'user:name',
  password: 'p@ ss/%',
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
  inbound_resource: 'fixed-in',
  status: 'running',
}

const resources: ResourceSnapshot = {
  templates: [],
  fixed: [{ name: 'fixed-in', template: 'wan', address: '2001:4860::10', ownership: 'address' }],
  addresses: [],
  pools: [{
    name: 'pool-in', kind: 'inbound', template: 'wan', capacity: 2, pinned: [],
    active: ['2001:4860::20', '2001:4860::21'], draining: [],
  }],
}

describe('buildNodeConnectionInfo', () => {
  it('builds encoded SOCKS5 and HTTP URIs for a fixed IPv6 inbound', () => {
    expect(buildNodeConnectionInfo(baseNode, resources, 'panel.example.test', () => 0)).toBe([
      'socks5://user%3Aname:p%40%20ss%2F%25@[2001:4860::10]:52000',
      'http://user%3Aname:p%40%20ss%2F%25@[2001:4860::10]:52000',
    ].join('\n'))
  })

  it('selects one active inbound pool address for each copy operation', () => {
    const node = { ...baseNode, protocol: 'socks' as const, inbound_resource: 'pool-in' }

    expect(buildNodeConnectionInfo(node, resources, 'panel.example.test', () => 0.75)).toBe(
      'socks5://user%3Aname:p%40%20ss%2F%25@[2001:4860::21]:52000',
    )
  })

  it('uses the panel hostname for IPv4 and emits both unique hosts for dual stack', () => {
    const unauthenticated = {
      ...baseNode,
      authentication: 'none' as const,
      username: '',
      password: '',
      inbound_mode: 'dual' as const,
    }

    expect(buildNodeConnectionInfo(unauthenticated, resources, 'panel.example.test', () => 0)).toBe([
      'socks5://panel.example.test:52000',
      'socks5://[2001:4860::10]:52000',
      'http://panel.example.test:52000',
      'http://[2001:4860::10]:52000',
    ].join('\n'))

    expect(buildNodeConnectionInfo({ ...unauthenticated, protocol: 'http', inbound_mode: 'ipv4' }, resources, '[2001:db8::5]', () => 0)).toBe(
      'http://[2001:db8::5]:52000',
    )
  })

  it('rejects missing, empty, outbound, and ambiguous inbound resources', () => {
    const poolNode = { ...baseNode, inbound_resource: 'pool-in' }
    const emptyPool = {
      ...resources,
      pools: [{ ...resources.pools[0], active: [] }],
    }
    expect(() => buildNodeConnectionInfo(poolNode, emptyPool, 'panel.example.test')).toThrow('入站池目前沒有可用的 IPv6 位址')

    expect(() => buildNodeConnectionInfo({ ...baseNode, inbound_resource: 'missing' }, resources, 'panel.example.test')).toThrow('找不到節點的 IPv6 入站資源')

    const outboundPool = {
      ...resources,
      pools: [{ ...resources.pools[0], kind: 'shared-outbound' as const }],
    }
    expect(() => buildNodeConnectionInfo(poolNode, outboundPool, 'panel.example.test')).toThrow('節點引用的資源不是入站池')

    const ambiguous = {
      ...resources,
      fixed: [...resources.fixed, { ...resources.fixed[0], name: 'pool-in' }],
    }
    expect(() => buildNodeConnectionInfo(poolNode, ambiguous, 'panel.example.test')).toThrow('節點的 IPv6 入站資源名稱不唯一')
  })
})

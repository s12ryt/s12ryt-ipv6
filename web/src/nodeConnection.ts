import type { NodeProtocol, NodeRecord, ResourceSnapshot } from './api'

type RandomSource = () => number

export function buildNodeConnectionInfo(
  node: NodeRecord,
  resources: ResourceSnapshot,
  panelHostname: string,
  random: RandomSource = Math.random,
): string {
  if (!Number.isInteger(node.port) || node.port < 1 || node.port > 65535) {
    throw new Error('節點目前沒有可用的代理埠')
  }

  const hosts: string[] = []
  if (node.inbound_mode === 'ipv4' || node.inbound_mode === 'dual') {
    const panelHost = normalizeHost(panelHostname)
    if (!panelHost) throw new Error('目前管理面板網址沒有可用的 hostname')
    hosts.push(panelHost)
  }
  if (node.inbound_mode === 'ipv6' || node.inbound_mode === 'dual') {
    hosts.push(resolveIPv6Inbound(node, resources, random))
  }

  const uniqueHosts = [...new Set(hosts)]
  const schemes = schemesFor(node.protocol)
  const userinfo = credentialUserinfo(node)
  return schemes.flatMap((scheme) => uniqueHosts.map((host) => (
    `${scheme}://${userinfo}${formatHost(host)}:${node.port}`
  ))).join('\n')
}

function resolveIPv6Inbound(node: NodeRecord, resources: ResourceSnapshot, random: RandomSource): string {
  const name = node.inbound_resource?.trim()
  if (!name) throw new Error('節點尚未設定 IPv6 入站資源')

  const fixed = resources.fixed.filter((item) => item.name === name)
  const pools = resources.pools.filter((item) => item.name === name)
  if (fixed.length+pools.length > 1) throw new Error('節點的 IPv6 入站資源名稱不唯一')
  if (fixed.length === 1) return normalizeIPv6(fixed[0].address)
  if (pools.length === 0) throw new Error('找不到節點的 IPv6 入站資源')

  const pool = pools[0]
  if (pool.kind !== 'inbound') throw new Error('節點引用的資源不是入站池')
  if (pool.active.length === 0) throw new Error('入站池目前沒有可用的 IPv6 位址')
  const value = random()
  const index = Math.min(pool.active.length - 1, Math.max(0, Math.floor(Number.isFinite(value) ? value * pool.active.length : 0)))
  return normalizeIPv6(pool.active[index])
}

function schemesFor(protocol: NodeProtocol): string[] {
  if (protocol === 'socks') return ['socks5']
  if (protocol === 'http') return ['http']
  return ['socks5', 'http']
}

function credentialUserinfo(node: NodeRecord): string {
  const authentication = node.authentication ?? (node.username ? 'credentials' : 'none')
  if (authentication === 'none') return ''
  if (!node.username || !node.password) throw new Error('節點帳密尚未完整產生')
  return `${encodeURIComponent(node.username)}:${encodeURIComponent(node.password)}@`
}

function normalizeIPv6(value: string): string {
  const host = normalizeHost(value)
  if (!host.includes(':') || /\s/.test(host)) throw new Error('IPv6 入站資源包含無效位址')
  return host
}

function normalizeHost(value: string): string {
  const host = value.trim()
  if (host.startsWith('[') && host.endsWith(']')) return host.slice(1, -1)
  return host
}

function formatHost(host: string): string {
  return host.includes(':') ? `[${host}]` : host
}

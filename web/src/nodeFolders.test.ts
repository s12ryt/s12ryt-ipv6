import { beforeEach, describe, expect, it } from 'vitest'
import type { NodeRecord } from './api'
import { groupNodesByFolder, persistCollapsedFolders, storedCollapsedFolders } from './nodeFolders'

const node = (id: string, folder = ''): NodeRecord => ({
  id,
  name: id,
  folder,
  protocol: 'socks',
  authentication: 'credentials',
  username: 'user',
  password: 'password-value',
  max_tcp: 1,
  max_udp: 1,
  dial_timeout: '1s',
  handshake_timeout: '1s',
  tunnel_idle_timeout: '0s',
  udp_idle_timeout: '1s',
  ula_override: 'inherit',
  outbound: 'fixed',
  port: 52000,
  inbound_mode: 'ipv4',
  status: 'running',
})

beforeEach(() => localStorage.clear())

describe('node folders', () => {
  it('sorts named folders and nodes while placing unclassified nodes last', () => {
    const groups = groupNodesByFolder([
      node('node-3'),
      node('node-2', '乙'),
      node('node-1', '甲'),
      node('node-0', '乙'),
    ])
    expect(groups.map((group) => [group.name, group.label, group.nodes.map((item) => item.id)])).toEqual([
      ['乙', '乙', ['node-0', 'node-2']],
      ['甲', '甲', ['node-1']],
      ['', '未分類', ['node-3']],
    ])
  })

  it('persists only normalized collapsed folder keys and tolerates invalid storage', () => {
    persistCollapsedFolders(new Set(['批次 2', '批次 1', '批次 2']))
    expect(localStorage.getItem('s12ryt_node_folders_collapsed')).toBe('["批次 1","批次 2"]')
    expect(storedCollapsedFolders()).toEqual(new Set(['批次 1', '批次 2']))
    localStorage.setItem('s12ryt_node_folders_collapsed', '{broken')
    expect(storedCollapsedFolders()).toEqual(new Set())
  })
})

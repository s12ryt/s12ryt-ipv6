import { describe, expect, it } from 'vitest'
import { nextBatchFolderName, nextFixedAddressName, nextNodeIdentities, nextNodeIdentity, nextPoolName, nextPrefixTemplateName } from './automaticNames'

describe('automatic resource names', () => {
  it('chooses the first node number whose ID and display name are both unused', () => {
    expect(nextNodeIdentity([
      { id: 'node-001', name: '舊節點' },
      { id: 'manual', name: '節點 2' },
    ])).toEqual({ id: 'node-003', name: '節點 3' })
  })

  it('generates editable resource names while skipping existing names', () => {
    expect(nextPrefixTemplateName('eth0', ['前綴 eth0 1', '前綴 eth1 1'])).toBe('前綴 eth0 2')
    expect(nextFixedAddressName(['固定位址 1', 'custom'])).toBe('固定位址 2')
    expect(nextPoolName('inbound', ['入站池 1'])).toBe('入站池 2')
    expect(nextPoolName('shared-outbound', ['共享出站池 1'])).toBe('共享出站池 2')
    expect(nextPoolName('dedicated-outbound', ['專用出站池 1'])).toBe('專用出站池 2')
  })

  it('generates a unique batch folder and consecutive node preview identities', () => {
    expect(nextBatchFolderName(['批次 1', '自訂', '批次 3'])).toBe('批次 2')
    expect(nextNodeIdentities([
      { id: 'node-001', name: '節點 1' },
      { id: 'manual', name: '節點 3' },
    ], 3)).toEqual([
      { id: 'node-002', name: '節點 2' },
      { id: 'node-004', name: '節點 4' },
      { id: 'node-005', name: '節點 5' },
    ])
  })
})

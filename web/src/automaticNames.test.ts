import { describe, expect, it } from 'vitest'
import { nextFixedAddressName, nextNodeIdentity, nextPoolName, nextPrefixTemplateName } from './automaticNames'

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
})

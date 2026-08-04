import type { PoolKind } from './api'

export function nextNodeIdentity(existing: Array<{ id: string; name: string }>) {
  const ids = new Set(existing.map((item) => item.id))
  const names = new Set(existing.map((item) => item.name))
  for (let number = 1; ; number += 1) {
    const id = `node-${String(number).padStart(3, '0')}`
    const name = `節點 ${number}`
    if (!ids.has(id) && !names.has(name)) return { id, name }
  }
}

export function nextNodeIdentities(existing: Array<{ id: string; name: string }>, count: number) {
  const generated: Array<{ id: string; name: string }> = []
  for (let index = 0; index < count; index += 1) {
    generated.push(nextNodeIdentity([...existing, ...generated]))
  }
  return generated
}

export function nextBatchFolderName(existing: string[]) {
  return nextIndexedName('批次', existing)
}

export function nextPrefixTemplateName(device: string, existing: string[]) {
  return nextIndexedName(`前綴 ${device.trim() || '介面'}`, existing)
}

export function nextFixedAddressName(existing: string[]) {
  return nextIndexedName('固定位址', existing)
}

export function nextPoolName(kind: PoolKind, existing: string[]) {
  const prefix = {
    inbound: '入站池',
    'shared-outbound': '共享出站池',
    'dedicated-outbound': '專用出站池',
  }[kind]
  return nextIndexedName(prefix, existing)
}

function nextIndexedName(prefix: string, existing: string[]) {
  const names = new Set(existing)
  for (let number = 1; ; number += 1) {
    const candidate = `${prefix} ${number}`
    if (!names.has(candidate)) return candidate
  }
}

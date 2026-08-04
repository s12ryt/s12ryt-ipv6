import type { NodeRecord } from './api'

const collapsedFoldersKey = 's12ryt_node_folders_collapsed'

export interface NodeFolderGroup {
  name: string
  label: string
  nodes: NodeRecord[]
}

export function groupNodesByFolder(nodes: NodeRecord[]): NodeFolderGroup[] {
  const grouped = new Map<string, NodeRecord[]>()
  for (const node of nodes) {
    const folder = node.folder?.trim() ?? ''
    const group = grouped.get(folder) ?? []
    group.push(node)
    grouped.set(folder, group)
  }

  return [...grouped.entries()]
    .sort(([left], [right]) => {
      if (!left) return 1
      if (!right) return -1
      return left.localeCompare(right, 'zh-Hant')
    })
    .map(([name, group]) => ({
      name,
      label: name || '未分類',
      nodes: [...group].sort((left, right) => left.id.localeCompare(right.id)),
    }))
}

export function storedCollapsedFolders(): Set<string> {
  try {
    const value: unknown = JSON.parse(localStorage.getItem(collapsedFoldersKey) ?? '[]')
    if (!Array.isArray(value)) return new Set()
    return new Set(value.filter((item): item is string => typeof item === 'string' && item.trim() !== ''))
  } catch {
    return new Set()
  }
}

export function persistCollapsedFolders(folders: Set<string>) {
  const normalized = [...folders]
    .map((folder) => folder.trim())
    .filter(Boolean)
    .filter((folder, index, values) => values.indexOf(folder) === index)
    .sort((left, right) => left.localeCompare(right, 'zh-Hant'))
  localStorage.setItem(collapsedFoldersKey, JSON.stringify(normalized))
}

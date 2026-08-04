export type PanelMode = 'basic' | 'advanced'

export function storedPanelMode(): PanelMode {
  return localStorage.getItem('s12ryt_panel_mode') === 'advanced' ? 'advanced' : 'basic'
}

export function persistPanelMode(mode: PanelMode) {
  localStorage.setItem('s12ryt_panel_mode', mode)
}

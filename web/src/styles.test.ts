import { readFileSync } from 'node:fs'
import { resolve } from 'node:path'
import { describe, expect, it } from 'vitest'

const css = readFileSync(resolve(process.cwd(), 'src/styles.css'), 'utf8')

describe('responsive layout contract', () => {
  it('switches the operator shell to its compact layout at tablet widths', () => {
    const tablet = css.slice(css.indexOf('@media (max-width: 1024px)'), css.indexOf('@media (max-width: 430px)'))

    expect(tablet).toContain('.app-shell { grid-template: auto 56px auto 1fr / minmax(0, 1fr); }')
    expect(tablet).toContain('.overview-grid { grid-template-columns: minmax(0, 1fr); }')
    expect(tablet).toContain('.resource-table-row { grid-template-columns: repeat(2, minmax(0, 1fr)); }')
    expect(tablet).toContain('.resolver-row { grid-template-columns: repeat(2, minmax(0, 1fr)); }')
  })

  it('constrains large batch previews and stacks folder controls on narrow screens', () => {
    const mobile = css.slice(css.indexOf('@media (max-width: 430px)'), css.indexOf('@keyframes pulse'))

    expect(css).toContain('.node-folder-move { grid-column: 1 / -1;')
    expect(css).toContain('.batch-preview { max-height: min(480px, 60vh); overflow-y: auto;')
    expect(css).toContain('.batch-preview-head, .batch-preview-row { display: grid;')
    expect(mobile).toContain('.batch-preview-head { display: none; }')
    expect(mobile).toContain('.batch-preview-row { grid-template-columns: minmax(0, 1fr); }')
    expect(mobile).toContain('.node-folder-heading { align-items: stretch; flex-direction: column; }')
  })

  it('styles modal structure, scroll isolation, and custom form controls', () => {
    expect(css).toContain('body.modal-open { overflow: hidden; }')
    expect(css).toContain('.modal-layer { position: fixed;')
    expect(css).toContain('grid-template-rows: auto minmax(0, 1fr) auto;')
    expect(css).toContain('.modal-body { overflow-y: auto;')
    expect(css).toContain('.modal-footer { display: flex;')
    expect(css).toContain('textarea { min-width: 0;')
    expect(css).toContain('.custom-checkbox input { position: absolute;')
    expect(css).toContain('.custom-checkbox input:checked + .checkbox-control')
    expect(css).toContain('.custom-checkbox input:focus-visible + .checkbox-control')
  })

  it('animates the operator shell without layout-shifting hover effects', () => {
    expect(css).toContain('.app-shell.sidebar-collapsed { grid-template-columns: 68px minmax(0, 1fr); }')
    expect(css).toContain('transition: grid-template-columns 180ms ease;')
    expect(css).toContain('@keyframes modal-enter')
    expect(css).toContain('@keyframes backdrop-enter')
    expect(css).toContain('@keyframes page-enter')
    expect(css).toContain('@keyframes disclosure-enter')
    expect(css).toContain('@keyframes feedback-enter')
    expect(css).not.toMatch(/:hover[^}]*transform:\s*scale/)
  })

  it('makes modal dialogs nearly full-screen on phones', () => {
    const mobile = css.slice(css.indexOf('@media (max-width: 430px)'), css.indexOf('@keyframes pulse'))

    expect(mobile).toContain('.modal-layer { padding: 0; }')
    expect(mobile).toContain('height: 100dvh;')
    expect(mobile).toContain('max-height: none;')
    expect(mobile).toContain('border-radius: 0;')
  })
})

describe('visual refresh contract', () => {
  it('defines layered theme tokens for canvas, chrome, and elevated surfaces', () => {
    expect(css).toContain('--chrome:')
    expect(css).toContain('--on-primary:')
    expect(css).toContain('--shadow-sm:')
    expect(css).toContain('color-scheme: light')
    expect(css).toContain('color-scheme: dark')
  })

  it('keeps the zh-TW-aware system font stack without external font loading', () => {
    expect(css).toContain('"Noto Sans TC"')
    expect(css).not.toContain('fonts.googleapis')
    expect(css).not.toContain('@import')
    expect(css).not.toContain('@font-face')
  })

  it('gives the brand mark a teal gradient identity', () => {
    expect(css).toMatch(/\.product-mark \{[^}]*linear-gradient\(135deg/)
  })

  it('renders live status pills with soft fills and dot indicators', () => {
    expect(css).toContain('.status-badge::before')
    expect(css).toMatch(/\.status-badge::before \{[^}]*border-radius: 999px/)
  })

  it('marks the active nav item with an inset accent rail on desktop', () => {
    expect(css).toMatch(/\.nav-button\.active \{[^}]*inset 3px 0 0 0 var\(--primary\)/)
  })

  it('elevates metric strips and data sections as cards', () => {
    expect(css).toMatch(/\.metrics \{[^}]*background: var\(--surface\)/)
    expect(css).toMatch(/\.data-section \{[^}]*background: var\(--surface\)/)
    expect(css).toMatch(/\.resource-section \{[^}]*background: var\(--surface\)/)
  })

  it('uses tabular numerals for numeric data', () => {
    expect(css).toContain('font-variant-numeric: tabular-nums;')
  })

  it('gives the login page an ambient identity backdrop', () => {
    expect(css).toMatch(/\.login-page \{[^}]*radial-gradient/)
  })

  it('adds press feedback without hover-scale transforms', () => {
    expect(css).toMatch(/button:active \{[^}]*transform: translateY\(1px\)/)
    expect(css).not.toMatch(/:hover[^}]*transform:\s*scale/)
  })

  it('styles accessible dark-theme primary buttons via on-primary text color', () => {
    expect(css).toMatch(/:root\[data-theme="dark"\] \{[^}]*--on-primary: #0/)
  })
})

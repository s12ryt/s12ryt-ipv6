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
})

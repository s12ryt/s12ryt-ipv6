import { cleanup, render, screen, within } from '@testing-library/react'
import userEvent from '@testing-library/user-event'
import { afterEach, describe, expect, it, vi } from 'vitest'
import { ModalDialog } from './ModalDialog'

afterEach(() => {
  cleanup()
  document.body.classList.remove('modal-open')
})

describe('ModalDialog', () => {
  it('ignores backdrop clicks, traps focus, locks scrolling, and restores focus', async () => {
    const user = userEvent.setup()
    const close = vi.fn()
    const trigger = document.createElement('button')
    trigger.textContent = '開啟設定'
    document.body.append(trigger)
    trigger.focus()

    const rendered = render(
      <ModalDialog title="節點設定" dirty={false} onClose={close}>
        <label>名稱<input data-autofocus /></label>
        <button type="button">儲存</button>
      </ModalDialog>,
    )

    const dialog = screen.getByRole('dialog', { name: '節點設定' })
    expect(dialog).toHaveAttribute('aria-modal', 'true')
    expect(document.body).toHaveClass('modal-open')
    expect(screen.getByLabelText('名稱')).toHaveFocus()

    await user.click(screen.getByTestId('modal-backdrop'))
    expect(close).not.toHaveBeenCalled()

    screen.getByRole('button', { name: '儲存' }).focus()
    await user.tab()
    expect(screen.getByRole('button', { name: '關閉節點設定' })).toHaveFocus()
    await user.tab({ shift: true })
    expect(screen.getByRole('button', { name: '儲存' })).toHaveFocus()

    await user.keyboard('{Escape}')
    expect(close).toHaveBeenCalledTimes(1)
    rendered.unmount()
    expect(document.body).not.toHaveClass('modal-open')
    expect(trigger).toHaveFocus()
    trigger.remove()
  })

  it('requires an explicit nested confirmation before discarding dirty state', async () => {
    const user = userEvent.setup()
    const close = vi.fn()
    render(
      <ModalDialog title="Resolver 設定" dirty onClose={close}>
        <input aria-label="Resolver 名稱" />
      </ModalDialog>,
    )

    const editor = screen.getByRole('dialog', { name: 'Resolver 設定' })
    await user.keyboard('{Escape}')
    const confirmation = screen.getByRole('dialog', { name: '放棄未儲存的變更' })
    expect(close).not.toHaveBeenCalled()
    expect(editor).toHaveAttribute('aria-hidden', 'true')
    expect(editor).toHaveAttribute('inert')
    expect(within(confirmation).getByRole('button', { name: '繼續編輯' })).toHaveFocus()

    await user.click(screen.getAllByTestId('modal-backdrop')[1])
    expect(confirmation).toBeInTheDocument()

    await user.keyboard('{Escape}')
    expect(screen.queryByRole('dialog', { name: '放棄未儲存的變更' })).not.toBeInTheDocument()
    expect(editor).not.toHaveAttribute('aria-hidden')
    expect(editor).not.toHaveAttribute('inert')
    expect(close).not.toHaveBeenCalled()

    await user.click(screen.getByRole('button', { name: '關閉Resolver 設定' }))
    await user.click(screen.getByRole('button', { name: '放棄變更' }))
    expect(close).toHaveBeenCalledTimes(1)
  })

  it('lets footer cancel actions use the same dirty-state close contract', async () => {
    const user = userEvent.setup()
    const close = vi.fn()
    render(
      <ModalDialog
        title="NAT64 設定"
        dirty
        onClose={close}
        footer={(requestClose) => <button type="button" onClick={requestClose}>取消</button>}
      >
        <input aria-label="NAT64 前綴" />
      </ModalDialog>,
    )

    await user.click(screen.getByRole('button', { name: '取消' }))
    expect(screen.getByRole('dialog', { name: '放棄未儲存的變更' })).toBeInTheDocument()
    await user.click(screen.getByRole('button', { name: '繼續編輯' }))
    expect(close).not.toHaveBeenCalled()
    expect(screen.getByLabelText('NAT64 前綴')).toHaveFocus()
  })
})

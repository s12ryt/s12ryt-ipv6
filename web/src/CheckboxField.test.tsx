import { render, screen } from '@testing-library/react'
import userEvent from '@testing-library/user-event'
import { useState } from 'react'
import { describe, expect, it, vi } from 'vitest'
import { CheckboxField } from './CheckboxField'

describe('CheckboxField', () => {
  it('keeps native checkbox semantics while rendering a custom control', async () => {
    const user = userEvent.setup()
    function Fixture() {
      const [checked, setChecked] = useState(false)
      return <CheckboxField checked={checked} onChange={setChecked}>啟用 Resolver</CheckboxField>
    }

    const { container } = render(<Fixture />)
    const checkbox = screen.getByRole('checkbox', { name: '啟用 Resolver' })
    expect(checkbox).not.toBeChecked()
    expect(container.querySelector('.checkbox-control')).toHaveAttribute('aria-hidden', 'true')

    await user.click(screen.getByText('啟用 Resolver'))
    expect(checkbox).toBeChecked()
    checkbox.focus()
    await user.keyboard(' ')
    expect(checkbox).not.toBeChecked()
  })

  it('does not emit changes while disabled', async () => {
    const user = userEvent.setup()
    const change = vi.fn()
    render(<CheckboxField checked disabled onChange={change}>鎖定設定</CheckboxField>)

    const checkbox = screen.getByRole('checkbox', { name: '鎖定設定' })
    expect(checkbox).toBeDisabled()
    await user.click(screen.getByText('鎖定設定'))
    expect(change).not.toHaveBeenCalled()
  })
})

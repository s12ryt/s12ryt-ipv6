import { afterEach, describe, expect, it, vi } from 'vitest'
import { copyText } from './clipboard'

afterEach(() => {
  vi.restoreAllMocks()
})

describe('copyText', () => {
  it('uses the Clipboard API when it is available', async () => {
    const writeText = vi.fn().mockResolvedValue(undefined)
    const execCommand = vi.fn()

    await expect(copyText('value', { clipboard: { writeText }, document, execCommand })).resolves.toBe('clipboard')
    expect(writeText).toHaveBeenCalledWith('value')
    expect(execCommand).not.toHaveBeenCalled()
  })

  it('falls back to a temporary selectable textarea when Clipboard API fails', async () => {
    const writeText = vi.fn().mockRejectedValue(new Error('insecure context'))
    const execCommand = vi.fn().mockReturnValue(true)

    await expect(copyText('fallback value', { clipboard: { writeText }, document, execCommand })).resolves.toBe('fallback')
    expect(execCommand).toHaveBeenCalledWith('copy')
    expect(document.querySelector('textarea[data-clipboard-fallback]')).not.toBeInTheDocument()
  })

  it('rejects when neither automatic copy mechanism succeeds', async () => {
    const execCommand = vi.fn().mockReturnValue(false)

    await expect(copyText('manual value', { document, execCommand })).rejects.toThrow('瀏覽器禁止自動複製')
    expect(document.querySelector('textarea[data-clipboard-fallback]')).not.toBeInTheDocument()
  })
})

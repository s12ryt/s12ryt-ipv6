export type CopyMethod = 'clipboard' | 'fallback'

interface ClipboardWriter {
  writeText(value: string): Promise<void>
}

interface CopyEnvironment {
  clipboard?: ClipboardWriter
  document?: Document
  execCommand?: (command: string) => boolean
}

export async function copyText(value: string, environment: CopyEnvironment = {}): Promise<CopyMethod> {
  if (!value) throw new Error('沒有可複製的內容')
  const clipboard = environment.clipboard ?? (typeof navigator === 'undefined' ? undefined : navigator.clipboard)
  if (clipboard?.writeText) {
    try {
      await clipboard.writeText(value)
      return 'clipboard'
    } catch {
      // Public HTTP and browser permissions commonly reject the modern API.
    }
  }

  const targetDocument = environment.document ?? (typeof document === 'undefined' ? undefined : document)
  if (!targetDocument?.body) throw new Error('瀏覽器禁止自動複製')
  const textarea = targetDocument.createElement('textarea')
  textarea.dataset.clipboardFallback = 'true'
  textarea.value = value
  textarea.readOnly = true
  textarea.style.position = 'fixed'
  textarea.style.left = '-10000px'
  textarea.style.opacity = '0'
  targetDocument.body.appendChild(textarea)
  textarea.focus()
  textarea.select()

  try {
    const execute = environment.execCommand ?? targetDocument.execCommand?.bind(targetDocument)
    if (execute?.('copy')) return 'fallback'
  } catch {
    // The manual copy dialog is handled by the caller.
  } finally {
    textarea.remove()
  }
  throw new Error('瀏覽器禁止自動複製')
}

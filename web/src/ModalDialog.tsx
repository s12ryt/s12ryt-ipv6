import { ReactNode, useCallback, useEffect, useId, useRef, useState } from 'react'
import { createPortal } from 'react-dom'
import { X } from 'lucide-react'

interface ModalDialogProps {
  title: string
  dirty?: boolean
  onClose: () => void
  children: ReactNode
  footer?: (requestClose: () => void) => ReactNode
  size?: 'medium' | 'large' | 'wide'
}

let bodyLockCount = 0

export function ModalDialog({
  title,
  dirty = false,
  onClose,
  children,
  footer,
  size = 'large',
}: ModalDialogProps) {
  const titleID = useId()
  const confirmationTitleID = useId()
  const panel = useRef<HTMLElement | null>(null)
  const confirmation = useRef<HTMLElement | null>(null)
  const previousFocus = useRef<HTMLElement | null>(null)
  const [confirmDiscard, setConfirmDiscard] = useState(false)

  const focusForm = useCallback(() => {
    focusFirstControl(panel.current)
  }, [])

  const continueEditing = useCallback(() => {
    setConfirmDiscard(false)
    queueMicrotask(focusForm)
  }, [focusForm])

  const requestClose = useCallback(() => {
    if (dirty) {
      setConfirmDiscard(true)
      return
    }
    onClose()
  }, [dirty, onClose])

  useEffect(() => {
    previousFocus.current = document.activeElement instanceof HTMLElement ? document.activeElement : null
    bodyLockCount += 1
    document.body.classList.add('modal-open')
    focusForm()

    return () => {
      bodyLockCount -= 1
      if (bodyLockCount === 0) document.body.classList.remove('modal-open')
      if (previousFocus.current?.isConnected) previousFocus.current.focus()
    }
  }, [focusForm])

  useEffect(() => {
    if (confirmDiscard) focusFirstControl(confirmation.current, '[data-confirm-default]')
  }, [confirmDiscard])

  useEffect(() => {
    const handleKeyDown = (event: KeyboardEvent) => {
      if (event.key === 'Escape') {
        event.preventDefault()
        event.stopPropagation()
        if (confirmDiscard) continueEditing()
        else requestClose()
        return
      }
      if (event.key !== 'Tab') return
      const activePanel = confirmDiscard ? confirmation.current : panel.current
      if (!activePanel) return
      const focusable = focusableElements(activePanel)
      if (focusable.length === 0) {
        event.preventDefault()
        activePanel.focus()
        return
      }
      const first = focusable[0]
      const last = focusable[focusable.length - 1]
      if (event.shiftKey && document.activeElement === first) {
        event.preventDefault()
        last.focus()
      } else if (!event.shiftKey && document.activeElement === last) {
        event.preventDefault()
        first.focus()
      }
    }

    document.addEventListener('keydown', handleKeyDown, true)
    return () => document.removeEventListener('keydown', handleKeyDown, true)
  }, [confirmDiscard, continueEditing, requestClose])

  return createPortal(
    <>
      <div className="modal-layer" data-testid="modal-backdrop">
        <section
          ref={panel}
          className={`modal-dialog modal-${size}`}
          role="dialog"
          aria-modal="true"
          aria-labelledby={titleID}
          aria-hidden={confirmDiscard || undefined}
          inert={confirmDiscard || undefined}
          tabIndex={-1}
        >
          <header className="modal-header">
            <h2 id={titleID}>{title}</h2>
            <button className="icon-button" type="button" title={`關閉${title}`} aria-label={`關閉${title}`} onClick={requestClose}>
              <X size={17} aria-hidden="true" />
            </button>
          </header>
          <div className="modal-body">{children}</div>
          {footer && <footer className="modal-footer">{footer(requestClose)}</footer>}
        </section>
      </div>
      {confirmDiscard && (
        <div className="modal-layer modal-confirm-layer" data-testid="modal-backdrop">
          <section
            ref={confirmation}
            className="modal-dialog modal-confirm"
            role="dialog"
            aria-modal="true"
            aria-labelledby={confirmationTitleID}
            tabIndex={-1}
          >
            <header className="modal-header"><h2 id={confirmationTitleID}>放棄未儲存的變更</h2></header>
            <div className="modal-body"><p>目前輸入尚未儲存。放棄後無法復原。</p></div>
            <footer className="modal-footer">
              <button data-confirm-default className="secondary-button" type="button" onClick={continueEditing}>繼續編輯</button>
              <button className="danger-button" type="button" onClick={onClose}>放棄變更</button>
            </footer>
          </section>
        </div>
      )}
    </>,
    document.body,
  )
}

function focusFirstControl(container: HTMLElement | null, preferred = '[data-autofocus]') {
  if (!container) return
  const target = container.querySelector<HTMLElement>(preferred)
    ?? container.querySelector<HTMLElement>('input:not([disabled]), select:not([disabled]), textarea:not([disabled])')
    ?? container.querySelector<HTMLElement>('button:not([disabled]), [href], [tabindex]:not([tabindex="-1"])')
    ?? container
  target.focus()
}

function focusableElements(container: HTMLElement) {
  return Array.from(container.querySelectorAll<HTMLElement>(
    'button:not([disabled]), input:not([disabled]), select:not([disabled]), textarea:not([disabled]), [href], [tabindex]:not([tabindex="-1"])',
  )).filter((element) => element.getAttribute('aria-hidden') !== 'true')
}

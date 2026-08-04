import { ReactNode } from 'react'
import { Check } from 'lucide-react'

interface CheckboxFieldProps {
  checked: boolean
  onChange: (checked: boolean) => void
  children: ReactNode
  disabled?: boolean
  className?: string
}

export function CheckboxField({ checked, onChange, children, disabled = false, className = '' }: CheckboxFieldProps) {
  return (
    <label className={`custom-checkbox${className ? ` ${className}` : ''}`}>
      <input
        type="checkbox"
        checked={checked}
        disabled={disabled}
        onChange={(event) => onChange(event.target.checked)}
      />
      <span className="checkbox-control" aria-hidden="true"><Check size={13} /></span>
      <span className="checkbox-label">{children}</span>
    </label>
  )
}

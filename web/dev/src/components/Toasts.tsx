import { createContext, useCallback, useContext, useMemo, useRef, useState, type ReactNode } from 'react'

type Tone = 'info' | 'success' | 'error'

interface ToastItem {
  id: number
  message: string
  tone: Tone
}

interface ToastApi {
  push: (message: string, tone?: Tone) => void
}

const ToastContext = createContext<ToastApi | null>(null)

export function ToastProvider({ children }: { children: ReactNode }) {
  const [items, setItems] = useState<ToastItem[]>([])
  const counter = useRef(0)

  const push = useCallback((message: string, tone: Tone = 'info') => {
    counter.current += 1
    const id = counter.current
    setItems((current) => [...current, { id, message, tone }])
    window.setTimeout(() => {
      setItems((current) => current.filter((item) => item.id !== id))
    }, 4200)
  }, [])

  const value = useMemo(() => ({ push }), [push])

  return (
    <ToastContext.Provider value={value}>
      {children}
      <div className="toasts" role="status" aria-live="polite">
        {items.map((item) => (
          <div key={item.id} className={`toast ${item.tone}`}>
            <span className={`dot ${item.tone === 'error' ? 'red' : item.tone === 'success' ? '' : 'yellow'}`} />
            <span>{item.message}</span>
          </div>
        ))}
      </div>
    </ToastContext.Provider>
  )
}

export function useToast(): ToastApi {
  const context = useContext(ToastContext)
  if (!context) throw new Error('useToast must be used inside ToastProvider')
  return context
}

import { useQueryClient } from '@tanstack/react-query'
import { useNavigate } from '@tanstack/react-router'
import { useEffect, useRef } from 'react'
import type { Page } from './navigation'

// The keyboard layer. Every shortcut is one key (or `g` then a page's letter
// from navigation.ts) and does nothing while the user is typing.

export const shortcutHelp: { keys: string[]; label: string }[] = [
  { keys: ['Ctrl', 'K'], label: 'Search pages and actions' },
  { keys: ['g', '…'], label: 'Go to a page (letters next to each page)' },
  { keys: ['/'], label: 'Focus the search or filter box' },
  { keys: ['r'], label: 'Refresh the data on screen' },
  { keys: ['['], label: 'Collapse or expand the sidebar' },
  { keys: ['?'], label: 'Show this sheet' },
  { keys: ['Esc'], label: 'Close the top dialog, drawer or menu' },
]

/** True while focus is in a text field, select, editor or the palette. */
export function isTyping(target: EventTarget | null): boolean {
  if (!(target instanceof HTMLElement)) return false
  if (target.isContentEditable) return true
  return target.closest('input, textarea, select, [contenteditable="true"], [cmdk-root]') !== null
}

/** Focuses the screen's first search or filter field. */
function focusSearch(): boolean {
  const field = document.querySelector<HTMLElement>(
    '.page input:not([type=checkbox]):not([type=radio]):not([type=hidden]):not(:disabled), .page textarea:not(:disabled)',
  )
  if (!field) return false
  field.focus()
  if (field instanceof HTMLInputElement) field.select()
  return true
}

export function useShortcuts({
  pages,
  onPalette,
  onHelp,
  onToggleSidebar,
}: {
  pages: Page[]
  onPalette: () => void
  onHelp: () => void
  onToggleSidebar: () => void
}) {
  const navigate = useNavigate()
  const queryClient = useQueryClient()
  const pendingG = useRef<number | null>(null)
  const latest = useRef({ pages, onPalette, onHelp, onToggleSidebar })
  latest.current = { pages, onPalette, onHelp, onToggleSidebar }

  useEffect(() => {
    const clearG = () => {
      if (pendingG.current !== null) window.clearTimeout(pendingG.current)
      pendingG.current = null
    }
    const handler = (event: KeyboardEvent) => {
      const { pages, onPalette, onHelp, onToggleSidebar } = latest.current
      if ((event.metaKey || event.ctrlKey) && event.key.toLowerCase() === 'k') {
        event.preventDefault()
        onPalette()
        return
      }
      if (event.metaKey || event.ctrlKey || event.altKey || event.defaultPrevented || isTyping(event.target)) return
      if (document.querySelector('.overlay, .command-overlay')) return

      if (pendingG.current !== null) {
        clearG()
        const page = pages.find((item) => item.key === event.key.toLowerCase())
        if (page) {
          event.preventDefault()
          void navigate({ to: page.to })
        }
        return
      }
      // Letters match in either case: the keys are shown as capitals.
      switch (event.key.length === 1 ? event.key.toLowerCase() : event.key) {
        case 'g':
          pendingG.current = window.setTimeout(clearG, 1500)
          return
        case '?':
          event.preventDefault()
          onHelp()
          return
        case '[':
          event.preventDefault()
          onToggleSidebar()
          return
        case '/':
          if (focusSearch()) event.preventDefault()
          return
        case 'r':
          event.preventDefault()
          void queryClient.refetchQueries({ type: 'active' })
          return
      }
    }
    window.addEventListener('keydown', handler)
    return () => {
      window.removeEventListener('keydown', handler)
      clearG()
    }
  }, [navigate, queryClient])
}

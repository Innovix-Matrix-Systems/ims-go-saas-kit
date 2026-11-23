import type { Page } from './navigation'
import { shortcutHelp } from './shortcuts'
import { Button, Kbd, Modal } from './ui'

/** The `?` sheet: every shortcut, and the `g` letter of every page. */
export function ShortcutsSheet({ open, pages, onClose }: { open: boolean; pages: Page[]; onClose: () => void }) {
  return (
    <Modal
      open={open}
      title="Keyboard shortcuts"
      description="Shortcuts do nothing while you are typing in a field."
      onClose={onClose}
      size="lg"
      actions={
        <Button variant="ghost" onClick={onClose}>
          Close
        </Button>
      }
    >
      <div className="shortcut-columns">
        <dl className="shortcut-list">
          {shortcutHelp.map((item) => (
            <div key={item.label} className="shortcut-row">
              <dt>
                {item.keys.map((key) => (
                  <Kbd key={key}>{key}</Kbd>
                ))}
              </dt>
              <dd>{item.label}</dd>
            </div>
          ))}
        </dl>
        <dl className="shortcut-list">
          {pages
            .filter((page) => page.key)
            .map((page) => (
              <div key={page.to} className="shortcut-row">
                <dt>
                  <Kbd>g</Kbd>
                  <Kbd>{page.key}</Kbd>
                </dt>
                <dd>{page.label}</dd>
              </div>
            ))}
        </dl>
      </div>
    </Modal>
  )
}

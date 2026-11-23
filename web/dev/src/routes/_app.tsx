import { Link, createFileRoute, redirect, type ErrorComponentProps } from '@tanstack/react-router'
import { getToken } from '../api/client'
import { AppShell } from '../components/AppShell'
import { Button, CodeBlock, Icon } from '../components/ui'

export const Route = createFileRoute('/_app')({
  beforeLoad: () => {
    if (!getToken()) {
      throw redirect({ to: '/login' })
    }
  },
  component: AppShell,
  errorComponent: AppErrorBoundary,
})

function AppErrorBoundary({ error, reset }: ErrorComponentProps) {
  const message = error instanceof Error ? error.message : 'Unexpected rendering error.'
  return (
    <div className="error-wrap">
      <div className="error-card card">
        <div className="flex align-start g-3">
          <div className="error-icon">
            <Icon name="alert" />
          </div>
          <div>
            <h1 className="login-title mb-4">Something went wrong</h1>
            <p className="login-sub mb-12">
              This screen failed to render. The API is usually fine: the error stays on this page,
              so retrying or going back to the overview is safe.
            </p>
          </div>
        </div>

        <div className="error-code">{message}</div>

        <details className="mt-16">
          <summary className="small muted pointer">Technical details</summary>
          <div className="mt-8"><CodeBlock code={error instanceof Error ? error.stack ?? message : String(error)} label="Stack trace" /></div>
        </details>

        <div className="modal-actions">
          <Link to="/" className="btn ghost">
            <Icon name="overview" /> Back to overview
          </Link>
          <Button variant="primary" icon="refresh" onClick={reset}>
            Retry
          </Button>
        </div>
      </div>
    </div>
  )
}


import { QueryClientProvider } from '@tanstack/react-query'
import { RouterProvider, createRouter } from '@tanstack/react-router'
import { StrictMode } from 'react'
import { createRoot } from 'react-dom/client'
import { PageSkeleton } from './components/Skeleton'
import { ToastProvider } from './components/Toasts'
import { startI18n } from './i18n'
import { currenciesQuery } from './money'
import { queryClient } from './query'
import { routeTree } from './routeTree.gen'
import './styles.css'

const router = createRouter({
  routeTree,
  defaultPreload: 'intent',
  scrollRestoration: true,
  // While a page's code or loader is on its way, show its outline instead
  // of nothing (after a short delay, so fast loads do not flash it).
  defaultPendingComponent: PageSkeleton,
  defaultPendingMs: 150,
  defaultPendingMinMs: 300,
})

declare module '@tanstack/react-router' {
  interface Register {
    router: typeof router
  }
}

// The starting language's text arrives before the first render, so the screen
// never shows one language and then another; the currencies load alongside, so
// prices never show in a placeholder format first.
void Promise.allSettled([startI18n(), queryClient.prefetchQuery(currenciesQuery)]).finally(() =>
  createRoot(document.getElementById('root')!).render(
    <StrictMode>
      <QueryClientProvider client={queryClient}>
        <ToastProvider>
          <RouterProvider router={router} />
        </ToastProvider>
      </QueryClientProvider>
    </StrictMode>,
  ),
)

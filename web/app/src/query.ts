import { QueryClient } from '@tanstack/react-query'
import { ApiError } from './api/client'

export const queryClient = new QueryClient({
  defaultOptions: {
    queries: {
      // Retry once for network/5xx failures only: a 4xx will not change on
      // retry, and retrying a 429 makes it worse.
      retry: (failureCount, error) =>
        failureCount < 1 && !(error instanceof ApiError && error.status >= 400 && error.status < 500),
      refetchOnWindowFocus: false,
      staleTime: 5_000,
    },
  },
})

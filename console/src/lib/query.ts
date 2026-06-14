import { QueryClient } from '@tanstack/react-query'

export const queryClient = new QueryClient({
  defaultOptions: {
    queries: {
      staleTime: 30_000,
      retry: 1,
      refetchOnWindowFocus: false,
    },
  },
})

export const qk = {
  me: ['me'] as const,
  tenants: ['tenants'] as const,
  bundle: (id: string) => ['bundle', id] as const,
}

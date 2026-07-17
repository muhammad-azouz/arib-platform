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
  stats: ['stats'] as const,
  audit: ['audit'] as const,
  clients: (q: string) => ['clients', q] as const,
  client: (id: string) => ['client', id] as const,
  bills: (tenantId: string) => ['bills', tenantId] as const,
}

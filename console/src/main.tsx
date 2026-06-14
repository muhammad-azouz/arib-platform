import { StrictMode } from 'react'
import { createRoot } from 'react-dom/client'
import { QueryClientProvider } from '@tanstack/react-query'
import { SolarProvider } from '@solar-icons/react'
import './index.css'
import App from './App.tsx'
import { AuthProvider } from '@/lib/auth'
import { queryClient } from '@/lib/query'
import { Toaster } from '@/components/ui/sonner'

createRoot(document.getElementById('root')!).render(
  <StrictMode>
    {/* One place sets the Solar "Line Duotone" weight for every icon. */}
    <SolarProvider value={{ weight: 'LineDuotone', size: 22 }}>
      <QueryClientProvider client={queryClient}>
        <AuthProvider>
          <App />
          <Toaster />
        </AuthProvider>
      </QueryClientProvider>
    </SolarProvider>
  </StrictMode>,
)

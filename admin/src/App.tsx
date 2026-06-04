import { BrowserRouter, Navigate, Route, Routes } from 'react-router-dom'
import { useAuth } from '@/lib/auth'
import { AppShell } from '@/components/AppShell'
import { Login } from '@/pages/Login'
import { Overview } from '@/pages/Overview'
import { Clients } from '@/pages/Clients'
import { ClientDetail } from '@/pages/ClientDetail'
import { Audit } from '@/pages/Audit'

function BootScreen() {
  return (
    <div className="grid min-h-screen place-items-center">
      <div className="flex items-center gap-3 text-sm text-muted-foreground">
        <span className="size-2 animate-ping rounded-full bg-primary" />
        Restoring session…
      </div>
    </div>
  )
}

export default function App() {
  const { status } = useAuth()

  if (status === 'loading') return <BootScreen />

  return (
    <BrowserRouter>
      {status === 'authed' ? (
        <Routes>
          <Route element={<AppShell />}>
            <Route index element={<Overview />} />
            <Route path="clients" element={<Clients />} />
            <Route path="clients/:id" element={<ClientDetail />} />
            <Route path="audit" element={<Audit />} />
          </Route>
          <Route path="*" element={<Navigate to="/" replace />} />
        </Routes>
      ) : (
        <Routes>
          <Route path="*" element={<Login />} />
        </Routes>
      )}
    </BrowserRouter>
  )
}

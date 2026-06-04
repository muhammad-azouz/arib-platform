import {
  createContext,
  useCallback,
  useContext,
  useEffect,
  useMemo,
  useState,
  type ReactNode,
} from 'react'
import { ApiError, authApi, session } from './api'

interface JwtClaims {
  email?: string
  admin?: boolean
  sub?: string
  exp?: number
}

/** Decode a JWT payload without verifying (verification happens server-side). */
function decodeJwt(token: string): JwtClaims | null {
  try {
    const payload = token.split('.')[1]
    const json = atob(payload.replace(/-/g, '+').replace(/_/g, '/'))
    return JSON.parse(decodeURIComponent(escape(json))) as JwtClaims
  } catch {
    return null
  }
}

type Status = 'loading' | 'unauthed' | 'authed'

interface AuthState {
  status: Status
  email: string | null
  requestCode: (email: string) => Promise<void>
  verify: (email: string, code: string) => Promise<void>
  logout: () => Promise<void>
}

const AuthContext = createContext<AuthState | null>(null)

/** Thrown when a verified login is not on the admin allow-list. */
export class NotAuthorizedError extends Error {
  constructor() {
    super('This account is not authorized for the admin console.')
    this.name = 'NotAuthorizedError'
  }
}

export function AuthProvider({ children }: { children: ReactNode }) {
  const [status, setStatus] = useState<Status>('loading')
  const [email, setEmail] = useState<string | null>(null)

  const reset = useCallback(() => {
    session.clear()
    setEmail(null)
    setStatus('unauthed')
  }, [])

  // Cold start: try to revive the session from the stored refresh token.
  useEffect(() => {
    session.setLogoutHandler(reset)
    let cancelled = false
    ;(async () => {
      if (!session.getRefresh()) {
        setStatus('unauthed')
        return
      }
      const ok = await session.refresh()
      const claims = ok ? decodeJwt(session.getAccess() ?? '') : null
      if (cancelled) return
      if (ok && claims?.admin) {
        setEmail(claims.email ?? null)
        setStatus('authed')
      } else {
        reset()
      }
    })()
    return () => {
      cancelled = true
    }
  }, [reset])

  const requestCode = useCallback(async (addr: string) => {
    await authApi.emailStart(addr)
  }, [])

  const verify = useCallback(async (addr: string, code: string) => {
    const s = await authApi.emailVerify(addr, code)
    const claims = decodeJwt(s.access_token)
    if (!claims?.admin) {
      // Not an admin: drop the just-issued tokens, refuse entry.
      session.setAccess(s.access_token)
      session.setRefresh(s.refresh_token)
      await authApi.logout().catch(() => undefined)
      session.clear()
      throw new NotAuthorizedError()
    }
    session.store(s)
    setEmail(claims.email ?? s.email)
    setStatus('authed')
  }, [])

  const logout = useCallback(async () => {
    await authApi.logout().catch(() => undefined)
    reset()
  }, [reset])

  const value = useMemo<AuthState>(
    () => ({ status, email, requestCode, verify, logout }),
    [status, email, requestCode, verify, logout],
  )

  return <AuthContext.Provider value={value}>{children}</AuthContext.Provider>
}

export function useAuth() {
  const ctx = useContext(AuthContext)
  if (!ctx) throw new Error('useAuth must be used within AuthProvider')
  return ctx
}

export function errorMessage(err: unknown): string {
  if (err instanceof NotAuthorizedError) return err.message
  if (err instanceof ApiError) return err.message
  if (err instanceof Error) return err.message
  return 'Something went wrong.'
}

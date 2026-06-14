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
  accountId: string | null
  requestCode: (email: string) => Promise<{ isNew: boolean }>
  verify: (
    email: string,
    code: string,
    firstName?: string,
    lastName?: string,
  ) => Promise<void>
  logout: () => Promise<void>
}

const AuthContext = createContext<AuthState | null>(null)

/*
 * Unlike the internal admin console, the tenant console has no allow-list:
 * every account that verifies an email OTP is a legitimate tenant owner. So
 * there is no `claims.admin` gate here — we accept any valid session.
 */
export function AuthProvider({ children }: { children: ReactNode }) {
  const [status, setStatus] = useState<Status>('loading')
  const [email, setEmail] = useState<string | null>(null)
  const [accountId, setAccountId] = useState<string | null>(null)

  const reset = useCallback(() => {
    session.clear()
    setEmail(null)
    setAccountId(null)
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
      if (ok && claims) {
        setEmail(claims.email ?? null)
        setAccountId(claims.sub ?? null)
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
    const { exists } = await authApi.emailStart(addr)
    return { isNew: !exists }
  }, [])

  const verify = useCallback(
    async (addr: string, code: string, firstName = '', lastName = '') => {
      const s = await authApi.emailVerify(addr, code, firstName, lastName)
      const claims = decodeJwt(s.access_token)
      session.store(s)
      setEmail(claims?.email ?? s.email)
      setAccountId(claims?.sub ?? s.account_id)
      setStatus('authed')
    },
    [],
  )

  const logout = useCallback(async () => {
    await authApi.logout().catch(() => undefined)
    reset()
  }, [reset])

  const value = useMemo<AuthState>(
    () => ({ status, email, accountId, requestCode, verify, logout }),
    [status, email, accountId, requestCode, verify, logout],
  )

  return <AuthContext.Provider value={value}>{children}</AuthContext.Provider>
}

export function useAuth() {
  const ctx = useContext(AuthContext)
  if (!ctx) throw new Error('useAuth must be used within AuthProvider')
  return ctx
}

export function errorMessage(err: unknown): string {
  if (err instanceof ApiError) return err.message
  if (err instanceof Error) return err.message
  return 'حدث خطأ ما.'
}

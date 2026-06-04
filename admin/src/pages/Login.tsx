import { useState } from 'react'
import { ArrowLeft, ArrowRight, KeyRound, Mail, ShieldCheck } from 'lucide-react'
import { toast } from 'sonner'
import { errorMessage, useAuth } from '@/lib/auth'
import { Button } from '@/components/ui/button'
import { Input } from '@/components/ui/input'
import { Label } from '@/components/ui/label'

export function Login() {
  const { requestCode, verify } = useAuth()
  const [step, setStep] = useState<'email' | 'code'>('email')
  const [email, setEmail] = useState('')
  const [code, setCode] = useState('')
  const [busy, setBusy] = useState(false)

  async function sendCode(e: React.FormEvent) {
    e.preventDefault()
    if (!email.includes('@')) return
    setBusy(true)
    try {
      await requestCode(email.trim())
      setStep('code')
      toast.success('Code sent — check your email')
    } catch (err) {
      toast.error(errorMessage(err))
    } finally {
      setBusy(false)
    }
  }

  async function submitCode(e: React.FormEvent) {
    e.preventDefault()
    if (code.trim().length < 4) return
    setBusy(true)
    try {
      await verify(email.trim(), code.trim())
      // On success the AuthProvider flips to 'authed' and the router swaps views.
    } catch (err) {
      toast.error(errorMessage(err))
    } finally {
      setBusy(false)
    }
  }

  return (
    <div className="grid min-h-screen lg:grid-cols-2">
      {/* Left: brand panel */}
      <div className="relative hidden flex-col justify-between overflow-hidden border-r border-border bg-card/30 p-10 lg:flex">
        <div
          className="pointer-events-none absolute inset-0 opacity-[0.18]"
          style={{
            backgroundImage:
              'linear-gradient(var(--border) 1px, transparent 1px), linear-gradient(90deg, var(--border) 1px, transparent 1px)',
            backgroundSize: '32px 32px',
            maskImage:
              'radial-gradient(60% 60% at 30% 30%, black, transparent)',
          }}
        />
        <div className="relative flex items-center gap-3">
          <div className="grid size-10 place-items-center rounded-lg bg-primary text-primary-foreground shadow-[0_0_28px_-4px_rgba(245,165,36,0.8)]">
            <span className="font-display text-lg font-bold">A</span>
          </div>
          <div>
            <div className="font-display text-lg font-semibold leading-none">
              Arib
            </div>
            <div className="text-[11px] uppercase tracking-[0.2em] text-muted-foreground">
              License Control
            </div>
          </div>
        </div>

        <div className="relative max-w-sm">
          <h2 className="font-display text-3xl font-semibold leading-tight">
            The operator console for every license, device, and seat.
          </h2>
          <p className="mt-4 text-sm leading-relaxed text-muted-foreground">
            Issue and suspend licenses, rebind devices, mint offline fallbacks,
            and read the full audit trail — all in one place.
          </p>
        </div>

        <div className="relative flex items-center gap-2 text-xs text-muted-foreground">
          <ShieldCheck className="size-4 text-success" />
          Restricted to authorized operators.
        </div>
      </div>

      {/* Right: auth form */}
      <div className="flex items-center justify-center p-6">
        <div className="w-full max-w-sm animate-rise">
          <div className="mb-8 lg:hidden">
            <div className="font-display text-xl font-semibold">
              Arib · License Control
            </div>
          </div>

          {step === 'email' ? (
            <form onSubmit={sendCode} className="grid gap-5">
              <div>
                <h1 className="font-display text-2xl font-semibold">Sign in</h1>
                <p className="mt-1 text-sm text-muted-foreground">
                  We'll email you a one-time code.
                </p>
              </div>
              <div className="grid gap-1.5">
                <Label htmlFor="email">Admin email</Label>
                <div className="relative">
                  <Mail className="pointer-events-none absolute left-3 top-1/2 size-4 -translate-y-1/2 text-muted-foreground" />
                  <Input
                    id="email"
                    type="email"
                    autoFocus
                    autoComplete="email"
                    placeholder="you@arib.app"
                    className="pl-9"
                    value={email}
                    onChange={(e) => setEmail(e.target.value)}
                  />
                </div>
              </div>
              <Button type="submit" disabled={busy || !email.includes('@')}>
                {busy ? 'Sending…' : 'Send code'}
                <ArrowRight className="size-4" />
              </Button>
            </form>
          ) : (
            <form onSubmit={submitCode} className="grid gap-5">
              <div>
                <h1 className="font-display text-2xl font-semibold">
                  Enter code
                </h1>
                <p className="mt-1 text-sm text-muted-foreground">
                  Sent to{' '}
                  <span className="font-mono text-foreground/80">{email}</span>.
                </p>
              </div>
              <div className="grid gap-1.5">
                <Label htmlFor="code">Verification code</Label>
                <div className="relative">
                  <KeyRound className="pointer-events-none absolute left-3 top-1/2 size-4 -translate-y-1/2 text-muted-foreground" />
                  <Input
                    id="code"
                    inputMode="numeric"
                    autoFocus
                    placeholder="000000"
                    className="pl-9 font-mono tracking-[0.4em]"
                    value={code}
                    onChange={(e) => setCode(e.target.value)}
                  />
                </div>
              </div>
              <Button type="submit" disabled={busy || code.trim().length < 4}>
                {busy ? 'Verifying…' : 'Verify & enter'}
              </Button>
              <button
                type="button"
                onClick={() => setStep('email')}
                className="inline-flex items-center justify-center gap-1.5 text-xs text-muted-foreground hover:text-foreground"
              >
                <ArrowLeft className="size-3" />
                Use a different email
              </button>
            </form>
          )}
        </div>
      </div>
    </div>
  )
}

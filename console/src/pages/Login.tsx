import { useState } from 'react'
import { useForm } from 'react-hook-form'
import { zodResolver } from '@hookform/resolvers/zod'
import { z } from 'zod'
import { toast } from 'sonner'
import { useAuth, errorMessage } from '@/lib/auth'
import { Brand } from '@/components/Brand'
import { EmailIcon, ArrowLeading } from '@/components/icon'
import { Button } from '@/components/ui/button'
import { Input } from '@/components/ui/input'
import { Label } from '@/components/ui/label'

const emailSchema = z.object({
  email: z.string().min(1, 'البريد الإلكتروني مطلوب').email('بريد إلكتروني غير صالح'),
})
type EmailForm = z.infer<typeof emailSchema>

const codeSchema = z.object({
  code: z.string().regex(/^\d{6}$/, 'الرمز مكوّن من ٦ أرقام'),
  firstName: z.string().optional(),
  lastName: z.string().optional(),
})
type CodeForm = z.infer<typeof codeSchema>

// Google / Facebook are wired server-side only for the desktop loopback flow
// (handleOAuthStart requires a 127.0.0.1 `cb`). A browser SPA can't use that,
// so these are shown but disabled until a web-callback path is added to the API.
const OAUTH = [
  { id: 'google', label: 'المتابعة عبر Google' },
  { id: 'facebook', label: 'المتابعة عبر Facebook' },
]

export function Login() {
  const { requestCode, verify } = useAuth()
  const [email, setEmail] = useState<string | null>(null)
  // True only for a first-time signup, so the name fields show just once.
  const [isNew, setIsNew] = useState(false)

  const emailForm = useForm<EmailForm>({
    resolver: zodResolver(emailSchema),
    defaultValues: { email: '' },
  })
  const codeForm = useForm<CodeForm>({
    resolver: zodResolver(codeSchema),
    defaultValues: { code: '', firstName: '', lastName: '' },
  })

  const onEmail = emailForm.handleSubmit(async ({ email }) => {
    try {
      const { isNew } = await requestCode(email)
      setIsNew(isNew)
      setEmail(email)
      toast.success('أرسلنا رمز الدخول إلى بريدك')
    } catch (err) {
      toast.error(errorMessage(err))
    }
  })

  const onCode = codeForm.handleSubmit(async ({ code, firstName, lastName }) => {
    if (!email) return
    try {
      await verify(email, code, firstName, lastName)
      // success → AuthProvider flips status to "authed"; the router takes over.
    } catch (err) {
      toast.error(errorMessage(err))
    }
  })

  return (
    <div className="grid min-h-screen place-items-center px-4 py-10">
      <div className="w-full max-w-sm animate-rise">
        <div className="mb-8 flex justify-center">
          <Brand subtitle="لوحة تحكم التاجر" />
        </div>

        <div className="rounded-2xl border border-border bg-card p-6 shadow-sm sm:p-8">
          {!email ? (
            <>
              <h1 className="font-display text-xl font-extrabold">تسجيل الدخول</h1>
              <p className="mt-1 text-sm text-muted-foreground">
                أدخل بريدك الإلكتروني وسنرسل لك رمز دخول لمرة واحدة.
              </p>

              <form onSubmit={onEmail} className="mt-6 space-y-4" noValidate>
                <div className="space-y-1.5">
                  <Label htmlFor="email">البريد الإلكتروني</Label>
                  <Input
                    id="email"
                    type="email"
                    inputMode="email"
                    autoComplete="email"
                    dir="ltr"
                    placeholder="you@example.com"
                    className="text-start"
                    {...emailForm.register('email')}
                  />
                  {emailForm.formState.errors.email && (
                    <p className="text-xs text-danger">
                      {emailForm.formState.errors.email.message}
                    </p>
                  )}
                </div>
                <Button
                  type="submit"
                  className="w-full"
                  disabled={emailForm.formState.isSubmitting}
                >
                  <EmailIcon className="size-4" />
                  إرسال رمز الدخول
                </Button>
              </form>

              <div className="my-5 flex items-center gap-3 text-xs text-muted-foreground/70">
                <span className="h-px flex-1 bg-border" />
                أو
                <span className="h-px flex-1 bg-border" />
              </div>

              <div className="space-y-2">
                {OAUTH.map((p) => (
                  <Button
                    key={p.id}
                    type="button"
                    variant="outline"
                    className="w-full"
                    disabled
                    title="غير متاح حالياً في النسخة الإلكترونية"
                  >
                    {p.label}
                  </Button>
                ))}
                <p className="pt-1 text-center text-[11px] text-muted-foreground/70">
                  تسجيل الدخول عبر مزوّدي الهوية غير متاح حالياً على الويب.
                </p>
              </div>
            </>
          ) : (
            <>
              <h1 className="font-display text-xl font-extrabold">أدخل رمز الدخول</h1>
              <p className="mt-1 text-sm text-muted-foreground">
                أرسلنا رمزًا مكوّنًا من ٦ أرقام إلى{' '}
                <span className="dir-ltr font-mono text-foreground">{email}</span>
              </p>

              <form onSubmit={onCode} className="mt-6 space-y-4" noValidate>
                <div className="space-y-1.5">
                  <Label htmlFor="code">رمز الدخول</Label>
                  <Input
                    id="code"
                    inputMode="numeric"
                    autoComplete="one-time-code"
                    dir="ltr"
                    maxLength={6}
                    placeholder="••••••"
                    className="text-center font-mono text-lg tracking-[0.5em]"
                    autoFocus
                    {...codeForm.register('code')}
                  />
                  {codeForm.formState.errors.code && (
                    <p className="text-xs text-danger">
                      {codeForm.formState.errors.code.message}
                    </p>
                  )}
                </div>

                {/* Shown only for a first-time signup; existing accounts skip it. */}
                {isNew && (
                  <div className="grid grid-cols-2 gap-3">
                    <div className="space-y-1.5">
                      <Label htmlFor="firstName">الاسم الأول</Label>
                      <Input id="firstName" {...codeForm.register('firstName')} />
                    </div>
                    <div className="space-y-1.5">
                      <Label htmlFor="lastName">اسم العائلة</Label>
                      <Input id="lastName" {...codeForm.register('lastName')} />
                    </div>
                  </div>
                )}

                <Button
                  type="submit"
                  className="w-full"
                  disabled={codeForm.formState.isSubmitting}
                >
                  <ArrowLeading className="size-4" />
                  تأكيد ودخول
                </Button>
                <button
                  type="button"
                  onClick={() => {
                    setEmail(null)
                    setIsNew(false)
                    codeForm.reset()
                  }}
                  className="w-full text-center text-xs text-muted-foreground transition-colors hover:text-foreground"
                >
                  تغيير البريد الإلكتروني
                </button>
              </form>
            </>
          )}
        </div>

        <p className="mt-6 text-center text-[11px] text-muted-foreground/60">
          أريب · لوحة تحكم التاجر
        </p>
      </div>
    </div>
  )
}

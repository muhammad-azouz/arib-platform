import { Toaster as Sonner, type ToasterProps } from 'sonner'

function Toaster(props: ToasterProps) {
  return (
    <Sonner
      theme="light"
      position="bottom-left"
      dir="rtl"
      toastOptions={{
        style: {
          background: 'var(--popover)',
          color: 'var(--popover-foreground)',
          border: '1px solid var(--border)',
          fontFamily: 'var(--font-sans)',
        },
      }}
      {...props}
    />
  )
}

export { Toaster }

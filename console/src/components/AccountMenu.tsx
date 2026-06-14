import { useAuth } from '@/lib/auth'
import { LogoutIcon } from '@/components/icon'
import { Button } from '@/components/ui/button'
import {
  DropdownMenu,
  DropdownMenuContent,
  DropdownMenuItem,
  DropdownMenuLabel,
  DropdownMenuSeparator,
  DropdownMenuTrigger,
} from '@/components/ui/dropdown-menu'

/** Account avatar + email with a logout action. Shared by every top bar. */
export function AccountMenu() {
  const { email, logout } = useAuth()
  const initials = (email ?? '؟').slice(0, 2).toUpperCase()

  return (
    <DropdownMenu>
      <DropdownMenuTrigger asChild>
        <Button variant="ghost" className="h-9 gap-2 px-2">
          <span className="grid size-7 place-items-center rounded-full bg-secondary text-xs font-semibold text-secondary-foreground">
            {initials}
          </span>
          <span className="dir-ltr hidden max-w-[14rem] truncate text-sm text-muted-foreground sm:inline">
            {email}
          </span>
        </Button>
      </DropdownMenuTrigger>
      <DropdownMenuContent align="end" className="w-56">
        <DropdownMenuLabel>الحساب</DropdownMenuLabel>
        <div className="dir-ltr truncate px-2 pb-1.5 text-start font-mono text-xs text-foreground/80">
          {email}
        </div>
        <DropdownMenuSeparator />
        <DropdownMenuItem variant="destructive" onSelect={() => void logout()}>
          <LogoutIcon className="size-4" />
          تسجيل الخروج
        </DropdownMenuItem>
      </DropdownMenuContent>
    </DropdownMenu>
  )
}

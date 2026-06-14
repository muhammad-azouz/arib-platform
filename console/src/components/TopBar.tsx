import { Link } from 'react-router-dom'
import { Brand } from '@/components/Brand'
import { AccountMenu } from '@/components/AccountMenu'

/** Account-level page header: brand lockup on the lead edge, account menu on the trail. */
export function TopBar({ subtitle }: { subtitle?: string }) {
  return (
    <header className="flex h-16 items-center justify-between px-5 sm:px-8">
      <Link to="/" aria-label="الرئيسية">
        <Brand subtitle={subtitle} />
      </Link>
      <AccountMenu />
    </header>
  )
}

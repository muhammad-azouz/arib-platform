/*
 * Single icon surface for the console. Everything pulls Solar icons through
 * here under app-semantic names, so swapping an icon is a one-line change and
 * the visual vocabulary stays consistent. The "Line Duotone" weight is set
 * once, app-wide, by <SolarProvider> in main.tsx — individual call sites only
 * pick the glyph and the size (Tailwind `className="size-5"`).
 */
import type { ComponentType } from 'react'
import type { IconProps } from '@solar-icons/react'
import {
  HomeSmile,
  Buildings,
  Buildings2,
  Shop,
  ShopMinimalistic,
  UsersGroupRounded,
  UserCircle,
  Logout2,
  Settings,
  Widget,
  AddCircle,
  PenNewSquare,
  TrashBinMinimalistic,
  CheckCircle,
  DangerTriangle,
  InfoCircle,
  QuestionCircle,
  CloseCircle,
  Copy,
  Letter,
  Phone,
  Hashtag,
  MapPoint,
  KeyMinimalistic,
  Database,
  ShieldKeyhole,
  MonitorSmartphone,
  Monitor,
  Smartphone,
  Refresh,
  AltArrowLeft,
  AltArrowRight,
  MenuDots,
  Bill,
  Wallet,
  Download,
  TagPrice,
  BoxMinimalistic,
  ChartSquare,
} from '@solar-icons/react'

/** Shared type for any icon component in this module (Solar forward-ref svg). */
export type IconComponent = ComponentType<IconProps>

// Brand / navigation
export const HomeIcon = HomeSmile
export const TenantIcon = Buildings
export const CompanyIcon = Buildings2
export const BranchIcon = Shop
export const BranchAltIcon = ShopMinimalistic
export const UsersIcon = UsersGroupRounded
export const AccountIcon = UserCircle
export const LogoutIcon = Logout2
export const SettingsIcon = Settings
export const DashboardIcon = Widget
export const CatalogIcon = TagPrice
export const InventoryIcon = BoxMinimalistic
export const ReportsIcon = ChartSquare

// Actions
export const AddIcon = AddCircle
export const EditIcon = PenNewSquare
export const DeleteIcon = TrashBinMinimalistic
export const CopyIcon = Copy
export const RefreshIcon = Refresh
export const MenuIcon = MenuDots

// Status / feedback
export const SuccessIcon = CheckCircle
export const DangerIcon = DangerTriangle
export const InfoIcon = InfoCircle
export const HelpIcon = QuestionCircle
export const CloseIcon = CloseCircle

// Domain
export const EmailIcon = Letter
export const PhoneIcon = Phone
export const TaxIcon = Hashtag
export const AddressIcon = MapPoint
export const SyncTokenIcon = KeyMinimalistic
export const DatabaseIcon = Database
export const SecurityIcon = ShieldKeyhole
export const DeviceIcon = MonitorSmartphone
export const DesktopIcon = Monitor
export const MobileIcon = Smartphone
export const BillingIcon = Bill
export const WalletIcon = Wallet
export const DownloadIcon = Download

// Directional — in RTL the "forward / next" arrow points left. Components that
// mean a literal physical direction import these explicitly.
export const ArrowLeading = AltArrowLeft
export const ArrowTrailing = AltArrowRight

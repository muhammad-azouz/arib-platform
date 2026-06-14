import { clsx, type ClassValue } from 'clsx'
import { twMerge } from 'tailwind-merge'

export function cn(...inputs: ClassValue[]) {
  return twMerge(clsx(inputs))
}

/**
 * Lowercase GUID generator for client-minted Company/Branch ids. The cloud
 * accepts a client-supplied `id` (mint-or-adopt rule) and these flow into the
 * tenant SQL DB as `uniqueidentifier` FKs, so the lowercase string form matches
 * the GUIDv7 PK convention.
 */
export function newGuid(): string {
  return crypto.randomUUID().toLowerCase()
}

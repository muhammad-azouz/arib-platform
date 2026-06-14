# Arib Console (tenant-facing)

The cloud console where Arib POS **tenants** manage their business: company info,
branches, device seats, and sync tokens for desktop installs. Sibling to the
internal operator app in `../admin`, sharing the same `/v1` Go license API.

> Account → Tenant → **one** Company → many Branches (each with device seats).
> Branch data lives in a per-tenant SQL central DB synced via Dotmim.Sync; the
> console reads/writes the cloud control plane only.

## Stack

React 19 · Vite 8 · TypeScript 6 · Tailwind v4 · shadcn/ui (Radix + CVA) ·
TanStack Query + Table · react-hook-form + Zod · react-router-dom v7 ·
**Solar Icons (Line Duotone)** · Arabic-first **RTL**.

## Develop

```bash
pnpm install
pnpm dev        # proxies /v1 → http://127.0.0.1:8080 (the Go API)
pnpm build      # tsc -b && vite build
pnpm lint
```

Set `VITE_API_BASE_URL` in production (see `.env.example`); leave empty in dev.

## Status — Phase 0 (foundation)

Done: tooling, RTL warm-light theme + design tokens, Solar icon layer, two-column
app shell + Home tile launcher, loading/empty/error states, toasts, email-OTP
auth (token store + transparent refresh + 401→logout), API/Query layer.

Phases 1–3 (tenant registration, Setup Wizard → company → branches) are
placeholders for now.

### Known API gap

OAuth (`/v1/auth/{provider}/start`) requires a `127.0.0.1` loopback `cb` (desktop
flow) and **cannot** be used from a browser SPA. OAuth buttons on the login page
are therefore disabled. Adding a web-callback path to the Go API is pending
explicit go-ahead.

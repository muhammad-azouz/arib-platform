# Arib · License Control (Admin Dashboard)

Operator console for the [Arib license API](../arib-license-api). Replaces the
old "manage licenses with `curl` and ad-hoc notes" workflow with a real UI:
search clients, assign/suspend licenses, rebind devices, mint offline fallback
strings, and read the audit trail.

> This is the **admin** surface. End clients never see it — they sign in from
> inside the POS app. Access here is restricted to the email(s) on the API's
> `ADMIN_EMAILS` allow-list.

## Stack

- **Vite + React + TypeScript**
- **Tailwind CSS v4** + hand-rolled **shadcn/ui**-style primitives (Radix under the hood)
- **TanStack Query** (server state), **React Router** (routing)
- **react-hook-form + zod** (forms), **sonner** (toasts), **lucide-react** (icons), **date-fns**

Aesthetic: a dark "control-room" console — near-black slate, a single signal-amber
accent, and IBM Plex Mono for every machine-readable value (license keys, machine
IDs, tokens). Fonts: Bricolage Grotesque (display), Hanken Grotesque (body),
IBM Plex Mono (mono).

## How auth works

There is no separate admin password. The dashboard reuses the API's **email OTP**
flow:

1. Enter your email → the API emails (or, in dev, **prints to its console**) a
   6-digit code.
2. Enter the code → the API returns a session. The dashboard decodes the access
   token's `admin` claim.
3. The API only sets `admin: true` for emails in its `ADMIN_EMAILS` list. Anyone
   else is shown **"not authorized"** and refused entry.

The access token is held in memory; the refresh token in `localStorage`. Requests
auto-refresh once on a `401`, then fall back to logout.

## Prerequisites

- Node 20+
- The Go API running and reachable (see `../arib-license-api`). Make sure your
  email is in its `ADMIN_EMAILS`.

## Local development

```bash
cp .env.example .env       # leave VITE_API_BASE_URL empty for dev
npm install
npm run dev                # http://localhost:5173
```

In dev, `vite.config.ts` proxies `/v1` and `/healthz` to `http://127.0.0.1:8080`,
so the browser talks to the API **same-origin** — no CORS needed. (You can still
exercise CORS by setting `VITE_API_BASE_URL` to the API origin and adding that
origin to the API's `DASHBOARD_ORIGINS`.)

Start the API first; if its `SMTP_HOST` is empty it logs the OTP to stdout:

```
otp for you@arib.app: 481920
```

## Scripts

```bash
npm run dev      # dev server
npm run build    # tsc -b && vite build  ->  dist/
npm run preview  # preview the production build
npm run lint     # eslint
```

## Project structure

```
src/
  lib/
    api.ts       # fetch wrapper: bearer header + transparent refresh-on-401, typed admin calls
    auth.tsx     # AuthProvider: OTP login, JWT admin-claim gate, token storage, logout
    types.ts     # API response types (NOTE: model types are PascalCase; wrappers are snake_case)
    format.ts    # date / status-tone helpers
    query.ts     # QueryClient + query keys
  components/
    AppShell.tsx, PageHeader.tsx, StatCard.tsx, CopyId.tsx, ConfirmDialog.tsx
    dialogs/     # Create / Edit client, Assign license, Sign-offline
    ui/          # shadcn-style primitives (button, dialog, table, badge, …)
  pages/
    Login.tsx, Overview.tsx, Clients.tsx, ClientDetail.tsx, Audit.tsx
```

## API surface used

| UI | Endpoint |
|---|---|
| Login | `POST /v1/auth/email/start`, `POST /v1/auth/email/verify`, `POST /v1/auth/refresh` |
| Overview | `GET /v1/admin/stats`, `GET /v1/admin/audit` |
| Clients | `GET /v1/admin/clients?q=`, `POST /v1/admin/clients` |
| Client detail | `GET /v1/admin/clients/{id}`, `PATCH /v1/admin/clients/{id}` |
| License actions | `POST /v1/admin/licenses`, `POST /v1/admin/licenses/{id}/status`, `POST /v1/admin/licenses/{id}/sign-offline` |
| Device action | `POST /v1/admin/devices/{id}/release` |
| Audit | `GET /v1/admin/audit` |

## Production deployment

Built as static files (`dist/`), served on its own subdomain behind HAProxy.

1. Build with the API origin baked in:
   ```bash
   VITE_API_BASE_URL=https://license.arib.app npm run build
   ```
2. Serve `dist/` at e.g. `https://admin.arib.app` (HAProxy / any static host).
   Route SPA fallbacks to `index.html`.
3. On the **API**, allow this origin for CORS:
   ```
   DASHBOARD_ORIGINS=https://admin.arib.app
   ```
4. Ensure your operator email is in the API's `ADMIN_EMAILS`.

Because auth uses bearer tokens (not cookies), CORS runs with
`AllowCredentials: false` — only `Authorization` + `Content-Type` are needed.

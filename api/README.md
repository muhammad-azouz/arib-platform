# Arib License API

License-management backend for **AribPOS**. It replaces the old manual flow
(client sends a machine ID → you hand-sign a license string → client pastes it)
with self-serve accounts, automatic device binding, periodic revalidation, and
centralized client records — while keeping the **same RSA signed-token format**
the desktop app already trusts.

- Language: **Go** (stdlib `net/http` + chi router)
- Storage: **MongoDB** (Atlas)
- Deploy: single static binary on a **VPS behind HAProxy** (HAProxy terminates TLS)

---

## How licensing works

### Players

| Player | What they do |
|---|---|
| **Client** (business owner) | Signs up / logs in inside the POS app (Google, Facebook, or email code). Owns one or more licenses. |
| **Device** | One physical PC, identified by a hashed `machineId`. Each license binds to exactly one device. |
| **Admin** (you) | Assigns paid licenses to clients, suspends them, force-releases devices, issues offline fallback strings. Via the admin dashboard / admin endpoints. |

### The token (unchanged envelope, extended payload)

A license is a string the desktop caches as `license.lic`:

```
base64(payload) "." base64(rsaSignature)
payload = machineId | features | hardExpiry | revalidateBy | licenseId
```

- Signed with **RSA-SHA256 (PKCS#1 v1.5)** using the private key in `keys/PrivateKey.xml`.
- Verifies against the **public key already embedded** in
  `AribPOS/Services/LicenseValidator.cs`. The server reuses the exact same keypair,
  so old 3-field offline licenses still validate too.

### Lifecycle

1. **Sign up / log in** in the POS app (email OTP, Google, or Facebook). First
   signup auto-creates a **7-day trial** license.
2. **Bind** — the app sends its `machineId`; the server picks a free license seat
   (paid preferred, else trial) and returns a signed token. One active device per seat.
3. **Run** — the app verifies the token offline on every launch.
4. **Revalidate** — after **14 days** (`revalidateBy`) the app calls `/devices/validate`
   and gets a fresh token, resetting the clocks.
   - **Offline grace:** if it can't reach the server it keeps running but **nags on
     every launch**. After **28 days** (`hardExpiry`) with no successful revalidation it **blocks**.
5. **Move PC** — the client releases the old device themselves (self-service, with a
   cooldown) — or you force-release it — then binds the new PC.

### Trial anti-abuse

A trial is tied to the **account** *and* the **first device's machineId** (a
`trial_ledger` entry). A machine that already used a trial cannot start another,
even under a fresh email.

### Hidden manual fallback

For no-internet installs / special cases, you generate an **offline signed string**
from the dashboard (`POST /v1/admin/licenses/{id}/sign-offline` with a `machine_id`).
The client pastes it into a hidden screen in the POS app (secret hotkey on the login
screen); it verifies offline against the public key — no account or internet needed.

---

## Architecture

```
cmd/api/main.go            wiring + graceful shutdown
internal/
  config/                  env loading + validation
  httpapi/                 chi router, middleware, handlers
  auth/                    OTP, Google/Facebook OAuth, session JWT + refresh
  license/                 license provisioning + RSA token signing
  device/                  bind / validate / release (+ trial & cooldown rules)
  admin/                   client/license/device management, offline signing, audit
  mail/                    SMTP OTP delivery (logs codes in dev)
  model/                   MongoDB document types
  store/mongo/             repositories + index setup
pkg/licensetoken/          .NET-compatible RSA sign/verify codec  (unit-tested)
keys/                      PrivateKey.xml / PublicKey.xml  (gitignored)
```

---

## Configuration

Copy `.env.example` to `.env` and fill it in. Key variables:

| Var | Meaning |
|---|---|
| `HTTP_ADDR` | Bind address (HAProxy forwards here, e.g. `127.0.0.1:8080`). |
| `PUBLIC_BASE_URL` | Public https URL; used to build OAuth callback URLs. |
| `MONGO_URI` / `MONGO_DB` | Atlas connection string + database name. |
| `PRIVATE_KEY_XML_PATH` | Path to the .NET RSA private key (same keypair as the POS public key). |
| `JWT_SECRET` | 32+ random bytes; signs session access tokens and OAuth state. |
| `REVALIDATE_AFTER` / `HARD_EXPIRE_AFTER` | Token clocks (default 14d / 28d). |
| `TRIAL_DURATION` | Free trial length (default 7d). |
| `RELEASE_COOLDOWN` / `RELEASE_MAX_PER_MONTH` | Self-service rebind throttle. |
| `SMTP_*` | OTP email delivery. Leave `SMTP_HOST` empty in dev to log codes instead. |
| `GOOGLE_*` / `FACEBOOK_*` | OAuth client credentials. A provider with no ID is disabled. |
| `ADMIN_EMAILS` | Comma-separated emails granted admin access. |

---

## Run locally

```bash
cp .env.example .env      # set MONGO_URI + JWT_SECRET at minimum
make run                  # or: go run ./cmd/api
make test                 # crypto codec round-trip tests
```

With `SMTP_HOST` empty, OTP codes are printed to the server log so you can test
email login without a mail provider.

---

## API reference

### Auth (public)
| Method | Path | Body | Notes |
|---|---|---|---|
| POST | `/v1/auth/email/start` | `{email}` | Emails a 6-digit code (rate-limited). |
| POST | `/v1/auth/email/verify` | `{email, code, first_name?, last_name?}` | Returns a session; creates account + trial on first login. |
| GET | `/v1/auth/{google\|facebook}/start?cb=<loopback>` | – | Browser redirect to consent. `cb` must be `http://127.0.0.1:<port>`. |
| GET | `/v1/auth/{provider}/callback` | – | Provider returns here; redirects back to `cb?code=<one-time>`. |
| POST | `/v1/auth/exchange` | `{code}` | Swaps the one-time OAuth code for a session. |
| POST | `/v1/auth/refresh` | `{refresh_token}` | Rotates and returns a new session pair. |
| POST | `/v1/auth/logout` | `{refresh_token}` | Revokes the refresh token. |

A session is `{access_token, refresh_token, expires_in}`. Send the access token as
`Authorization: Bearer <token>`.

### Client (authenticated)
| Method | Path | Body | Notes |
|---|---|---|---|
| GET | `/v1/me` | – | Account + licenses + devices. |
| POST | `/v1/devices/bind` | `{machine_id, machine_name?, os?}` | Binds a seat, returns a signed token. |
| POST | `/v1/devices/validate` | `{machine_id}` | Revalidates; returns a fresh token. |
| POST | `/v1/devices/release` | `{device_id}` | Self-service release (cooldown enforced). |

### Admin (`Authorization` of an `ADMIN_EMAILS` account)
| Method | Path | Body |
|---|---|---|
| GET | `/v1/admin/clients?q=` | – |
| POST | `/v1/admin/clients` | `{email, first_name?, last_name?, notes?}` |
| GET | `/v1/admin/clients/{id}` | – |
| POST | `/v1/admin/licenses` | `{email, features, expires_at, count, notes?}` |
| POST | `/v1/admin/licenses/{id}/status` | `{status: active\|suspended\|expired}` |
| POST | `/v1/admin/licenses/{id}/sign-offline` | `{machine_id}` → `{license}` |
| POST | `/v1/admin/devices/{id}/release` | – (force release) |
| GET | `/v1/admin/audit` | – |

---

## curl walkthrough (local, email login)

```bash
BASE=http://127.0.0.1:8080

# 1. Request a code (printed in the server log in dev).
curl -s $BASE/v1/auth/email/start -d '{"email":"client@example.com"}'

# 2. Verify -> session (also creates the 7-day trial on first login).
curl -s $BASE/v1/auth/email/verify \
  -d '{"email":"client@example.com","code":"123456","first_name":"Sara","last_name":"A"}'
ACCESS=...   # access_token from the response

# 3. Bind this machine -> signed license token.
curl -s $BASE/v1/devices/bind -H "Authorization: Bearer $ACCESS" \
  -d '{"machine_id":"abc123","machine_name":"Front Till","os":"windows"}'

# 4. Revalidate (fresh token, clocks reset).
curl -s $BASE/v1/devices/validate -H "Authorization: Bearer $ACCESS" \
  -d '{"machine_id":"abc123"}'
```

---

## OAuth provider setup

Register **one** redirect URI per provider, pointing at this API (not the desktop):

- Google: `https://<PUBLIC_BASE_URL>/v1/auth/google/callback`
- Facebook: `https://<PUBLIC_BASE_URL>/v1/auth/facebook/callback`

The desktop app passes its own loopback URL via `?cb=http://127.0.0.1:<port>`; the
API validates it is loopback and hands back a one-time code there after consent.
Because the API is a confidential client (it holds the client secret) and the
handoff code is single-use and short-lived, no PKCE is required.

---

## Deploy (VPS + HAProxy + Atlas)

1. Build a static binary and copy it + `keys/PrivateKey.xml` + `.env` to the VPS:
   ```bash
   make build               # bin/arib-license-api (CGO disabled)
   scp bin/arib-license-api keys/PrivateKey.xml .env  user@vps:/opt/arib-license/
   ```
2. Run it under systemd binding to `127.0.0.1:8080` (see `HTTP_ADDR`).
3. Point HAProxy at it and terminate TLS there, e.g.:
   ```
   frontend https
     bind :443 ssl crt /etc/haproxy/certs/license.arib.pem
     default_backend arib_license
   backend arib_license
     server api 127.0.0.1:8080
   ```
4. Set `PUBLIC_BASE_URL=https://license.arib...` so OAuth callbacks use https.
5. Keep `keys/PrivateKey.xml` readable only by the service user. **Never commit it.**

> Docker is intentionally not used yet; revisit later if you want containerized deploys.

Example systemd unit:

```ini
[Unit]
Description=Arib License API
After=network.target

[Service]
WorkingDirectory=/opt/arib-license
EnvironmentFile=/opt/arib-license/.env
ExecStart=/opt/arib-license/arib-license-api
Restart=on-failure
User=arib

[Install]
WantedBy=multi-user.target
```

---

## Desktop integration (AribPOS)

- `LicenseApiClient` calls the auth + device endpoints; stores the refresh token in
  the existing remember-me secure storage.
- The login view offers email-code, Google and Facebook; on success it auto-binds the
  device and writes `license.lic`.
- `LicenseValidator` parses the 5-field token, enforces signature + machine + `hardExpiry`,
  and triggers a background `/devices/validate` after `revalidateBy` (nag on offline,
  block past `hardExpiry`).
- A hidden hotkey on the login screen opens the manual paste box for an admin-signed
  offline string.

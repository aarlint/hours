# Hours — Time Tracking & Invoicing for Contractors

Contract-based time tracking, expenses, quotes, and PDF invoice generation. Three ways to use it:

- **Hosted web app** at [`hours.arlint.dev`](https://hours.arlint.dev) — sign in with your existing identity, get an isolated tenant, log time and generate invoices from any browser.
- **Native desktop app** — single-binary Wails build for macOS that ships with a local SQLite DB at `~/.hours/db`. Same UI, runs offline.
- **MCP servers** — both stdio (for the local desktop binary) and remote streamable HTTP (against the hosted app) so Claude Desktop can read and write your hours conversationally.

The same Go binary powers all three modes. The differences are auth, storage location, and which tools are exposed.

---

## Hosted vs Local — pick one

| | Hosted (`hours.arlint.dev`) | Local (desktop / stdio MCP) |
|---|---|---|
| **Auth** | OIDC sign-in (Cloudflare Access SaaS) + per-user API tokens with fine-grained scopes | None — single user on your machine |
| **Storage** | SQLite, multi-tenant, `user_id` on every business row | SQLite at `~/.hours/db`, single tenant |
| **Tenant isolation** | Every query filtered by `user_id`; per-user UNIQUE constraints | N/A |
| **Web UI** | `https://hours.arlint.dev` | Embedded in the Wails desktop app |
| **MCP transport** | Streamable HTTP at `/api/mcp`, stateless, bearer-token auth | stdio (launched by Claude Desktop) |
| **Backup/restore tools over MCP** | Not exposed (whole-DB ops can't be tenant-scoped) | Available |
| **Per-user export dir for generated files** | `~/.hours/exports/<user_id>/` (server-side; ephemeral) | Wherever the Save dialog lands |
| **Best for** | Multiple users, remote access, long-running deployments | Single contractor working on one machine |

You can use both — they have separate data. Many people run the hosted version for day-to-day with Claude, plus the desktop app for offline backups and CSV exports.

---

## Quick Start — Hosted

1. **Sign in** at [`hours.arlint.dev`](https://hours.arlint.dev). You'll be redirected to Cloudflare Access for OIDC; once you're verified you land in the app with your own isolated tenant. First sign-in creates an empty tenant for you.
2. **Configure your business profile** in Settings → Business Info — the name, address, and contact details that appear on invoice headers.
3. **Add a client** in the Clients view, then **a contract** (rate + terms) tied to that client.
4. **Log time** in the Time view, against a specific contract.
5. **Generate invoices** from the Invoices view; the PDF streams back to your browser as a download.
6. **Mint an API token** in Settings → API Tokens to give Claude Desktop access (see [Claude Desktop](#claude-desktop) below).

There's nothing to install for the hosted path. All data lives in your tenant on the server. You can use `Settings → Export & Import` to dump everything as JSON anytime, or revoke access entirely by deleting your tokens and signing out.

---

## Quick Start — Local Desktop

### Pre-built binaries

Download the binary for your platform from [Latest Release](https://github.com/aarlint/hours/releases/latest):

| Platform | Download |
|----------|----------|
| **macOS (Apple Silicon)** | `hours-mcp-darwin-arm64` |
| **macOS (Intel)** | `hours-mcp-darwin-amd64` |
| **Linux (x64)** | `hours-mcp-linux-amd64` |
| **Linux (ARM64)** | `hours-mcp-linux-arm64` |
| **Windows (x64)** | `hours-mcp-windows-amd64.exe` |

Install:

```bash
chmod +x hours-mcp-*
mv hours-mcp-* ~/.local/bin/hours-mcp
```

The binary has three modes:
- `hours-mcp` (no args) — runs the native Wails desktop app (macOS only for the prebuilt `Hours.app`).
- `hours-mcp --mcp` — runs the MCP stdio server. This is what Claude Desktop launches as a subprocess.
- `hours-mcp --serve --addr :7878` — runs the multi-tenant HTTP server. This is what powers `hours.arlint.dev` in Docker. Requires OIDC env vars; see [Self-hosting](#self-hosting).

### First steps (local)

1. **Configure your business**: *"Set up my business information"*
2. **Add a client**: *"Add client Acme Corp with address 123 Main St, San Francisco, CA"*
3. **Create a contract**: *"Add contract AC-001 for Acme Corp at $150/hour"*
4. **Track time**: *"Add 2 hours for contract AC-001 today - API development"*
5. **Generate invoice**: *"Create invoice for Acme Corp for this month"*

(Local desktop is single-tenant — there's no sign-in.)

---

## Claude Desktop

### Local stdio MCP (single user)

```json
{
  "mcpServers": {
    "hours": {
      "command": "/Users/YOUR_USERNAME/.local/bin/hours-mcp",
      "args": ["--mcp"]
    }
  }
}
```

Config file paths:
- macOS: `~/Library/Application Support/Claude/claude_desktop_config.json`
- Windows: `%APPDATA%\Claude\claude_desktop_config.json`
- Linux: `~/.config/Claude/claude_desktop_config.json`

Restart Claude Desktop fully (Cmd+Q on macOS — closing the window isn't enough). All 41 tools are available, including the four backup tools (`create_database_backup`, etc.).

### Remote HTTPS MCP (hosted, scoped per-token)

The hosted deployment exposes a streamable HTTP MCP endpoint at `https://hours.arlint.dev/api/mcp`. It runs in **stateless mode**, so container restarts and network blips don't expire your client session — no need to relaunch Claude Desktop just because the server cycled.

1. **Mint a token** at [`hours.arlint.dev`](https://hours.arlint.dev) → Settings → API Tokens. Pick scopes:
    - **Read-only assistant**: every `:read` scope (`clients:read`, `time_entries:read`, `invoices:read`, `quotes:read`, `expenses:read`, `business_info:read`, `recipients:read`, `contracts:read`, `payment_methods:read`).
    - **Time logger**: add `time_entries:write`.
    - **Full assistant**: add the matching `:write` scopes for whatever Claude should be able to do (`clients:write`, `contracts:write`, `invoices:write`, etc.).
    - Wildcard `*` is admin-only.
2. **Copy the token** — it starts with `ht_` and is shown ONCE. The server only stores its SHA-256 hash; a lost token must be revoked and re-minted.
3. **Add to Claude Desktop**:

   ```json
   {
     "mcpServers": {
       "hours-remote": {
         "command": "npx",
         "args": [
           "-y",
           "mcp-remote",
           "https://hours.arlint.dev/api/mcp",
           "--header",
           "Authorization: Bearer ht_paste-your-token-here"
         ]
       }
     }
   }
   ```

4. **Restart Claude Desktop**. Tools missing the required scope return `permission denied: missing scope(s) <scope>` instead of executing.

The four whole-DB backup tools are intentionally NOT exposed on the remote transport — they can't be tenant-scoped. Use the local stdio binary for backups.

---

## Features

- **Contract-based billing** with individual rates, currencies, payment terms, and optional billing cycles per engagement
- **Client management** with full address info; same client name can exist across different tenants
- **Time tracking** in 15-minute increments, scoped to specific contracts
- **Expenses** logged against a client (and optionally a contract); rolled into invoice generation when unbilled
- **Quotes** with line items, status workflow (draft → sent → accepted → converted), and one-click conversion to a contract
- **Invoice generation** as a PDF stream (web) or via native Save-As (desktop). Snapshots payment method onto the invoice so historical bank info is preserved through later changes
- **Payment methods** (business-level, multiple per user) attached to contracts and snapshotted onto invoices
- **Recipients** — multiple billing contacts per client, optionally one marked primary
- **Multi-tenant on the hosted deployment** — every business row is keyed by `user_id`; isolation enforced at the SQL layer
- **Fine-grained API tokens** with 23 resource-action scopes; usage telemetry on every call
- **Live updates** via SSE — UI reflects changes from MCP/CLI tool calls in real time, fan-out scoped per user
- **Data portability** — JSON export and import. Import wipes only the calling user's rows and re-stamps `user_id` on every insert.

---

## Database Schema

SQLite with WAL mode. Multi-tenant data tables carry a `user_id` foreign key to `users`. Schema relationships:

```mermaid
erDiagram
    users {
        int id PK
        string oidc_subject UK
        string email
        string name
        string role "user|admin"
        datetime created_at
        datetime last_login_at
    }

    sessions {
        string token PK
        int user_id FK
        datetime expires_at
        datetime created_at
    }

    api_tokens {
        int id PK
        int user_id FK
        string name
        string token_hash UK "SHA-256"
        string token_prefix "first 11 chars"
        string scopes "comma-separated"
        datetime expires_at
        datetime last_used_at
        datetime created_at
        datetime revoked_at
    }

    api_token_usage {
        int id PK
        int token_id FK
        int user_id FK
        string method
        string path
        int status
        int duration_ms
        string error
        datetime created_at
    }

    clients {
        int id PK
        int user_id FK
        string name "UNIQUE(user_id, name)"
        string address
        string city
        string state
        string zip_code
        string country
        datetime created_at
        datetime updated_at
    }

    contracts {
        int id PK
        int user_id FK
        int client_id FK
        string contract_number "UNIQUE(user_id, contract_number)"
        string name
        real hourly_rate
        string currency
        string contract_type
        date start_date
        date end_date
        string status
        string payment_terms
        string notes
        int payment_method_id FK
        int billing_cycle_day
        string billing_cycle_type
        date next_billing_date
        boolean auto_bill_enabled
        datetime created_at
        datetime updated_at
    }

    recipients {
        int id PK
        int client_id FK
        string name
        string email
        string title
        string phone
        boolean is_primary
        datetime created_at
    }

    payment_methods {
        int id PK
        int user_id FK
        string label
        string bank_name
        string account_number
        string routing_number
        string swift_code
        string payment_terms
        string notes
        boolean is_default
        datetime created_at
        datetime updated_at
    }

    payment_details {
        int id PK
        int client_id FK "legacy, one per client"
        string bank_name
        string account_number
        string routing_number
        string swift_code
        string payment_terms
        string notes
        datetime updated_at
    }

    time_entries {
        string id PK "UUID"
        int user_id FK
        int client_id FK
        int contract_id FK
        date date
        real hours
        string description
        string contract_ref
        int invoice_id FK
        datetime created_at
    }

    invoices {
        int id PK
        int user_id FK
        int client_id FK
        string invoice_number "UNIQUE(user_id, invoice_number)"
        date issue_date
        date due_date
        real total_amount
        string status
        string pdf_path
        int payment_method_id FK
        datetime created_at
    }

    expenses {
        string id PK "UUID"
        int user_id FK
        int client_id FK
        int contract_id FK
        date date
        string description
        real amount
        string currency
        string category
        string receipt_path
        int invoice_id FK
        datetime created_at
    }

    quotes {
        int id PK
        int user_id FK
        int client_id FK
        string quote_number "UNIQUE(user_id, quote_number)"
        string title
        date issue_date
        date valid_until
        real subtotal
        real total_amount
        string currency
        string status
        string notes
        string pdf_path
        int converted_contract_id FK
        datetime created_at
        datetime updated_at
    }

    quote_line_items {
        int id PK
        int quote_id FK
        string description
        real quantity
        string unit
        real unit_price
        real amount
        int sort_order
    }

    business_info {
        int user_id PK "one per user"
        string business_name
        string contact_name
        string email
        string phone
        string address
        string city
        string state
        string zip_code
        string country
        string tax_id
        string website
        string logo_path
        string invoice_prefix
        datetime updated_at
    }

    migrations {
        int id PK
        string name UK
        datetime applied_at
    }

    users ||--o{ sessions : "owns"
    users ||--o{ api_tokens : "owns"
    users ||--o{ api_token_usage : "generated"
    api_tokens ||--o{ api_token_usage : "tracks"
    users ||--o{ clients : "owns"
    users ||--o{ contracts : "owns"
    users ||--o{ time_entries : "owns"
    users ||--o{ invoices : "owns"
    users ||--o{ expenses : "owns"
    users ||--o{ quotes : "owns"
    users ||--o{ payment_methods : "owns"
    users ||--|| business_info : "configures"
    clients ||--o{ contracts : "has contracts"
    clients ||--o{ recipients : "has contacts"
    clients ||--o| payment_details : "legacy banking"
    clients ||--o{ invoices : "receives invoices"
    clients ||--o{ quotes : "receives quotes"
    clients ||--o{ expenses : "incurs"
    contracts ||--o{ time_entries : "tracks hours against"
    contracts ||--o{ expenses : "categorises against"
    contracts }o--o| payment_methods : "default method"
    invoices ||--o{ time_entries : "includes entries"
    invoices ||--o{ expenses : "includes expenses"
    invoices }o--o| payment_methods : "snapshotted method"
    quotes ||--o{ quote_line_items : "has line items"
    quotes }o--o| contracts : "converted to"
```

### Key relationships and design notes

- **Tenancy** — every business-level table has a direct `user_id` column (denormalized from the parent chain) so handler queries scope with a single `WHERE user_id = ?` rather than multi-step JOINs. The handful of FK-scoped child tables (`recipients`, `payment_details`, `quote_line_items`) reach through their parent's `user_id`.
- **Per-user uniqueness** — `clients.name`, `contracts.contract_number`, `invoices.invoice_number`, and `quotes.quote_number` are all `UNIQUE(user_id, …)` so two tenants can have a "Contract #001" without colliding.
- **`business_info`** is keyed by `user_id` (was a global singleton in earlier versions; now one row per tenant).
- **Contract-based billing** — clients no longer carry rates. Every time entry references a contract, and the contract owns the rate. Historical rate changes are preserved by versioning contracts.
- **Payment methods** are business-level (multiple per tenant), attached to contracts, and snapshotted onto invoices. Old `payment_details` (one per client) is retained for backward compatibility but new invoices read from `payment_methods`.
- **Quotes** flow `draft → sent → accepted → converted` with one-click conversion that creates a `contract` and stamps `converted_contract_id` for traceability.
- **Audit log** — `api_token_usage` records every bearer-authed call (REST or MCP) with method, path, status, duration, and an error tail. Surfaced per-token in Settings → API Tokens → Usage.

---

## API Tokens & Scopes

API tokens are personal bearer credentials minted per user, scoped to a chosen subset of permissions, and bound to the issuing user's tenant. Tokens are sent as `Authorization: Bearer ht_…` headers; both the REST API and the MCP HTTP endpoint accept them.

| Scope | What it grants |
|---|---|
| `clients:read` / `clients:write` | List/get / create-edit-delete clients |
| `contracts:read` / `contracts:write` | List/get / create-edit contracts |
| `time_entries:read` / `time_entries:write` | List/search / add-edit-delete-bulk on time entries |
| `invoices:read` / `invoices:write` | List/get/preview/download / create-status-delete invoices |
| `quotes:read` / `quotes:write` | List/get/download / create-edit-delete-convert quotes |
| `expenses:read` / `expenses:write` | List / create-edit-delete expenses |
| `payment_methods:read` / `payment_methods:write` | List/get / create-edit-delete payment methods |
| `recipients:read` / `recipients:write` | List / create-delete recipients per client |
| `business_info:read` / `business_info:write` | Get / set business profile |
| `stats:read` | Dashboard stats endpoint |
| `events:read` | SSE realtime stream |
| `data:export` | Download tenant JSON export |
| `data:import` | Wipe-and-replace tenant from JSON (admin-only at the role layer) |
| `*` | All of the above (admin-only minting) |

**Security properties:**
- Tokens stored as SHA-256 hash + 11-char prefix; raw token shown once at mint time.
- Token-management endpoints (`/api/tokens/*`) reject bearer auth — only session cookies can mint or revoke. Prevents a leaked token from minting siblings.
- Non-admin users cannot mint `*` or `data:import` scopes.
- Expired (`expires_at < now`) and revoked (`revoked_at IS NOT NULL`) tokens are rejected at lookup.
- Every bearer-authed call lands a row in `api_token_usage`. View per-token usage at Settings → API Tokens → USAGE.

---

## Self-hosting

The same `hours-mcp --serve` binary that powers `hours.arlint.dev` can run anywhere with Docker.

### docker-compose.yaml

```yaml
services:
  hours:
    build: .
    container_name: hours
    restart: unless-stopped
    networks:
      - traefik-proxy
    labels:
      - "traefik.docker.network=traefik-proxy"
      - "traefik.http.services.hours.loadbalancer.server.port=7878"
    environment:
      OIDC_ISSUER: ${OIDC_ISSUER}
      OIDC_CLIENT_ID: ${OIDC_CLIENT_ID}
      OIDC_CLIENT_SECRET: ${OIDC_CLIENT_SECRET}
      OIDC_REDIRECT_URL: ${OIDC_REDIRECT_URL}
      OIDC_SCOPES: ${OIDC_SCOPES:-openid profile email}
      OIDC_COOKIE_SECURE: "1"
      OIDC_ALLOWED_EMAILS: ${OIDC_ALLOWED_EMAILS:-}
      OIDC_BOOTSTRAP_ADMIN_EMAILS: ${OIDC_BOOTSTRAP_ADMIN_EMAILS:-}
    volumes:
      - hours-data:/home/hours/.hours

volumes:
  hours-data:

networks:
  traefik-proxy:
    external: true
```

### Required env vars

| Variable | Required? | Purpose |
|---|---|---|
| `OIDC_ISSUER` | yes | OIDC discovery URL (e.g. `https://your-team.cloudflareaccess.com/cdn-cgi/access/sso/oidc/<client_id>`) |
| `OIDC_CLIENT_ID` | yes | OIDC client ID for this app |
| `OIDC_CLIENT_SECRET` | yes | OIDC client secret |
| `OIDC_REDIRECT_URL` | yes | `https://your-host/auth/callback` |
| `OIDC_SCOPES` | no | space-separated; defaults to `openid profile email` |
| `OIDC_COOKIE_SECURE` | no | `"1"` if behind TLS; required for `SameSite=None` cookies on cross-site OIDC redirect chains |
| `OIDC_ALLOWED_EMAILS` | no | comma-separated allowlist; if empty, any verified user can sign up |
| `OIDC_BOOTSTRAP_ADMIN_EMAILS` | no | comma-separated; matching emails are auto-promoted to `admin` on every login |

`--serve` mode refuses to start if the four required OIDC vars are missing — multi-tenant with no auth is never the right answer.

### Identity provider

Any OIDC-compliant provider works. The hosted deployment uses **Cloudflare Access** as a SaaS OIDC IdP, which has the nice property that you can layer Cloudflare's edge gating in front of the same domain for defense-in-depth (or skip the edge gate and let the app's OIDC dance handle login alone — both work).

---

## Build from source

```bash
git clone https://github.com/aarlint/hours.git
cd hours
go mod download

# Build the local stdio + Wails binary
make build

# Build for all platforms
make build-all

# Install to ~/.local/bin
make install
```

For the hosted/server build (Linux + Wails dependencies + Vite frontend), use the `Dockerfile` in the repo:

```bash
docker compose up -d --build
```

### Development

```bash
make run     # go run . --serve, with OIDC env vars set
make test    # go test ./...
make clean   # remove build artifacts
make app     # full Wails desktop app bundle (macOS only)
```

---

## Usage examples

### Client & contract management
```
"Add client Acme Corp with address 123 Business St, San Francisco, CA 94102"
"Edit client Acme Corp to update address to 456 New Street, Los Angeles, CA 90210"
"List all clients"
"Add contract AC-2025-001 for Acme Corp with rate $150/hour for Backend Development"
"List contracts for Acme Corp"
"Add recipient John Doe john@acmecorp.com for Acme Corp with title CTO"
"List recipients for Acme Corp"
"Remove recipient ID 5"
```

### Time tracking
```
"Add 2 hours for contract AC-2025-001 today"
"Add 8 hours for contract AC-2025-001 this week"
"Add 4.5 hours for contract AC-2025-001 yesterday with description 'Backend API development'"
"List hours for this month"
"Show all hours for Acme Corp last week"
"Search time entries for contract AC-2025-001"
```

### Invoices, quotes, expenses
```
"Create invoice for Acme Corp for this month"
"Make invoice for ClientX for last month"
"Create invoice for January 2025 for Acme Corp"
"List all pending invoices"
"Show invoice INV-202501-abc12345"
"Add expense $42 lunch for Acme Corp today"
"Quote Acme Corp for migration project: 80 hours at $175/hr"
"Convert quote QT-2025-002 to a contract"
```

### Natural language input

The MCP supports flexible time inputs:
- **Time periods**: "today", "yesterday", "this week", "last week", "this month", "January 2025"
- **Hour increments**: 0.25 (15 min), 0.5 (30 min), 0.75 (45 min), etc.
- **Bulk entries**: "Add 8 hours for contract CA-001 this week" adds 8 hours to each weekday
- **Detailed descriptions**: rich free-text on every entry

---

## PDF output

Invoices and quotes are generated as PDFs:

- **Web (`hours.arlint.dev`)**: streamed back with `Content-Disposition: attachment` so the browser handles the save (Downloads folder or Save-As, depending on your browser settings).
- **Desktop (Wails)**: the frontend pops a native Save-As dialog and writes bytes to the chosen path. Same handler, different transport.
- **Stdio MCP / CLI**: writes to `~/.hours/exports/<user_id>/` and returns the path.

Each invoice includes a business-info header, a billing block (FROM your business / BILL TO the client + recipients), the contract details, an itemized time-entries table, an expenses table when present, totals, and a payment ledger. Status flows are `pending → paid` (with `cancelled` and `overdue` available).

---

## License

MIT

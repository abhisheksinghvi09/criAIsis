# criAIsis Quickstart

Bring-your-own-key, multi-tenant. Each customer supplies their own Anthropic key,
their own embeddings key, and their own Slack or Discord webhook. The platform
operator never holds a shared model credential.

---

## Credentials and who holds what

| Credential | Held by | Can do | Cannot do |
|---|---|---|---|
| `CRIAISIS_SECURITY_ADMIN_API_KEY` | Platform operator | Create workspaces | Read or change any tenant's keys |
| Workspace **admin key** | The customer | Configure *their* workspace, upload runbooks, rotate their alert token | Touch any other workspace |
| Workspace **alert token** | The customer's monitoring system | Open incidents | Read or change keys |
| `CRIAISIS_SECURITY_CREDENTIAL_ENCRYPTION_KEY` | Platform operator | Decrypt stored tenant credentials at runtime | — |

Three separate credentials, on purpose. A Grafana instance holding an alert token
can open incidents but cannot read the tenant's Anthropic key. A workspace admin
key is checked against the workspace named in the URL, so a valid key for tenant A
is rejected against tenant B.

Every tenant secret is stored as AES-GCM-256 ciphertext. Admin keys and alert
tokens are stored only as SHA-256 digests and are shown exactly once.

---

## 1. Start the platform

```bash
task setup:env     # writes .env with freshly generated platform secrets
task db:up         # PostgreSQL 16 + pgvector, published on host port 55432
task run           # loads .env automatically
```

`task setup:env` prints the platform admin key once. No model keys are needed to
start: those belong to each tenant.

The dev ports avoid the usual collisions — the database publishes **55432** and
the API listens on **8088** — because 5432 and 8080 are routinely held by a
native PostgreSQL, another project, or a container port-forward. A forward bound
to `127.0.0.1` will shadow this process on IPv4 without either side reporting an
error. Override with `CRIAISIS_DB_HOST_PORT` and `CRIAISIS_SERVER_PORT`.

Migrations apply on boot. No model keys are needed to start: they belong to tenants.

## 2. Start the dashboard

```bash
cd web && npm install && npm run dev      # http://localhost:3000
```

The dashboard proxies `/api/*` to the Go API, so the workspace admin key travels
same-origin. Point it elsewhere with `CRIAISIS_API_URL`.

Everything below can be done in the dashboard instead of curl. The curl commands
are kept because they are also the API contract.

## 3. Create a customer workspace (platform operator)

```bash
curl -X POST http://localhost:8088/api/v1/workspaces/ \
  -H "Authorization: Bearer $CRIAISIS_SECURITY_ADMIN_API_KEY" \
  -H 'Content-Type: application/json' \
  -d '{"team_id":"acme","name":"Acme Engineering"}'
```

Returns an **admin key** and an **alert token**, both shown once. Hand the admin
key to the customer; everything below is done by them.

## 4. The customer adds their own keys

```bash
ADMIN=<the admin key from step 2>
BASE=http://localhost:8088/api/v1/workspaces/acme

# Anthropic — verified live before it is stored
curl -X PUT $BASE/llm -H "Authorization: Bearer $ADMIN" \
  -H 'Content-Type: application/json' \
  -d '{"api_key":"sk-ant-..."}'

# Embeddings — verified, and checked to return 1536 dimensions
curl -X PUT $BASE/embeddings -H "Authorization: Bearer $ADMIN" \
  -H 'Content-Type: application/json' \
  -d '{"api_key":"sk-..."}'

# Where investigations are posted
curl -X PUT $BASE/notifications -H "Authorization: Bearer $ADMIN" \
  -H 'Content-Type: application/json' \
  -d '{"provider":"slack","webhook_url":"https://hooks.slack.com/services/..."}'
```

Each key is tested against the live provider before being stored, so a bad
credential fails here rather than four model calls into a real incident. A wrong
embedding dimension is rejected too, because it would fail on every insert.

**Slack webhook:** api.slack.com/apps → *Incoming Webhooks* → *Add New Webhook*.
**Discord webhook:** channel → *Edit Channel* → *Integrations* → *New Webhook*.

## 5. Upload runbooks

Specialists cite the customer's documentation. With none loaded they correctly
report no confidence rather than inventing advice.

```bash
curl -X POST $BASE/runbooks -H "Authorization: Bearer $ADMIN" \
  -H 'Content-Type: application/json' \
  -d "$(jq -n --arg c "$(cat runbooks/postgres.md)" \
        '{persona:"database", title:"PostgreSQL Saturation", content:$c}')"

curl $BASE/runbooks -H "Authorization: Bearer $ADMIN"   # coverage per specialist
```

Embedding runs on the customer's key, so ingestion cost is theirs. Re-uploading
unchanged content is skipped by content hash.

## 6. Prove it works end to end

```bash
curl -X POST $BASE/test-investigation -H "Authorization: Bearer $ADMIN"
```

This runs a **real** investigation on a representative incident using their keys
and their runbooks, and posts the result to their channel. It exercises the whole
chain before any real outage. `POST $BASE/notifications/test` checks delivery alone.

## 7. Connect monitoring

Expose the port: `cloudflared tunnel --url http://localhost:8088`

**Grafana** → *Alerting* → *Contact points* → *Webhook*:
- URL: `https://<tunnel>/api/v1/integrations/alerts/grafana?workspace=acme`
- Header: `X-Criaisis-Token: <alert token>`

**AWS CloudWatch:** point an SNS subscription at
`https://<tunnel>/api/v1/integrations/alerts/cloudwatch?workspace=acme` with the
same header. The subscription confirmation handshake is completed automatically,
and only `SubscribeURL`s on `amazonaws.com` are followed.

---

## The dashboard

Next.js + TypeScript, in `web/`. Six routes, all scoped to the signed-in workspace:

| Route | Purpose |
|---|---|
| Overview | Readiness, recent incidents, run a setup check |
| Setup | Enter your Anthropic key, embeddings key and webhook |
| Runbooks | Coverage per specialist, upload documents |
| Incidents | History, and the full debate transcript with citations |
| Specialists | Tune each persona's prompt, enable or disable |
| Integrations | Grafana and CloudWatch endpoints, rotate the alert token |

Sign in with the workspace id and the admin key. The key is held in
`sessionStorage` for that browser session only and is verified against the
workspace before it is stored.

Light is the first-use mode; the switch sits at the bottom of the sidebar and
changes colour and type without resetting the route or any form you are filling in.

---

## Management API

All routes below `/api/v1/workspaces/{workspace}` require
`Authorization: Bearer <workspace admin key>` for that same workspace.

| Method | Path | Purpose |
|---|---|---|
| POST | `/api/v1/workspaces/` | Create a workspace (platform admin key) |
| GET | `/{workspace}/` | Configuration and what is still missing |
| PUT | `/{workspace}/llm` | Set the model credential |
| PUT | `/{workspace}/embeddings` | Set the embeddings credential |
| PUT | `/{workspace}/notifications` | Set Slack or Discord destination |
| POST | `/{workspace}/notifications/test` | Post a test message |
| POST | `/{workspace}/test-investigation` | Run a real investigation end to end |
| POST | `/{workspace}/alert-token/rotate` | Issue a new ingestion token |
| GET / POST | `/{workspace}/runbooks` | Coverage / upload |

`GET /{workspace}/` never returns a secret. It reports `api_key_set`,
`webhook_set`, `ready_to_investigate`, and a `missing` list.

---

## Troubleshooting

**401 on a management route.** The admin key must match the workspace in the URL.
Unknown workspace and wrong key return the same response on purpose: the
difference would let someone enumerate tenants.

**"the model credential was rejected".** The key was tested live and failed. The
message carries the provider's reason.

**"embedding model returns N dimensions, but the schema requires 1536".** Use
`text-embedding-3-small`, or a model configured to 1536 dimensions.

**Alert accepted, nothing posted.** Check `GET /{workspace}/`. If
`ready_to_investigate` is false, `missing` says what is absent. A workspace that
is not ready logs a warning and skips the debate rather than half-running it.

**503 from the alert endpoint.** The queue is saturated. The incident was saved;
the debate was shed. Retry.

**Specialists report no confidence.** No runbook matched. That is the designed
behaviour: report the gap rather than guess.

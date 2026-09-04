# vaulty-keeper database tunnel proxy · diagrams

> [中文](db-proxy-architecture.zh-CN.md) | English
>
> Visuals: what's inside Docker, where credentials live, how the tunnel works, and where the security boundary is.
> Read alongside `scripts/dbtest.sh` — run it once and check back against these diagrams and it all clicks.

---

## Figure 1 · Overview: the whole chain in one picture

```
┌─────────────────────────────────────────────────────────────────────┐
│ Docker container (AI agent isolation domain: no keys / ciphertext   │
│ / real credentials reachable)                                       │
│                                                                     │
│   your clients: psql / mysql / redis-cli / DBeaver / Redis Insight  │
│     │  only carry a TOKEN, never the real account/password          │
└──────┼──────────────────────────────────────────────────────────────┘
       │ TCP (when a container reaches the host, write host.docker.internal)
       ▼
┌─────────────────────────────────────────────────────────────────────┐
│ Host: vaulty-keeper serve --addr 0.0.0.0:8972   (one process, two   │
│ services)                                                           │
│                                                                     │
│  ① HTTP mask bridge :8972         ② DB tunnels (one TCP port per    │
│    /api/* all require token         connection)                     │
│    remote list/get/compare          pgdb       :15432 (postgres)    │
│    remote dblist → connection list  mysqltest  :15435 (mysql·sha2)  │
│                                     mysqlnative:15436 (mysql·native)│
│                                     cache      :15434 (redis)       │
│        │                                  │                         │
│        │  token check                     │  token check (protocol  │
│        ▼                                  │  field)                 │
│     read db.json (decrypt with DB Key)    ▼                         │
│        │                ┌───────────────────────────────────────────┘
│        │                ▼                                           │
│        │  DB Key (system keyring / env VAULTY_KEEPER_DB_KEY fallback)
└───────┼─────────────────────────────────────────────────────────────┘
        │ TCP (real credentials appear only in host process memory,
        │ never leave the host)
        ▼
┌─────────────────────────────────────────────────────────────────────┐
│ Real databases (Docker containers / intranet hosts / cloud RDS —    │
│ all the same to the tunnel)                                         │
│   PostgreSQL :59918    MySQL :59919    Redis :59920                 │
└─────────────────────────────────────────────────────────────────────┘
```

In one sentence: **AI only knows tokens and tunnel ports; real credentials live only inside the host's `serve` process; between the two, the tunnel swaps credentials during the handshake.**

---

## Figure 2 · What's in Docker right now (test-environment snapshot)

```
Running containers (just "databases for testing"; serve treats them as plain remote DBs)
┌───────────┬──────────────────┬────────────────┬──────────────────────┐
│ container │ image            │ host port      │ real credentials     │
├───────────┼──────────────────┼────────────────┼──────────────────────┤
│ aipg      │ postgres:17.6    │ 127.0.0.1:59918│ app / pgpass / appdb │
│ aimysql8  │ mysql:8.4        │ 127.0.0.1:59919│ sha2user+sha2pass    │
│           │                  │                │ nativeuser+nativepass│
│           │                  │                │ db: shop             │
│ airedis   │ redis:7          │ 127.0.0.1:59920│ :redispass / 0       │
└───────────┴──────────────────┴────────────────┴──────────────────────┘

These URLs are encrypted by vaulty-keeper and registered as "connections" (stored in db.json):
  pgdb       ← postgres://app:pgpass@127.0.0.1:59918/appdb
  mysqltest  ← mysql://sha2user:sha2pass@127.0.0.1:59919/shop
  mysqlnative← mysql://nativeuser:nativepass@127.0.0.1:59919/shop
  cache      ← redis://:redispass@127.0.0.1:59920/0

serve opens one tunnel port per connection; the "address" AI gets (links always carry user+password; the token goes in the user field for PG/MySQL / the AUTH password for Redis, the other field is a placeholder `x`):
  PostgreSQL:   jdbc:postgresql://127.0.0.1:15432/appdb?user=<TOKEN>&password=x
  MySQL(sha2):  127.0.0.1:15435  user=<TOKEN>  any password
  MySQL(native):127.0.0.1:15436 user=<TOKEN>  any password
  Redis:        127.0.0.1:15434  AUTH <TOKEN> (URL form redis://x:<TOKEN>@.../0)
```

Note: **tunnel ports (15432/15435/15436/15434) are fixed** (given via `--port` at registration or auto-assigned and written into db.json); **container host ports (59918/59919/59920) are dynamic** and change every time `scripts/dbtest.sh` reruns. AI only needs the tunnel ports, never the container ports.

---

## Figure 3 · Where the real account/password lives (storage chain)

```
① Register: echo 'postgres://app:pgpass@...' | vaulty-keeper db add pgdb
             │  URL read from stdin, never a CLI arg → never in shell history / ps
             ▼
Real URL (with account + password)
   │  AES-256-GCM encrypted (key = DB Key, independent of snapshot/sensitive keys)
   ▼
db.json (ciphertext on disk, mode 0600, no plaintext) ◄── a leaked snapshot/sensitive key can't decrypt this
   │
   │  on serve start: get DB Key from Keychain (or env VAULTY_KEEPER_DB_KEY) → decrypt URL
   ▼
URL in process memory (lives only inside the host's serve process)
   │  per connection: token check → connect to the real DB with real credentials → inject during handshake → pure byte forwarding
   ▼
Real database
```

Why the layering matters:
| Layer | Protects against |
|---|---|
| db.json holds only ciphertext + 0600 | disk / backups / accidental file sharing leak nothing |
| DB Key independent of other keys | a leaked snapshot key ≠ leaked DB credentials |
| DB Key in the system keyring | other users / other machines / accidental plaintext |
| Real credentials only in host memory | logs, replies, clients, AI environments never see them |
| token gating | third parties without a token (fallback when 0.0.0.0 is exposed on a LAN). Tokens are **per-connection** (128-bit random, generated by `db add`, encrypted to disk alongside the URL); `db regen <name>|--all` rotates them and old tokens die immediately; legacy connections without their own token fall back to the global bridge token (same as the mask bridge) |
| tunnel switch | tunnels are **on by default**; `db on/off <name>|--all` turns a connection's tunnel off (port stops listening) / back on, state persisted in db.json, serve hot-reloads every 2 s; unneeded exposure can be closed at any time |

---

## Figure 4 · Auth injection for three DBs: clients send only a token, the tunnel swaps in real credentials

```
PostgreSQL :15432           MySQL :15435/15436          Redis :15434
┌──────────────┐           ┌──────────────────┐        ┌──────────────┐
│ psql/DBeaver │           │ mysql/DBeaver    │        │ redis-cli /  │
│ user=<TOKEN> │           │ username=<TOKEN> │        │ Redis Insight│
│ empty pass   │           │ any password     │        │ AUTH <TOKEN> │
└──────┬───────┘           └────────┬─────────┘        └──────┬───────┘
       │ fake server lets through  │ fake server checks      │ first command
       │ (AuthenticationOk,        │ username=token          │ must be AUTH
       │  trust style)             │ then replies OK         │ token, then
       ▼                           ▼                         ▼ +OK
┌──────────────────────────────────────────────────────────────────────┐
│  serve connects to the real DB with the decrypted URL from db.json   │
│  (real account/password appear only at this step)                    │
│                                                                      │
│  PG   : real user/password complete SCRAM-SHA-256 / md5 / cleartext  │
│  MySQL: auth-reply swap (mysql_native_password / caching_sha2 + RSA) │
│  Redis: relays AUTH <real password> (+ SELECT db number)             │
└──────────────────────────────────────────────────────────────────────┘
       │ once both sides authenticate → splice (pure byte forwarding,
       ▼ no more protocol parsing)
Real database
```

Key point: the **fake server** (client → tunnel) and the **real client** (tunnel → database) are two independent handshakes; the bridge in between is each protocol's handler in `internal/dbproxy`.

---

## Figure 5 · Security boundary: what AI can vs cannot see

```
AI / inside the container can see                     AI can NEVER see
──────────────────────────────────────────            ─────────────────────────
✔ connection name / type / tunnel port (db list)      ✘ real URL (address / account / password)
✔ its own tunnel token (per-connection; legacy falls  ✘ db.json ciphertext contents
  back to bridge token)                               ✘ DB Key or any key
✔ query results (that's the data AI is meant to get)  ✘ plaintext exits (reveal/export etc. TTY-only)
✔ audit log "success / rejected" lines (no SQL/creds) ✘ any plaintext intermediate state outside serve
✔ free queries on tunnel ports with native clients

Defense chain (layered, against "deliberately adversarial AI"):
  ① container isolation: AI goes into Docker, can't touch ~/.vaulty, the keyring, or real credentials
  ② token gating: a third party without a token is cut off at the tunnel port
  ③ credentials never leave the host: real account/password only in serve process memory
  ④ audit: every success/reject is logged (time, source IP, connection name)
  ⑤ mis-flag / plaintext gates: plaintext commands work only on the user's own TTY
```

---

## Figure 6 · One full query, step by step (DBeaver querying PG as an example)

```
DBeaver                     serve(host)                    real PG
   │ jdbc → 127.0.0.1:15432  │                              │
   │ user=<TOKEN>            │                              │
   ├────────────────────────▶│                              │
   │                         │ check user==token?           │
   │                         │ reply AuthenticationOk       │
   │◀────────────────────────┤ (trust style, let through)   │
   │                         │ connect 127.0.0.1:59918      │
   │                         │ SCRAM with app/pgpass        │
   │                         ├─────────────────────────────▶│
   │                         │◀──────── AuthenticationOk ───┤
   │                         │ splice: both sides become a  │
   │                         │ pure byte channel            │
   │ SELECT * FROM t ───────▶│─────────────────────────────▶│
   │◀──────── rows ──────────│◀──────────── rows ───────────┤
   │   (DBeaver never learns │                              │
   │    the real account is  │                              │
   │    app)                 │                              │
```

---

*Code references: `internal/dbproxy/tunnel.go` (tunnel framework / audit), `store.go` (encrypted db.json storage), `postgres.go` / `mysql.go` / `redis.go` (auth injection for the three protocols), `internal/cli/db.go` (commands), `scripts/dbtest.sh` (Docker test environment).*

---

## Part 2 · Core flows, walkthrough (vertical diagrams)

> These give one annotation per step — good for following along end to end.

### Flow 1 · The life of the real credentials (register → store → run → gone)

```
① register: echo 'postgres://app:pgpass@...' | vaulty-keeper db add pgdb
   │ URL via stdin, never a CLI arg → never in ps / shell history
   ▼
② encrypt to disk: AES-256-GCM (key = DB Key) ◄── DB Key independent of snapshot/sensitive keys
   ▼
③ db.json (0600, only url_cipher + nonce) ◄── no plaintext on disk; accidental share/backup/rsync leaks nothing
   │
   ▼
④ serve start: get DB Key from Keychain (or env) → decrypt into process memory
   │ the real URL now lives only in host memory
   ▼
⑤ per incoming connection: token check → connect to the real DB with real credentials → inject during handshake → pure byte forwarding
   │ clients / logs / replies never see the URL
   ▼
⑥ used then discarded: process exits, connection closes → the URL in memory dies with it
```

### Flow 2 · The full lifecycle of one tunneled query (Redis as an example)

```
client redis-cli -a $TOKEN -p 15434
   │ ① send AUTH <token>
   ▼
serve (cache tunnel)
   │ ② compare token?
   │    no → -ERR authentication required (reject + audit log)
   ▼ yes
   │ ③ connect to the real redis with the URL decrypted from db.json (127.0.0.1:59920)
   │ ④ relay AUTH <real password> ◄── the real password appears only on this host-internal leg
   ▼
real Redis
   │ ⑤ reply +OK
   ▼
serve
   │ ⑥ relay +OK back to the client ◄── the client thinks its token passed; it never learns the real password
   ▼
client
   │ ⑦ from here on, pure byte forwarding (splice); the tunnel stops parsing the protocol
   ▼
client ⇄ real Redis (PING / GET / SET ... results pass straight through)
```

### Flow 3 · Ways an AI might try to get the real password (each one is blocked)

```
Way 1 make the tunnel send the password back?
   serve only replies +OK / -ERR / query results ◄── auth exchange happens host-side; the client never sees it
   result: blocked

Way 2 query SQL for password plaintext?
   SELECT ...password... ◄── no SQL returns password plaintext
   result: blocked (hashes would need brute force; a restricted account can't even read hashes)

Way 3 read db.json?
   the file isn't in the container ◄── not mounted; on the host it's 0600 ciphertext
   result: blocked

Way 4 read the key / process memory?
   no keys in the container ◄── separate VM (Docker isolation); same-host same-account is not defended (trust boundary)
   result: blocked under Docker; same-host relies on the "AI doesn't actively read" convention
```

### Flow 4 · Key hierarchy (who protects what)

```
system keyring (macOS Keychain / Windows Credential Manager / Linux Secret Service)
   │
   ├─ apollo snapshot key ───────── encrypts → Apollo snapshots' "non-sensitive values"
   ├─ sensitive key ─────────────── encrypts → snapshots' "sensitive values"
   └─ db database key (VAULTY_KEEPER_DB_KEY) ─ encrypts → db.json's "real database URLs"
        │
        ▼ impact of an isolated leak
   apollo key leaked   → can decrypt non-sensitive snapshot values, but not sensitive values or db.json
   sensitive key leaked→ can decrypt sensitive values, but not db.json
   db key leaked       → can decrypt database URLs (most valuable, hence separate)
```

### Flow 5 · serve startup sequence

```
vaulty-keeper serve --addr 0.0.0.0:8972
   │ ① generate 128-bit random global bridge token → write ~/.vaulty/bridge-token (0600) + print
   │    (for the mask bridge /api; per-connection tunnel tokens are generated by db add, rotated by db regen)
   ▼
   │ ② read db.json (decrypt with DB Key) → open one TCP tunnel per connection
   │    pgdb :15432 / mysqltest :15435 / mysqlnative :15436 / cache :15434
   ▼
   │ ③ start the HTTP mask bridge :8972 (/api/* all require token + failure rate limiting)
   ▼
ready: 4 tunnels + 1 bridge, waiting for connections
   │ each connection → audit log (authenticated / invalid token, no SQL/credentials)
   ▼
Ctrl-C / exit → tunnels and bridge close; the real URLs in memory die
```

### Flow 6 · Apollo + Docker: AI reads / compares config inside a container

```
Host side (prepared first; AI never sees these):
  vaulty-keeper apollo init / sensitive init    ← snapshot key + sensitive key into Keychain
  vaulty-keeper apollo import prod.txt --app-id xx
       │ plaintext appears only at the moment you import; everything after is encrypted to disk
       ▼
  ~/.vaulty/apollo/prod__xx.json (0600 ciphertext) ◄── no plaintext on disk, not mounted into the container
       ▼
  vaulty-keeper serve --addr 0.0.0.0:8970        ← host holds the keys, always returns masks only
       │
       ▼
Container side (AI's view; every operation goes through the bridge):
  vaulty-keeper remote list prod --appid xx
       │ ① AI asks serve: "what keys does prod have?" (with token)
       ▼
  serve
       │ ② decrypt the snapshot with the Keychain key → compute 「mask + length + fingerprint」 per value
       ▼
  AI gets:  APP_NAME    = *** (5 chars)  [51650fd5fb747230]
            DB_PASSWORD = *** (11 chars) [afd76e19e7393961]
       │ no plaintext, but length + fingerprint are enough
       ▼
comparison (AI's core job):
  vaulty-keeper remote compare prod test --appid xx --appid-to xx
       ▼
  ~ DB_PASSWORD: *** (11 chars) [afd76e19] -> *** (11 chars) [b5a112e5]
       │ different fingerprint = different content (even at the same length)
       ▼
  AI's conclusion: DB_PASSWORD differs between environments, LOG_LEVEL too → report to the human
       │
       ▼
boundary: remote is read-only ◄── the bridge has no set/unset/import endpoints;
      a containerized AI can only ever "mask-read + compare"; changes stay on the host CLI/UI
```

---

## Appendix · Docker's two roles in this project

The Docker files in this project serve **two independent purposes**; don't mix them up:

### Role A: test databases (`scripts/dbtest.sh`)
Spins up PG/MySQL/Redis **database containers** as targets for the tunnel. Purely for local verification; they don't participate in any vaulty-keeper logic — serve treats them as ordinary remote DBs.

### Role B: isolating the AI agent (`Dockerfile` + `docker-compose.yml` + `docker/agent-entrypoint.sh`)
Puts the **AI itself** inside a container so it can't reach the host's keys/ciphertext — the only reliable defense against a "deliberately adversarial" AI.

```
Dockerfile (two stages)
  stage 1 golang → go build produces the vaulty-keeper binary
  stage 2 node (the agent CLI is an npm package) + git + non-root user agent

docker-compose.yml (isolation essentials)
  cap_drop: ALL             container has no kernel privileges
  no-new-privileges         no privilege escalation
  volumes: mount project dir only    no ~/.vaulty / Keychain / ~/.ssh / docker.sock
  env: only BRIDGE_ADDR/TOKEN (no keys)
  extra_hosts: host.docker.internal:host-gateway   Linux compatible (no effect on Docker Desktop)
  volumes: agent-home:/home/agent                   persistent CLI/history (survives rebuild)

agent-entrypoint.sh
  auto npm-installs codex/claude/opencode when VAULTY_KEEPER_INSTALL_AGENTS is set, then opens a shell
```

The container has both capabilities at once (both via the host's serve):
- **Apollo mask reads**: `vaulty-keeper remote list|get|compare` → only `*** (n chars)` + fingerprint
- **DB tunnels**: `db list` gives tunnel ports → psql/mysql/redis-cli connect to `host.docker.internal:port`, tunnel token as username/AUTH (per-connection, printed by `db connect`; legacy connections fall back to the bridge token)

**Boundary reminder**: Docker is strong isolation against "most AI actively grabbing keys", but not absolute (the daemon is a root service; container escape is a real attack surface). For very high threat levels, upgrade to a separate account / VM / cloud sandbox (README "Alternatives to Docker").

### When to use which

| Scenario | Use |
|---|---|
| Local DB-tunnel verification | `scripts/dbtest.sh` (needs Docker Desktop locally) |
| Defend against an AI that "actively reads keys" | `docker compose up -d` (host runs `serve --addr 0.0.0.0:8970` first + exports the token) |
| Only defend against a "well-behaved" AI | no Docker; serve directly on the host |
| Real isolation without Docker | separate macOS account / VM (README "Alternatives to Docker") |
| Very high threat / compliance audit | separate account / VM / cloud sandbox (one more layer on top of Docker) |
| Production / cloud | no Docker: the tunnel is pure TCP, the DB can be cloud RDS, AI goes into any isolated domain |

### How the two roles connect

```
host: vaulty-keeper serve (mask bridge + DB tunnels)  ← runs bare, unrelated to Docker
in Docker: [role-B agent container: AI] --db list--> [tunnel port] --token--> real DB
                                       (real DB = role-A container / intranet / cloud RDS, any)
```

---

## Appendix · What AI can actually get through the tunnel (measured)

Key principle: **a tunnel hands AI the full permissions of the registered account**. Whatever the registered URL's account can do is the ceiling of what AI can do through the tunnel.

| What it tries to get | Possible? | Notes |
|---|---|---|
| Password plaintext | ❌ no | auth exchange completes host-side; the client only sees "success/failure"; no SQL path returns password plaintext; logs/replies verified to contain no passwords |
| Password hashes | ⚠️ depends on account | register a high-privilege account (e.g. superuser) → can read `pg_authid`/`mysql.user` for hashes (recovering still needs brute force); register a restricted read-only account → `permission denied` ✅ |
| Real account name | ⚠️ always visible | `SELECT current_user` / `CURRENT_USER()` return the session owner — an inherent property of "giving a real session", can't be hidden. Mitigation: register a **dedicated read-only account** (e.g. `app_ro`) whose name is not sensitive |
| Real address | ⚠️ partially visible | `inet_server_addr()`/`inet_server_port()` return the real server address (a container-internal IP in the Docker case); hostname/mapped ports never appear in config or logs |
| Write/delete data | ⚠️ depends on account | read-only account → rejected; a writable account → allowed (the proxy does not enforce read-only) |

Measured (restricted account `app_ro`, SELECT only):
```
SELECT current_user            → app_ro            (account name visible)
SELECT rolpassword FROM pg_authid → permission denied for table pg_authid   ✅
INSERT INTO t ...                → permission denied for table t             ✅
SELECT count(*) FROM t           → 2                (normal queries unaffected)
```

**Conclusion**: the password (plaintext) never leaves the host under any circumstance; whether hashes/write permissions can be obtained is entirely decided by the account you register. So the **precondition for safe use** is: register a **dedicated, read-only, least-privilege** account with `vaulty-keeper db add` instead of registering a high-privilege account.

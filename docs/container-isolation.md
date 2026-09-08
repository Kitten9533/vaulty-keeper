# Container Isolation (against deliberately hostile AI, macOS / Windows)

> [中文](container-isolation.zh-CN.md) | English

How to isolate an AI agent from host keys and ciphertext with the provided Docker configuration, plus Docker-free alternatives. Install and quick start live in the [README](../README.md); the canonical security boundary lives in the [security model](security-model.md).

Masking and TTY guards do not constrain a hostile same-user process. Isolation must put keys and ciphertext outside the agent's reach; the provided Docker configuration is one starting point (Docker Desktop uses a Linux VM). It does not enforce network egress restrictions or prevent access to authorized database contents. See the [security model](security-model.md).

```
[Docker container: codex / claude / opencode / pi]
      │  vaulty-keeper remote list|get|compare (masked only)
      ▼
[Host: holds the keys]
      vaulty-keeper serve --addr 0.0.0.0:8970   ← snapshot API masks values; DB tunnels return data
      ▼
      OS secret store + ~/.vaulty/ (not mounted by the supplied compose file)
```

## Host side: start the masking proxy

Human host terminal 1: leave `serve` running. Use a trusted, firewall-restricted interface; the following binds all interfaces. Register databases before starting it if tunnels are needed.

```sh
vaulty-keeper serve --addr 0.0.0.0:8970     # prints token and writes ~/.vaulty/bridge-token
```

- Snapshot API values are always masked, **even for keys marked safe with `set --plain`**. JSON list/compare responses include length/fingerprint metadata; `remote get` prints only the mask.
- Every `/api` endpoint requires the token (written 0600 to `~/.vaulty/bridge-token`); failed checks add 50 ms per failure, capped at 2 seconds. This token also authorizes new and legacy PG/MySQL/Redis tunnel connections, so it is not a harmless metadata token.
- `0.0.0.0` lets Docker reach the host but also exposes plaintext HTTP and tunnel listeners to reachable networks. Token checks do not encrypt transport or make LAN exposure safe; restrict access with network controls. Use `127.0.0.1` for host-only access.

## Container side: agent isolation domain

Human host terminal 2, from the repository: choose a project directory containing no secret files. The token handoff below is an authorization decision; the entrypoint prints only a `<set>`/`<unset>` marker, never the token itself. The image builds Go inside Docker and includes neither optional agent CLIs nor database clients by default.

```sh
# build from source inside Docker; no host make build needed
docker build -t vaulty-keeper-agent:local .

# human host handoff: grants snapshot metadata and PG/MySQL/Redis database access
export VAULTY_KEEPER_BRIDGE_TOKEN="$(cat ~/.vaulty/bridge-token)"
export VAULTY_KEEPER_PROJECT_DIR="$PWD"   # current repository; review contents before mounting
docker compose up -d

# usable without installing an agent CLI; lists the host bridge's snapshots
docker compose exec agent vaulty-keeper remote list
docker compose exec agent vaulty-keeper remote dblist
```

To use `codex`, set `VAULTY_KEEPER_INSTALL_AGENTS='@openai/codex'` before creating the container and supply the CLI's own login/config separately, then run `docker compose exec agent codex`. Native DB commands additionally require `psql`, MySQL `mysql`, `redis-cli` or `mongosh` in the client environment. Generate tunnel commands on the host with `db connect <name> --container`, then deliver only the authorized tunnel credentials, never host encryption keys.

Isolation essentials (already built into `docker-compose.yml`):

- **No explicit mounts** of `~/.vaulty`, the OS secret store, `~/.ssh`, or the Docker socket. Do not defeat this by choosing a project directory containing those files or passing encryption keys via environment variables.
- Non-root user + `cap_drop: ALL` + `no-new-privileges`
- `VAULTY_KEEPER_BRIDGE_ADDR` / `VAULTY_KEEPER_BRIDGE_TOKEN` configure bridge access; compose does **not** make it the container's only network destination.
- Agent CLIs: `VAULTY_KEEPER_INSTALL_AGENTS='@openai/codex @anthropic-ai/claude-code opencode-ai'` (npm-installed into the user dir on container start)
- **Persistence**: the `agent-home` named volume mounts at `/home/agent`, so installed CLIs and session history survive rebuilds. Its actual name depends on the Compose project; remove only that verified volume after detaching its containers if deliberately resetting history and installed tools.
- **Linux**: compose includes `extra_hosts: host.docker.internal:host-gateway` (macOS/Windows Docker Desktop already provides it, no effect)

## What isolation does and does not do

When mounts, privileges and credentials are kept within these boundaries, the container has no direct path to host key storage or snapshot files. That does not stop it reading mounted project secrets, using its bridge token on PG/MySQL/Redis tunnels, querying business data or sending reachable data over the network. These are separate permissions, not encryption failures.

**Docker itself is not absolute isolation**: reduced capabilities and `no-new-privileges` reduce attack surface, but daemon privileges and container escape remain concerns. Stronger threats require separately evaluated account/VM/sandbox and network controls; no configuration here establishes a measured prevention rate.

## Windows users

- Same compose/image; Windows Docker Desktop is WSL2 underneath, `host.docker.internal` works the same
- Keys live in **Windows Credential Manager** (`vaulty-keeper apollo init` / `sensitive init` adapt automatically; no `security` command needed)
- Plaintext CLI guards check stdin console status (`GetConsoleMode` on Windows), not human identity; there is no interactive menu. Non-TTY local reads mask unmarked values, while bridge snapshot reads always mask values.

## Alternatives to Docker

`serve` + `remote` are not tied to Docker; the isolation domain can be any environment that **cannot touch keys or ciphertext**:

**① Run locally (no isolation, defends against "well-behaved" AI)**

In terminal 1, run `vaulty-keeper serve --addr 127.0.0.1:8970` and leave it running. In terminal 2 on the same host, run:

```sh
export VAULTY_KEEPER_BRIDGE_ADDR=http://127.0.0.1:8970
vaulty-keeper remote list   # uses the host's token file unless an env override is set
```

When the AI shares your account, defense rests on masking + TTY gating; no protection against an AI that actively reads keys.

**② Separate macOS account (real isolation, Docker alternative)**

Create a standard, non-admin `ai` account using macOS account settings and review filesystem access; do not put its real password in a shell command. With `codex` separately installed, a human host can explicitly delegate the bridge token:

```sh
sudo -u ai env VAULTY_KEEPER_BRIDGE_ADDR=http://127.0.0.1:8970 \
  VAULTY_KEEPER_BRIDGE_TOKEN="$(cat ~/.vaulty/bridge-token)" codex
```

The separate account should have no host keys and no read access to the host's 0700 `~/.vaulty/`. Verify permissions and other shared files; the delegated token still grants PG/MySQL/Redis access. You must manage account credentials, agent installation and filesystem permissions.

**③ Remote machine / WSL2**

Put the agent on a separately controlled machine/VM and restrict bridge/tunnel reachability to a trusted network. WSL2 alone is not a guarantee of separation from Windows host files. Token-gated snapshot masking does not protect plaintext transport or redact database results.

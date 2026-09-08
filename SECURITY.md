# Security Policy for vaulty-keeper

> [中文](SECURITY.zh-CN.md) | English

vaulty-keeper is a personal AI toolbox: encrypted Apollo snapshot storage,
AES-256-GCM helpers and local database tunnels with a loopback-only web UI.
The canonical security reference is the [security model](docs/security-model.md)
(English) / [中文版](docs/security-model.zh-CN.md) — trust layers, plaintext
lifecycle, token lifecycle, container limits, protocol limits and verification
status. This file is only the entry point and reporting policy.

## Supported versions

The latest tagged release is supported. Security fixes are applied to the
current source tree and released as a new tag; no LTS or backport commitment is
made for older releases.

## Reporting a vulnerability

Do **not** open a public issue for security problems. Report privately:

- Open a [private security advisory](https://github.com/Kitten9533/vaulty-keeper/security/advisories/new)
  on GitHub, or
- Email the repository owner with a description, affected version, and (if
  possible) a minimal reproduction.

Please do not include real secrets, real DSNs or key material in any report.
You will be acknowledged and asked about public disclosure timing.

## Security boundaries at a glance

| Area | What is guaranteed |
|---|---|
| At-rest encryption | Stored snapshot values and registered database URLs/tunnel tokens are AES-256-GCM encrypted (0600). Not covered: the plaintext AES key/IV JSON, import sources, exports/downloads, editor temp files, shell history, terminal output, clipboard, process memory. |
| Same-user processes | OS key storage and 0600 files do **not** stop a same-user process (including an AI shell) from reading keys. Do not place keys, DSNs or ciphertext where an untrusted agent can reach them. |
| Plaintext exits | `reveal`/`export`/`edit`/`aes decrypt`/`db show` require stdin TTY; TTY is an accident-prevention gate, not proof of a human. |
| Web UI | Loopback-only; non-GET operations require the UI token; explicit plaintext routes additionally require `--allow-plaintext`. |
| DB tunnels | Tokens gate access; PG/MySQL/Redis accept a global or dedicated token, MongoDB only its dedicated token. Tokens do not encrypt client-to-proxy transport. |

These are summaries. Read the [security model](docs/security-model.md) before
making trust decisions, and never rely on a documentation claim as test or
production evidence.

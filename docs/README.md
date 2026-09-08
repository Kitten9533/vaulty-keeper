# vaulty-keeper Documentation Index

> [中文](README.zh-CN.md) | English

Entry points and current-owner map for `vaulty-keeper` docs. Keep this index thin: each item below is the single current home for its subject. Historical implementation records are archived under git tag `docs-superpowers-archive` and are not listed here.

## Entry points

| Doc | Role |
|---|---|
| [../README.md](../README.md) · [中文](../README.zh-CN.md) | Install, quick start and command reference (human-facing). |
| [../CONTRIBUTING.md](../CONTRIBUTING.md) · [中文](../CONTRIBUTING.zh-CN.md) | How to build, test and contribute; documentation conventions. |
| [../SECURITY.md](../SECURITY.md) · [中文](../SECURITY.zh-CN.md) | Security reporting policy and boundary summary; links the security model. |
| [../AGENTS.md](../AGENTS.md) | Agent operating constraints and checks (single-entry Chinese file). |

## Current guides (English + Chinese pairs)

| Guide | Covers |
|---|---|
| [security-model](security-model.md) · [中文](security-model.zh-CN.md) | **Canonical security boundary**: trust layers, plaintext lifecycle, tokens, container limits, protocol limits, verification status. |
| [cli-reference](cli-reference.md) · [中文](cli-reference.zh-CN.md) | Complete command reference: apollo snapshot tool, aes helpers, misc commands. |
| [apollo-snapshot-guide](apollo-snapshot-guide.md) · [中文](apollo-snapshot-guide.zh-CN.md) | Snapshot implementation: encrypted file layout, dual-key design, sensitive detection, masking/fingerprints, explicit safe allowlist, tested examples. |
| [ui-guide](ui-guide.md) · [中文](ui-guide.zh-CN.md) | Web UI: navigation, fields, confirmation flows, AES layers, database tunnels in the UI. |
| [db-proxy-architecture](db-proxy-architecture.md) · [中文](db-proxy-architecture.zh-CN.md) | DB tunnel architecture for PG/MySQL/Redis: what runs where, credential injection, sequence. |
| [db-proxy-examples](db-proxy-examples.md) · [中文](db-proxy-examples.zh-CN.md) | DB usage examples and synthetic fixtures (source-checked, not re-executed during doc work). |
| [mongodb-tunnel-guide](mongodb-tunnel-guide.md) · [中文](mongodb-tunnel-guide.zh-CN.md) | MongoDB 8 tunnel: exact URL options, security limits, troubleshooting, dated verification matrix. |
| [container-isolation](container-isolation.md) · [中文](container-isolation.zh-CN.md) | Docker/agent isolation from host keys and ciphertext, plus Docker-free alternatives. |

## Principles

- One current home per subject. The security model lives only in `security-model.md`; guides link it instead of duplicating it.
- Everything user-facing is English-default with a `.zh-CN.md` pair and a language-switch link at the top.
- README owns install/getting-started, AGENTS owns agent constraints/checks, guides own current usage.
- A document is historical when its work is superseded; archive it under a git tag instead of keeping it in the tree as if current.

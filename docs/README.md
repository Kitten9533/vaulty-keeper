# vaulty-keeper Documentation Index

> [中文](README.zh-CN.md) | English

Entry points and current-owner map for `vaulty-keeper` docs. Keep this index thin: each item below is the single current home for its subject; historical records stay dated and point at their successor.

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
| [apollo-snapshot-guide](apollo-snapshot-guide.md) · [中文](apollo-snapshot-guide.zh-CN.md) | Snapshot implementation: encrypted file layout, dual-key design, sensitive detection, masking/fingerprints, explicit safe allowlist, tested examples. |
| [ui-guide](ui-guide.md) · [中文](ui-guide.zh-CN.md) | Web UI: navigation, fields, confirmation flows, AES layers, database tunnels in the UI. |
| [db-proxy-architecture](db-proxy-architecture.md) · [中文](db-proxy-architecture.zh-CN.md) | DB tunnel architecture for PG/MySQL/Redis: what runs where, credential injection, sequence. |
| [db-proxy-examples](db-proxy-examples.md) · [中文](db-proxy-examples.zh-CN.md) | DB usage examples and synthetic fixtures (source-checked, not re-executed during doc work). |
| [mongodb-tunnel-guide](mongodb-tunnel-guide.md) · [中文](mongodb-tunnel-guide.zh-CN.md) | MongoDB 8 tunnel: exact URL options, security limits, troubleshooting, dated verification matrix. |

## Historical records (`superpowers/`, English + Chinese pairs)

Implementation designs/plans kept as dated evidence. They are **not** current instructions and are generally **not re-runnable**; each links its successor guide where one exists. Do not replay commands from them against real data.

| Record | Status / successor |
|---|---|
| [specs/2026-08-25-claude-style-web-ui-design](superpowers/specs/2026-08-25-claude-style-web-ui-design.md) | Historical source for the first UI; successor: [ui-guide](ui-guide.md). |
| [plans/2026-08-25-local-web-ui](superpowers/plans/2026-08-25-local-web-ui.md) | Historical execution record; not re-runnable; successor: [ui-guide](ui-guide.md). |
| [specs/2026-08-25-full-ui-migration-design](superpowers/specs/2026-08-25-full-ui-migration-design.md) · [plans/2026-08-25-full-ui-migration](superpowers/plans/2026-08-25-full-ui-migration.md) | UI migration decisions/history; successor: [ui-guide](ui-guide.md). |
| [specs/2026-08-25-env-appid-and-snapshot-delete](superpowers/specs/2026-08-25-env-appid-and-snapshot-delete.md) | Implemented env/AppID/delete decisions; still-useful naming/identity constraints. |
| [specs/2026-08-31-db-proxy-tunnel-design](superpowers/specs/2026-08-31-db-proxy-tunnel-design.md) · [plans/2026-08-31-db-proxy-tunnel](superpowers/plans/2026-08-31-db-proxy-tunnel.md) | Historical three-protocol tunnel design; successor: [db-proxy-architecture](db-proxy-architecture.md) / [mongodb-tunnel-guide](mongodb-tunnel-guide.md). |
| [specs/2026-09-07-mongodb-tunnel-design](superpowers/specs/2026-09-07-mongodb-tunnel-design.md) · [plans/2026-09-07-mongodb-tunnel](superpowers/plans/2026-09-07-mongodb-tunnel.md) | Accepted Mongo design decisions + dated execution evidence; current behavior and matrix: [mongodb-tunnel-guide](mongodb-tunnel-guide.md). |

## Principles

- One current home per subject. The security model lives only in `security-model.md`; guides link it instead of duplicating it.
- Everything user-facing is English-default with a `.zh-CN.md` pair and a language-switch link at the top.
- README owns install/getting-started, AGENTS owns agent constraints/checks, guides own current usage, records own dated evidence.
- A document is historical when its work is superseded; it stays for evidence and links its successor rather than being silently deleted or rewritten as if current.

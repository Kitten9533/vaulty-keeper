# vaulty-keeper Apollo 快照 · 使用示例与实现讲解

> 讲解"从 Apollo 门户粘贴配置 → 加密落盘 → AI 安全读取"这条链路是怎么实现的：快照文件长什么样、双密钥怎么分工、敏感值怎么识别、AI 为什么只能拿到掩码、以及怎么显式放行/判断一致性。
> 配套：`README.md`（命令全表）、`docs/db-proxy-architecture.md`（数据库隧道图解）。

---

## 图 1 · 总览：一图看懂全链路

```
┌─────────────────────────────────────────────────────────────────────┐
│ Apollo 门户（配置中心）                                               │
│   复制 KEY = value 文本 —— 明文只出现在"你手动粘贴导入"这一刻             │
└──────────────────────┬──────────────────────────────────────────────┘
                       │  vaulty-keeper apollo import prod.txt --name prod --appid merdi
                       ▼
┌─────────────────────────────────────────────────────────────────────┐
│ Host：~/.vaulty/apollo/{env}__{appid}.json   （0600，磁盘无明文）        │
│                                                                     │
│   items：KEY → { enc(AES-256-GCM 密文), nonce(随机), secret }         │
│     secret=false → 快照密钥加密        secret=true → 敏感值密钥加密     │
│   两把密钥都在系统密钥库（Keychain / env VAULTY_KEEPER_*_KEY 兜底）     │
└───────┬──────────────────────────┬────────────────────┬──────────────┘
        │ ① 用户本人 TTY           │ ② AI / 脚本（非 TTY）│ ③ 容器内 AI（隔离域）
        ▼                         ▼                    ▼
┌───────────────────┐  ┌──────────────────────┐  ┌──────────────────────┐
│ get/list/compare  │  │ get/list/compare      │  │ remote list/get/…    │
│ reveal/export/edit│  │  → 掩码 + 长度 + 指纹  │  │  （经 serve 掩码桥，   │
│  → 明文（含敏感值） │  │ 明文命令一律拒绝        │  │   永远只有掩码）      │
└───────────────────┘  └──────────────────────┘  └──────────────────────┘
```

一句话：**配置只在导入/导出那一刻以明文出现，其余时间全是密文；AI/脚本默认只能拿到掩码+长度+指纹，明文出口只在你自己终端可用；容器 AI 要拿任何配置（哪怕掩码），都只有经 serve 那一条路。**

---

## 1 · 一分钟上手

```sh
# ① 首次：两把密钥进系统密钥库（macOS Keychain / Windows 凭据管理器 / Linux Secret Service）
vaulty-keeper apollo init        # 快照密钥（加密非敏感值）
vaulty-keeper sensitive init     # 敏感值密钥（加密敏感值）

# ② 从 Apollo 门户复制 KEY=value 文本，导入加密快照
#    --appid 必填；--name 省略时取文件名；已存在需 --force
vaulty-keeper apollo import prod.txt --name prod --appid merdi
#   imported 4 entries into snapshot "prod" (appid merdi) (~/.vaulty/apollo/prod__merdi.json)

# ③ 列出（默认掩码，AI 友好）
vaulty-keeper apollo list prod --appid merdi --json
#   { "name": "prod", "app_id": "merdi", "items": {
#     "API_SECRET": "*** (12 chars)", "APP_NAME": "*** (5 chars)", ... } }

# ④ 读单个值（非 TTY 下只对标记安全的 key 给明文，见 §6）
vaulty-keeper apollo get prod APP_NAME --appid merdi
```

快照默认存 `~/.vaulty/apollo/`，以「环境名 + AppID」寻址 `{env}__{appid}.json`；旧版无 AppID 的快照是 `{env}.json`，不传 `--appid` 读取。

---

## 2 · 加密落盘长什么样（快照文件结构）

导入上面 4 个值后，`~/.vaulty/apollo/prod__merdi.json`（权限 0600）实际内容：

```json
{
  "meta": {
    "name": "prod",
    "app_id": "merdi",
    "captured_at": "2026-09-03T08:23:26Z"
  },
  "items": {
    "API_SECRET": {
      "enc": "3Xz4BLf8aeoZNCLmmYY0NxSh92J3yPYtYYU=",
      "nonce": "VnBOQYdbGGJ/Qbji",
      "secret": true
    },
    "APP_NAME": {
      "enc": "X/OMnOMPicPpuxoUX1bPVCbuSFYeL4jNTmh/5A==",
      "nonce": "yuRomYvHYPyWEBtg",
      "secret": false
    },
    "REDIS_URI": {
      "enc": "5T6vGBMiX41Jhf06RfW6WxTe3NfsVooxWrC4/nATfacIH591XtuaCK0B1AtglR2fCc1jy5WJLL2WIJzDbcQw",
      "nonce": "i0nCJQycO9R3L+oZ",
      "secret": true
    }
  }
}
```

要点（`internal/apollo/store.go`）：

- **值全部是密文**：每条 value 用 AES-256-GCM 单独加密，`enc` = Base64 密文，`nonce` = 该条独立的随机 nonce（每条不同，相同明文密文也不同）。磁盘上没有任何明文值。
- **`secret` 字段标记加密用哪把钥匙**：`true` = 用敏感值密钥，`false` = 用快照密钥（§3）。
- **`meta.captured_at`** 记录导入时刻（UTC RFC3339）。
- 文件名 `prod__merdi.json` 里 `__` 是分隔符，`{env}__{appid}.json`（`internal/apollo/store.go:88` 的 `FileName`）。

---

## 3 · 双密钥分工：为什么敏感值要单独一把钥匙

两把独立密钥，都在系统密钥库（`internal/apollo/keyring.go`），均可环境变量覆盖：

| 密钥 | Keychain account | 环境变量 | 加密对象 |
|---|---|---|---|
| 快照密钥 | `apollo-snapshot-key` | `VAULTY_KEEPER_APOLLO_KEY` | 非敏感值（secret=false） |
| 敏感值密钥 | `sensitive-key` | `VAULTY_KEEPER_SENSITIVE_KEY` | 敏感值（secret=true） |

安全价值：**快照密钥泄露（比如误发到别处）也解不开敏感值**——敏感值被敏感值密钥加密，而 `apollo init` 和 `sensitive init` 是两次独立的密钥生成。`reveal`/`--reveal` 显示敏感值明文必须同时有敏感值密钥（`internal/app/snapshot.go` 的 `DecryptItem` 按 `secret` 选钥匙）。

> env 覆盖只在无密钥库的环境（如 Linux 无头服务器没有 Secret Service）兜底，属于红线密钥：不要导进 AI 会话、不要放命令行参数。

---

## 4 · 敏感识别：什么会被自动标成 secret

导入时自动判断（`internal/apollo/mask.go` 的 `IsSensitiveKeyValue`），命中任一条 → `secret=true`：

1. **key 名命中**（不区分大小写）：
   `password|passwd|pwd|token|secret|salt|credential|private|access[_-]?key|secret[_-]?key|api[_-]?key`
   → `API_SECRET`、`CMS_SECRET`、`SENTRY_AUTH_TOKEN`、`MONGODB_URI`… 全中。
2. **带凭据的 URI/DSN**：key 名含 `uri|url|dsn|connection|endpoint|addr|address`，**且**值形如 `scheme://user[:password]@host`
   → `REDIS_URI=redis://:pw@r-abc...:6379/0` 中；纯 URL 不带 `@` 凭据（如 `https://example.com/api`）不中。
3. **JWT**：值形如 `eyJ...` 三段 base64url → `SUPABASE_SERVICE_ROLE_KEY` 这类中。

原则是**宁多掩不漏掩**：自动识别不写回文件（导入后 `secret` 是本次判定结果；`set` 不带 `--plain/--secret` 时保留已有条目的原判定）。

---

## 5 · 反转默认：AI 为什么只能拿到掩码

非 TTY（脚本 / AI）下的输出规则（`internal/cli/cli.go` 的 `maskedFor`）：

- **默认全部掩码**，不靠 key 名猜测——`get`/`list`/`compare` 对**未显式标记安全**的 key 一律输出 `*** (n chars)`（`MaskWithLen`，保留长度信息）。
- 只有 `set --plain` / `mark --plain` **显式标记为安全**的 key 才输出明文。
- 明文出口（`reveal`/`export`/`edit`/`list|compare --reveal`/`aes decrypt`）**非交互终端一律拒绝，加 `--yes` 也无法放行**——只在用户本人 TTY 可用。

```sh
vaulty-keeper apollo get prod REDIS_URI --appid merdi     # 非 TTY → *** (43 chars)
vaulty-keeper apollo get prod APP_NAME --appid merdi      # 未放行 → *** (5 chars)
```

TTY 下则是老启发式：敏感名字/内联凭据掩码，`--reveal` 显示明文。

---

## 6 · 显式放行：set --plain / mark --plain

```sh
# set 时直接标记
vaulty-keeper apollo set prod NEXT_PUBLIC_SAFE_FLAG true --plain --appid merdi

# 或对已有 key 只翻标记（不改值）
vaulty-keeper apollo mark prod APP_NAME --plain --appid merdi
vaulty-keeper apollo mark prod APP_NAME --secret --appid merdi   # 撤销放行
```

放行后写回文件的 `safe:true`，非 TTY 的 `get`/`list` 才给明文：

```json
"APP_NAME": { "enc": "...", "nonce": "...", "secret": false, "safe": true }
```

**防误标守卫**（`guardPlainMark`）：`--plain` 命中的 key 名/值看起来是敏感内容时，非 TTY 一律拒绝、TTY 需二次确认——防止误把 `API_SECRET` 标成"安全"漏给 AI。

---

## 7 · 指纹：掩码下怎么判断"两个值是否一致"

掩码只给长度，同长度的不同值看不出区别。`remote compare`/`remote get` 额外给 **HMAC-SHA256 指纹**（8 字节 hex，`internal/apollo/mask.go` 的 `Fingerprint`）：

- 指纹密钥 = 快照密钥：**密钥不泄露时无法离线枚举弱值来匹配指纹**。
- 判断两环境某 key 是否一致：**长度相同 + 指纹相同 → 值相同**，全程不出现明文。

```sh
vaulty-keeper remote compare prod test --appid merdi --appid-to merdi2 --json
#   { "changed": { "REDIS_URI": { "old": {"value":"*** (43 chars)","fingerprint":"3f2a..."},
#                                   "new": {"value":"*** (43 chars)","fingerprint":"9c11..."} } } }
```

指纹不同 → 值不同，即使长度一样。**判断一致性用 compare，不要 get 明文**（这是给 AI 的安全姿势）。

---

## 8 · 导入解析规则（粘贴文本怎么被理解）

`internal/apollo/parser.go` 的 `ParseKV`：

- 每行 `KEY = value`，按**第一个 `=`** 分割，两侧去空格，value 可含 `=`。
- 空行、行首 `#` 的整行（单行/多行注释）跳过。
- **粘连自动拆分**：一行内粘在一起的多个 `KEY = ` 条目自动拆开并告警（如 `A = 1B = 2`），同时避免误拆 URL 查询参数（`...?TOKEN=1` 前的 `?` 不是 glue）。
- key 校验 `[A-Za-z_][A-Za-z0-9_.-]*`，非法行跳过并告警。
- 值两侧成对引号会被剥掉（`"merdi"` ≡ `merdi`）。

---

## 9 · 明文命令：reveal / export / edit（仅你的 TTY）

```sh
vaulty-keeper apollo reveal prod --appid merdi API_SECRET      # 单个敏感值明文
vaulty-keeper apollo reveal prod --appid merdi --json          # 多个 key 的 JSON
vaulty-keeper apollo export prod --appid merdi                 # 全量 KEY = value（粘贴回 Apollo）
vaulty-keeper apollo export prod --appid merdi --copy          # 直接进剪贴板
vaulty-keeper apollo edit prod --appid merdi                   # $EDITOR 打开明文，保存后自动重新加密
```

- 全部**只在交互式终端可用**；脚本/AI 一律拒绝（`internal/cli/cli.go` 各命令入口的 `isTerminal()` 门禁）。
- `edit` 流程 = `Export` 明文到临时文件（0600）→ 编辑器 → `ParseKV` 解析 → 整份重新加密写回（`app.EditLoad`/`EditApply`），编辑时不用手动管两把钥匙。

---

## 10 · 常见场景速查

| 想干什么 | 命令 |
|---|---|
| 从 Apollo 复制配置落地 | `apollo import prod.txt --name prod --appid merdi` |
| 列出全部快照 | `apollo list` |
| AI 读某个值（掩码） | `apollo get prod KEY --appid merdi` |
| AI 判断两环境是否一致 | `apollo compare prod test --appid merdi --appid-to merdi2 --json` |
| 给 AI 放行一个确定安全的 key | `set prod KEY v --plain` / `mark prod KEY --plain` |
| 撤销放行 | `mark prod KEY --secret` |
| 看敏感值明文（自己 TTY） | `apollo reveal prod KEY --appid merdi` |
| 整份导出/编辑 | `apollo export prod --appid merdi` / `apollo edit prod --appid merdi` |
| 报错"快照不存在" | 看提示里的**相近快照**（同 env 的其他 appid），多半是 `--appid` 拼错 |

# vaulty-keeper Apollo 快照 · 使用示例与实现讲解

> 中文 | [English](apollo-snapshot-guide.md)
>
> 说明快照存储、密钥选择、分类、显式明文放行和掩码对比。这是操作指南，不是同用户进程无法取得明文的保证。
> 配套：[命令参考](../README.zh-CN.md)、[UI 指南](ui-guide.zh-CN.md)、[安全模型](security-model.zh-CN.md)、[文档索引](README.md)。

---

## 图 1 · 总览：一图看懂全链路

```text
Apollo 文本 / 输入文件（明文）
  -> import -> 快照 JSON（value 加密；元数据可读）
                 secret=false：快照密钥
                 secret=true：敏感值密钥（新加密数据）
                 safe=true：允许普通非 TTY/UI 读取明文

宿主 CLI，stdin 是 TTY：get 输出明文；list/compare 使用启发式
宿主 CLI，非 TTY：普通读取未标 safe 就掩码；受限明文出口拒绝
UI GET：掩码或 safe 明文；DB connect GET 还返回访问 token
隔离客户端经 serve：快照 value 始终掩码
```

**加密落盘保证针对已存储的快照 value，不覆盖所有文件与输出。** 导入文件、编辑临时文件、导出/下载、剪贴板、终端输出和进程内存都可能包含明文；独立的 AES key/IV 列表是受文件权限保护的明文 JSON。TTY 检查是防误操作门禁，不验证真人身份或保护 stdout。Agent 不得调用明文出口或获取密钥。`serve` 掩码快照值，但不强制成为容器唯一网络出口；其全局 token 还可访问 PostgreSQL/MySQL/Redis 隧道。共同信任边界见[安全模型](security-model.zh-CN.md)。

---

## 1 · 用合成数据开始

前提：按 [README](../README.zh-CN.md) 安装/构建 `vaulty-keeper`。在人工控制的终端中只初始化缺失的密钥；重新生成前先检查环境覆盖（§3）。用本地编辑器准备以下两份完整的**合成**文件。不要把真实凭据粘贴到命令参数或 shell history。

`prod.txt`：

```properties
APP_NAME = merdi
API_SECRET = demo-token-a
REDIS_URI = redis://:demo@localhost:6379/0
```

`test.txt`：

```properties
APP_NAME = merdi
API_SECRET = demo-token-b
REDIS_URI = redis://:demo@localhost:6379/0
```

```sh
# ① 首次：两把密钥进系统密钥库（macOS Keychain / Windows 凭据管理器 / Linux Secret Service）
vaulty-keeper apollo init        # 快照密钥（加密非敏感值）
vaulty-keeper sensitive init     # 敏感值密钥（加密敏感值）

# ② 导入两份夹具；使用未占用的环境/AppID 组合
#    --appid 必填；--name 省略时取文件名
vaulty-keeper apollo import prod.txt --name prod --appid merdi
vaulty-keeper apollo import test.txt --name test --appid merdi2

# ③ 非 TTY 普通读取时，未显式标记 safe 的条目掩码
vaulty-keeper apollo list prod --appid merdi --json
vaulty-keeper apollo compare prod test --appid merdi --appid-to merdi2 --json

# ④ TTY get 输出明文；非 TTY get 未标 safe 就掩码（§6）
vaulty-keeper apollo get prod APP_NAME --appid merdi
```

每份文件有三条配置；对比会报告 `API_SECRET` 变化。非 TTY 中虽然值不同，两边掩码都是 `*** (12 chars)`。TTY 中 `list` 显示 `APP_NAME`，凭据型条目仍掩码。这些是源码推导的预期，不是新的运行验证记录。CLI 覆盖已有快照时，TTY 会询问确认，非 TTY 需 `--force`；覆盖是整份替换而非合并。UI 则以 HTTP 409 拒绝重名。

快照默认存 `~/.vaulty/apollo/`，以「环境名 + AppID」寻址 `{env}__{appid}.json`；旧版无 AppID 的快照是 `{env}.json`，不传 `--appid` 读取。

---

## 2 · 加密落盘长什么样（快照文件结构）

`~/.vaulty/apollo/prod__merdi.json`（权限 0600）的结构示意。以下密文/nonce 仅说明格式，不是合成夹具的可复现输出：

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

- **快照中存储的 value 全部是密文**：每条 value 用 AES-256-GCM 单独加密，`enc` = Base64 密文，`nonce` = 独立随机 nonce。元数据和 key 名仍可读；明文文件/输出例外见前文。
- **新加密时 `secret` 选择密钥**：`true` = 敏感值密钥，`false` = 快照密钥；旧格式解密有所不同（§3）。
- **`safe` 是独立的输出许可**：只有 `secret=false` 不足以允许非 TTY/UI 明文。新自动分类条目默认 `safe=false`；显式 `--plain` 设置 `secret=false, safe=true`，`--secret` 设置 `secret=true, safe=false`。
- **`meta.captured_at`** 记录导入时刻（UTC RFC3339）。
- 文件名 `prod__merdi.json` 里 `__` 是分隔符，`{env}__{appid}.json`（`internal/apollo/store.go:88` 的 `FileName`）。

---

## 3 · 双密钥分工：为什么敏感值要单独一把钥匙

两把独立密钥，都在系统密钥库（`internal/apollo/keyring.go`），均可环境变量覆盖：

| 密钥 | Keychain account | 环境变量 | 加密对象 |
|---|---|---|---|
| 快照密钥 | `apollo-snapshot-key` | `VAULTY_KEEPER_APOLLO_KEY` | 非敏感值（secret=false） |
| 敏感值密钥 | `sensitive-key` | `VAULTY_KEEPER_SENSITIVE_KEY` | 敏感值（secret=true） |

对使用独立生成密钥的新加密条目，快照密钥不能单独解开敏感值密钥密文。旧格式敏感条目可能仍用快照密钥：[`internal/apollo/store.go`](../internal/apollo/store.go) 的 `DecryptItem` 先选择配置密钥，敏感值密钥解密失败后再尝试快照密钥。此兼容路径不会迁移旧密文。

> 非空环境变量优先于系统密钥库，即使密钥库正常可用。每个变量必须是恰好 32 字节密钥的标准 Base64 编码。无效覆盖会报错而不回退，格式有效但错误的密钥也会导致解密失败。生成/替换前先检查当前来源；换钥匙不会重新加密已有数据。不要将真实密钥导入 AI 会话或放进命令行参数。

---

## 4 · 敏感识别：什么会被自动标成 secret

导入时自动判断（`internal/apollo/mask.go` 的 `IsSensitiveKeyValue`），命中任一条 → `secret=true`：

1. **key 名命中**（不区分大小写）：
   `password|passwd|pwd|token|secret|salt|credential|private|access[_-]?key|secret[_-]?key|api[_-]?key`
   → `API_SECRET`、`CMS_SECRET`、`SENTRY_AUTH_TOKEN` 命中；`MONGODB_URI` 不会仅凭名字命中。
2. **带凭据的 URI/DSN**：key 名含 `uri|url|dsn|connection|endpoint|addr|address`，**且**值形如 `scheme://user[:password]@host`
   → 合成示例 `REDIS_URI=redis://:demo@localhost:6379/0` 和含凭据的 `MONGODB_URI` 值命中；纯 URL 不带 `@` 凭据（如 `https://example.com/api`）不中。
3. **JWT**：值形如 `eyJ...` 三段 base64url → `SUPABASE_SERVICE_ROLE_KEY` 这类中。

导入和新增条目的 `set` 会持久化检测出的 `secret` 分类，默认 `safe=false`。普通读取不改写分类。`set` 不带 `--plain/--secret` 时保留已有条目的 `secret` 与 `safe`。非 TTY/UI 读取是否掩码由 `safe` 决定，不依赖检测是否命中。

---

## 5 · 反转默认与 TTY 边界

非 TTY（脚本 / AI）下的输出规则（`internal/cli/cli.go` 的 `maskedFor`）：

- **默认全部掩码**，不靠 key 名猜测——`get`/`list`/`compare` 对**未显式标记安全**的 key 一律输出 `*** (n chars)`（`MaskWithLen`，保留长度信息）。
- 只有 `set --plain` / `mark --plain` **显式标记为安全**的 key 才输出明文。
- 明文出口（`reveal`/`export`/`edit`/`list|compare --reveal`/`aes decrypt`）在 stdin 非 TTY 时拒绝，加 `--yes` 也不放行。实现检查的是 stdin，不是真人身份或 stdout。Agent 不得伪造 TTY 或换路径取得明文。

```sh
vaulty-keeper apollo get prod REDIS_URI --appid merdi     # 非 TTY → *** (30 chars)
vaulty-keeper apollo get prod APP_NAME --appid merdi      # 未放行 → *** (5 chars)
```

TTY 中 `list`/`compare` 会掩码 `secret` 条目和启发式命中项，除非指定 `--reveal`。**TTY `get` 不需要 `--reveal` 就直接输出值，包括敏感值**。本地 CLI list/get 掩码没有指纹；bridge list/compare 和 UI 对比提供指纹（§7）。

---

## 6 · 显式放行：set --plain / mark --plain

```sh
# set 时直接标记
vaulty-keeper apollo set prod NEXT_PUBLIC_SAFE_FLAG true --plain --appid merdi

# 或对已有 key 只翻标记（不改值）
vaulty-keeper apollo mark prod APP_NAME --plain --appid merdi
vaulty-keeper apollo mark prod APP_NAME --secret --appid merdi   # 撤销放行
```

执行 `--plain` 后（尚未用 `--secret` 撤销时），文件写入 `safe:true`，非 TTY 的 `get`/`list` 才给明文。`items` 内条目示例：

```json
{ "APP_NAME": { "enc": "...", "nonce": "...", "secret": false, "safe": true } }
```

**防误标守卫**（`guardPlainMark`）：`--plain` 命中的 key 名/值看起来是敏感内容时，非 TTY 一律拒绝、TTY 需二次确认——防止误把 `API_SECRET` 标成"安全"漏给 AI。

---

## 7 · 指纹：掩码下怎么判断"两个值是否一致"

掩码只给长度，同长度的不同值看不出区别。`remote list <env>` 和 `remote compare` 还显示 **HMAC-SHA256 指纹**（前 8 字节，编码为 16 位十六进制；[`internal/apollo/mask.go`](../internal/apollo/mask.go) 的 `Fingerprint`）。`remote get` 虽然 API 响应包含指纹，CLI 只打印掩码。

- 指纹密钥 = 快照密钥：**密钥不泄露时无法离线枚举弱值来匹配指纹**。
- 指纹只可在同一 HMAC 密钥下比较，并使用 `NormValue`（剥掉一对匹配的外层引号；去除首尾空白是文本解析的独立步骤）。匹配是归一化值一致的高置信信号，不是原始字节相等的证明；需考虑归一化和截断哈希碰撞。
- 长度来自 Go `len(string)`，是 **UTF-8 字节数**，即使 UI/CLI 字面标签写 `chars`。UI 对比 JSON 对掩码条目使用 `length`/`fingerprint` 和 `value:null`；bridge 值包含掩码字符串及指纹。本地 CLI JSON 使用掩码字符串，不是该 API 结构。

```sh
# 宿主终端：保持此进程运行，另开终端执行 remote 读取
vaulty-keeper serve --addr 127.0.0.1:8970
```

```sh
# 第二个宿主终端；使用宿主 bridge-token 文件，不把加密密钥放进 argv
vaulty-keeper remote list prod --appid merdi --json
vaulty-keeper remote compare prod test --appid merdi --appid-to merdi2 --json
```

`remote compare --json` 返回 `from`、`to`、`added`、`removed`、`changed`；变化项的 `old`/`new` 对象含 `value` 掩码字符串和 `fingerprint`。指纹依赖密钥，此处不编造具体值。本地和远程 `compare --json` 在没有差异时目前都会打印普通文本的快照相同提示。同一 HMAC 密钥下，指纹不同意味着归一化值不同。**使用对比，不请求明文。** 隔离客户端只接收获准的代理凭据和可达地址；[DB 示例](db-proxy-examples.zh-CN.md) 区分宿主生成链接和容器使用。

---

## 8 · 导入解析规则（粘贴文本怎么被理解）

`internal/apollo/parser.go` 的 `ParseKV`：

- 每行 `KEY = value`，按**第一个 `=`** 分割，两侧去空格，value 可含 `=`。
- 空行、行首 `#` 的整行（单行/多行注释）跳过。
- **粘连自动拆分**：一行内粘在一起的多个 `KEY = ` 条目自动拆开并告警（如 `A = 1B = 2`），同时避免误拆 URL 查询参数（`...?TOKEN=1` 前的 `?` 不是 glue）。
- key 校验 `[A-Za-z_][A-Za-z0-9_.-]*`，非法行跳过并告警。
- 值两侧成对引号会被剥掉（`"merdi"` ≡ `merdi`）。

---

## 9 · 明文命令：reveal / export / edit（要求 stdin TTY）

以下仅供人工操作。输出可能留在终端日志、重定向、编辑器备份和剪贴板历史中。程序读取 stdin 不会抹掉提供输入的 shell 命令。

```sh
vaulty-keeper apollo reveal prod --appid merdi API_SECRET      # 单个敏感值明文
vaulty-keeper apollo reveal prod API_SECRET APP_NAME --appid merdi --json # 指定 key 的 JSON
vaulty-keeper apollo export prod --appid merdi                 # 全量 KEY = value（粘贴回 Apollo）
vaulty-keeper apollo export prod --appid merdi --copy          # 先打印，再调用 macOS pbcopy
vaulty-keeper apollo edit prod --appid merdi                   # $EDITOR 打开明文，保存后自动重新加密
```

- 这些命令要求 stdin TTY（`internal/cli/cli.go` 的 `isTerminal()`），不是在验证真人身份。Agent 不得对真实数据执行它们。`export --copy` 在调用 `pbcopy` 前仍打印明文，在其他平台可能复制失败。
- `edit` 流程 = `Export` 明文到临时文件（0600）→ 编辑器 → `ParseKV` 解析 → 整份重新加密写回（`app.EditLoad`/`EditApply`），编辑时不用手动管两把钥匙。
- **整份替换，不是局部补丁**：edit 和覆盖导入重建快照、重新检测 `secret`，并将 `safe` 重置为 false。省略或解析失败的条目可能消失。edit 会拒绝完全无法解析的结果，但只要部分条目可解析，目前就会丢弃解析警告；保存前检查全文，保存后复核 key、数量和分类。需要保留已有分类时，使用不带分类 flag 的单条 `set`。
- 作为 value 存储的外部 AES 密文是第二层：先解开快照包装，再用原外部 key/IV 解密。入口见 [UI AES 流程](ui-guide.zh-CN.md)。新 AES-GCM 加密绝不能对不同消息重复使用同一 key/IV。`aes gen-key` 会打印生成的秘密，不属于掩码读取。

---

## 10 · 常见场景速查

| 想干什么 | 命令 |
|---|---|
| 从 Apollo 复制配置落地 | `apollo import prod.txt --name prod --appid merdi` |
| 列出全部快照 | `apollo list` |
| AI 读某个值（掩码） | `apollo get prod KEY --appid merdi` |
| AI 判断两环境是否一致 | `apollo compare prod test --appid merdi --appid-to merdi2 --json` |
| 给 AI 放行一个确定安全的 key | `apollo set prod KEY v --plain --appid merdi` / `apollo mark prod KEY --plain --appid merdi` |
| 撤销放行（同时分类为 secret） | `apollo mark prod KEY --secret --appid merdi` |
| 看敏感值明文（自己 TTY） | `apollo reveal prod KEY --appid merdi` |
| 整份导出/编辑 | `apollo export prod --appid merdi` / `apollo edit prod --appid merdi` |
| 报错"快照不存在" | 看提示里的**相近快照**（同 env 的其他 appid），多半是 `--appid` 拼错 |

表中命令为子命令，需加 `vaulty-keeper` 前缀；`KEY`/`v` 是占位符，不是夹具里的名字/值。

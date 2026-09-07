# AGENTS.md

个人 AI 工具箱（Go 单二进制）：加密 Apollo 配置快照、AES 加解密（Java CryptoUtil 兼容）、本地 Web UI。完整命令文档见 `README.md`；每条命令都能用 `vaulty-keeper <cmd> -h` 自查。

## 构建

```sh
make build     # 产物 bin/vaulty-keeper
```

改过 `internal/ui/static/` 下的文件（HTML/CSS/JS）后**必须**重建：静态资源用 `go:embed` 打进二进制，源码改动不重建不生效。

## AI 调用 vaulty-keeper

### 安全模型（先读这个）

统一安全边界见 `docs/security-model.zh-CN.md`（信任层级、明文生命周期、token 生命周期、容器限制、各协议限制、验证状态）。下面是与 agent 直接相关的操作要点。

安全设计一句话：**非 TTY 明文出口一律拒绝（`--yes` 也无法放行）；非 TTY 读取默认掩码，除非显式标记 safe**。TTY 检查只验证 stdin 是否为终端（防误操作），**不是真人身份**——TTY 下 `get` 可直接输出明文。对"会主动读取密钥的同用户 AI"，本工具不承诺防护（信任边界见下）。

- **加密**：已存储的快照 value AES-256-GCM 加密落盘（0600）。**不覆盖所有文件**：AES key/iv 列表（`~/.vaulty/aes.json`）是明文 JSON；导入源、导出/下载、编辑临时文件、shell history 都可能含明文。两把独立密钥都在 macOS Keychain（环境变量兜底）：**快照密钥**（`apollo init`）加密非敏感值，**敏感值密钥**（`sensitive init`）加密敏感值——快照密钥泄露（如误发到别处）也解不开敏感值（新格式快照）。
- **信任边界（实测）**：Keychain 不防同用户进程——与用户同 UID 的进程（含 AI shell）可无弹窗执行 `security find-generic-password -w` 读出两把密钥（`internal/apollo/keyring.go:105` 本身就是这么读的）。Keychain 实际防的是其他用户/其他机器/意外明文，**不是**恶意同用户进程；要防后者只能让密钥不落在这台机器（不同账号/沙箱/会话解锁），本工具默认不做。
- **AI 读**：`list`/`compare`/`get` 默认只给掩码 `*** (n chars)` + 指纹——**反转默认**：不靠 key 名猜测，非 TTY 下未显式标记安全的 key 一律掩码；只有 `set --plain` / `mark --plain` 显式标记为安全的 key 才输出明文。任何明文输出（reveal、export、edit、`--reveal`、`aes decrypt`）**非交互终端（脚本/AI）一律拒绝，即使加 `--yes`**。该检查是防误操作门禁，不验证操作者或 stdout——TTY 下 `get` 可直接输出明文。Agent 的操作规则：不调用明文出口、不伪造 TTY。
- **AI 写**：`set`/`unset`/`import` 安全，无需 `--yes`（AI 写的就是它已知的明文，写入即加密）。
- **Web UI**：仅监听 127.0.0.1，随机 token 门控写操作与明文出口；GET 返回显式 safe 值，也会返回可用的 DB 连接 token/链接（不经 UI token）——这些是访问能力，不只是掩码元数据；**明文接口（reveal/export/明文编辑/AES 解密）默认禁用**，需 `--allow-plaintext` 显式开启（否则带 token 也 403）；token 失败限速为有上限的线性延迟（非指数退避）。
- **掩码代理（serve/remote）**：`vaulty-keeper serve --addr 0.0.0.0:8970` 在 host 起掩码代理，`vaulty-keeper remote list/get/compare` 在隔离域（Docker 容器/独立账号/VM）内读配置。代理**无条件掩码**——即使 `set --plain` 标记安全的 key 也不回明文；全部 /api 端点要 token（0600 写 `~/.vaulty/bridge-token`）+ 失败限速。对"故意对抗的同用户 AI"，这是唯一真正能防住的方式：把 AI 放进摸不到密钥/密文的隔离域（见 README「容器隔离部署」，docker-compose 已内置不挂载密钥目录/cap_drop/no-new-privileges）。
- **DB 隧道（db/serve）**：`vaulty-keeper db add` 只加密数据库 URL（独立 DB 密钥 `VAULTY_KEEPER_DB_KEY` + `~/.vaulty/db.json`，0600），并为每条连接生成**专属隧道 token**；`serve` 为每条连接起 TCP 隧道，在握手阶段把真实凭据注入（PG trust 风格 / MySQL 认证应答替换 / Redis 代发 AUTH），之后纯字节转发。客户端只需隧道 token（`db connect <name>` 打印；PG/MySQL/Redis **新旧连接都接受**全局 `VAULTY_KEEPER_BRIDGE_TOKEN` 或专属 token，PG/MySQL 放 username 字段 / Redis 放 AUTH 首命令），**不需要真实账号密码**；`db regen <name>|--all` 轮换专属 token（旧 token 对新连接立即失效，**不终止已有会话**，全局 token 不受影响）；DSN 永不离开 host、不进日志/回包。隧道**默认开启**，`db on/off <name>|--all` 按连接开关（serve 约 2 秒内生效，端口停止/恢复监听），UI 里每行有「开启隧道/关闭隧道」按钮。隧道监听地址跟随 `--addr`，`0.0.0.0` 时靠 token 门控兜底。只读靠注册只读账号实现，代理不强制。同名 `db add` 保留原端口但生成新 token 并重置为开启，需重新分发链接。
- **防破解**：掩码指纹是 HMAC-SHA256（密钥=快照密钥），密钥不泄露时无法离线枚举弱值匹配指纹；token 为 128 位随机，AES-256-GCM 暴力不可行。
- **判断一致性**：用 `compare`（掩码 + 长度 + 指纹即可判断），不要 `get` 明文。

### 命令

以下命令输出对 AI 安全（敏感值自动掩码为 `*** (n chars)`），默认使用：

```sh
bin/vaulty-keeper apollo list [<env>] --appid <id> --json
bin/vaulty-keeper apollo compare <a> <b> --appid <a_id> --appid-to <b_id> --json
bin/vaulty-keeper apollo get <env> <key> --appid <id>        # 非 TTY 只对标记为安全的 key 给明文
bin/vaulty-keeper apollo set/unset <env> <key> [<value>] --appid <id>
bin/vaulty-keeper apollo mark <env> <key> --plain|--secret --appid <id>   # 不改值，翻转安全/敏感标记
bin/vaulty-keeper aes encrypt --key ... --iv ...
bin/vaulty-keeper remote list|get|compare ...     # 容器/隔离域内经掩码代理读（永远只有掩码）
bin/vaulty-keeper db list / remote dblist ...     # 只列连接名/类型/端口/开关状态（不返回 URL）
bin/vaulty-keeper db connect <name>            # 打印带 token 的完整客户端命令（--container 用 host.docker.internal）
bin/vaulty-keeper db on/off <name>|--all       # 开启/关闭连接的隧道（AI 安全，不碰明文；serve 约 2 秒内生效）
bin/vaulty-keeper db test <name>                 # 验证注册的连接可用（AI 安全，不打印 URL）；失败提示 db add 同名修复（端口不变）
bin/vaulty-keeper db show <name>                 # 打印解密后的真实 URL（TTY-only，与 reveal 同门禁）
bin/vaulty-keeper db add <name>                   # 注册连接（URL 从 stdin 读，加密落盘）
bin/vaulty-keeper lang [en|zh]                    # 查看/设置共享语言（UI 与 CLI 互通，默认英文）
```

DB 隧道用法（AI 侧）：`db list`（或 `remote dblist`）拿到连接名 + 隧道端口后，用原生客户端连代理端口，token 用 `db connect <name>` 打印的连接专属 token（PG/MySQL/Redis **新旧连接也接受**全局 `VAULTY_KEEPER_BRIDGE_TOKEN`，Mongo 无全局回退）：
  psql "postgresql://$TOKEN:x@host.docker.internal:15432/appdb"   # token 放 user 字段，数据库名/账号密码一律用注册 URL 里的
  mysql -h host.docker.internal -P 15435 -u "$TOKEN" -px
  redis-cli -a "$TOKEN" -p 15434
AI 不需要真实 URL/凭据；不要从 db.json、serve 日志或回包中寻找 DSN。MongoDB 8 固定单端点已接入：虚拟用户 `vaulty`，专属 token 放密码字段，客户端使用 SCRAM-SHA-256、`authSource=admin`、`directConnection=true`、`retryWrites=false`；无全局 token 回退。Mongo 使用双端认证和持续命令白名单检查，不是认证后裸转发；管理命令、系统集合、视图和部分聚合不开放。注册 URL、查询限制及安全边界见 `docs/mongodb-tunnel-guide.zh-CN.md`，修改 Mongo 协议/认证/连接配置时先读该文档。业务文档保持原样，不承诺清除数据本身含有的秘密。serve 热加载：db add/rm/regen/on/off 后隧道自动开/关（每 2 秒同步 db.json），不用重启；Mongo token 轮换影响新连接，不强制中断已有会话。**watcher 前提**：serve 只在启动时 DB 存储已存在且密钥可用的情况下启动隧道 watcher；bridge-only 启动后首次注册 DB 需重启 serve。
本地人工验证：`./scripts/dbtest.sh`（Docker 起 pg/mysql/redis + 起 serve + 全量正/负向测试；`--clean` 收尾）。当前脚本已隔离重构（C02 完成）：每次运行用唯一临时目录/容器名、PID 与容器标签跟踪、假 HOME 与合成密钥，只清理自身，不碰真实 `~/.vaulty`/keyring。历史版本曾宽泛 pkill 并覆盖真实 bridge-token，现已不适用。
Mongo 隔离验收：`bash scripts/mongotest.sh --mongosh`；固定副本集端点加 `--replica-set`。脚本仅创建和清理自身夹具，测试使用临时存储及合成凭据，不读取用户配置或 Keychain。
图解：`docs/db-proxy-architecture.zh-CN.md`（Docker 里是什么/凭据存哪/三库认证注入/安全边界/时序）。
用法示例：`docs/db-proxy-examples.zh-CN.md`（多连接/各客户端/容器 AI/权限/脚本；按源码核对，执行证据为历史记录）。

明文命令 —— `apollo reveal`、`apollo export`、`apollo edit`、`apollo list/compare --reveal`、`aes decrypt` 会把明文打到 stdout，永久进入会话日志。**这些命令只在 stdin 为 TTY 时可用；脚本/AI 环境一律拒绝，加 `--yes` 也无法放行**。这是防误操作门禁，不是真人身份——TTY 下 `get` 可直接输出明文，同用户进程也能读取密钥。Agent 操作规则：不调用明文出口、不伪造 TTY。判断两个环境某 key 是否一致用 `compare`（掩码 + 长度即可判断），不要 get 明文。

`apollo get` 在非 TTY 下对未标记安全的 key 只输出 `*** (n chars)`；需要 AI 读取的确定安全 key（如 APP_NAME）用 `set --plain` 或 `mark --plain` 显式放行。

**语言**：工具默认英文；Web UI 顶栏可切换中英文（浏览器记在 `localStorage`，并尽力同步写 `~/.vaulty/prefs.json`——UI 与 CLI 解析顺序不同，同步可能失败）。切换方式：`vaulty-keeper lang zh|en`（写共享文件）或环境变量 `VAULTY_KEEPER_LANG`（优先级最高）；CLI 解析顺序 `VAULTY_KEEPER_LANG` → `~/.vaulty/prefs.json` → 默认 `en`。部分提示/flag 描述、shell 补全描述与底层库错误保持英文（中文界面下是「中文提示 + 英文底层细节」）。

红线：

- **密钥不进 AI 环境**：快照密钥走系统密钥库（`vaulty-keeper apollo init` 创建；macOS Keychain / Windows 凭据管理器 / Linux Secret Service）、敏感值密钥同理（`vaulty-keeper sensitive init` 创建）、数据库密钥同理（`vaulty-keeper db init` 创建，env 兜底 `VAULTY_KEEPER_DB_KEY`），不要在 AI 会话里 `export VAULTY_KEEPER_APOLLO_KEY` / `VAULTY_KEEPER_SENSITIVE_KEY` / `VAULTY_KEEPER_DB_KEY`——AI 拿到敏感值密钥就能自行解密所有快照的敏感值、拿到 DB 密钥就能解出全部数据库 URL。`VAULTY_KEEPER_AES_KEY` / `VAULTY_KEEPER_AES_IV` 同理，不要作为 `--key`/`--iv` 命令行参数传给命令（会出现在 `ps` 与 shell history）。注意：与 AI 同权限的进程本身就能读系统密钥库与 `~/.vaulty/aes.json`，这条红线防的是**额外扩散**（env/参数/日志），不是"同用户进程读取"；要防"故意对抗"的同用户 AI，用 Docker 容器隔离（见 README「容器隔离部署」）。
- **明文不可得**：明文命令在非交互环境一律拒绝（即使 `--yes`），不要尝试用 `--yes`、伪造 TTY、或替代命令（如 `aes decrypt`）获取明文；需要判断一致性用 `compare`。
- **Web UI 带访问令牌**：`vaulty-keeper ui` 启动时打印带 `?t=<token>` 的 URL，写操作（导入/增删改/导出/解密/明文编辑）都要这个令牌；**明文接口默认禁用**（需 `--allow-plaintext` 才开，否则带 token 也 403）。不要替用户执行会输出明文的 UI 操作（curl API），即使拿到了 token——明文操作需用户在浏览器中确认。
- **`--plain` 防误标守卫**：`set --plain` / `mark --plain` 命中敏感规则（password/token/secret/JWT/带凭据 URI）的 key 时，非 TTY 一律拒绝、TTY 需二次确认。AI 不要尝试用 `--plain` 放行敏感 key 给自己读明文。
- 快照目录：`--dir` 或 `VAULTY_KEEPER_APOLLO_DIR`，默认 `~/.vaulty/apollo/`。
- 非 TTY 下 `apollo rm` 需 `--yes`、`apollo import` 覆盖已有快照需 `--force`；不要绕过。
- 无参数 `vaulty-keeper` 显示完整命令 usage，并自动完成首次初始化：创建数据目录（`~/.vaulty/`、`~/.vaulty/apollo/`，0700）并生成 AES key/iv 列表的 `default` 条目（`~/.vaulty/aes.json`，0600），检测三把加密密钥（快照/敏感值/数据库）是否已初始化——缺失时 TTY 询问初始化、非 TTY 打印提示（交互菜单已移除，手动操作走 `vaulty-keeper ui` 或直接子命令）。

## 开发

- 改 Go 代码后跑 `go test ./...`；改动涉及并发/终端时加跑 `go test -race ./...` 和 `go vet ./...`。
- **改 `internal/ui/static/` 前端后**：`make test` 会先跑 `node scripts/check-ui.mjs`（JS 语法 / DOM id / 变量遮蔽 / i18n key 双语对齐），必须通过再 `make build` 重建。历史教训：局部变量遮蔽全局翻译函数 `t()`（`relTime`、`renderCompareRefs`）会导致整页渲染中断且单测抓不到。
- 加密快照格式、解析规则等见 `README.md`「验证」与对应包注释，改前先读。

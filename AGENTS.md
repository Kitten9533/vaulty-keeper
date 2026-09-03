# AGENTS.md

个人 AI 工具箱（Go 单二进制）：加密 Apollo 配置快照、AES 加解密（Java CryptoUtil 兼容）、本地 Web UI。完整命令文档见 `README.md`；每条命令都能用 `vaulty-keeper <cmd> -h` 自查。

## 构建

```sh
make build     # 产物 bin/vaulty-keeper
```

改过 `internal/ui/static/` 下的文件（HTML/CSS/JS）后**必须**重建：静态资源用 `go:embed` 打进二进制，源码改动不重建不生效。

## AI 调用 vaulty-keeper

### 安全模型（先读这个）

安全设计一句话：**明文出口只在用户本人终端可用（AI/脚本环境一律拒绝，`--yes` 也无法放行）；掩码默认反转**。对"会主动读取密钥的同用户 AI"，本工具不承诺防护（信任边界见下）。

- **加密**：快照所有值 AES-256-GCM 加密落盘（0600，磁盘无明文）。两把独立密钥都在 macOS Keychain（环境变量兜底）：**快照密钥**（`apollo init`）加密非敏感值，**敏感值密钥**（`sensitive init`）加密敏感值——快照密钥泄露（如误发到别处）也解不开敏感值（新格式快照）。
- **信任边界（实测）**：Keychain 不防同用户进程——与用户同 UID 的进程（含 AI shell）可无弹窗执行 `security find-generic-password -w` 读出两把密钥（`internal/apollo/keyring.go:105` 本身就是这么读的）。Keychain 实际防的是其他用户/其他机器/意外明文，**不是**恶意同用户进程；要防后者只能让密钥不落在这台机器（不同账号/沙箱/会话解锁），本工具默认不做。
- **AI 读**：`list`/`compare`/`get` 默认只给掩码 `*** (n chars)` + 指纹——**反转默认**：不靠 key 名猜测，非 TTY 下未显式标记安全的 key 一律掩码；只有 `set --plain` / `mark --plain` 显式标记为安全的 key 才输出明文。任何明文输出（reveal、export、edit、`--reveal`、`aes decrypt`）**非交互终端（脚本/AI）一律拒绝，即使加 `--yes`**——明文只在用户本人终端（TTY）可用。
- **AI 写**：`set`/`unset`/`import` 安全，无需 `--yes`（AI 写的就是它已知的明文，写入即加密）。
- **Web UI**：仅监听 127.0.0.1，随机 token 门控写操作与明文出口，GET 只返回掩码数据；**明文接口（reveal/export/明文编辑/AES 解密）默认禁用**，需 `--allow-plaintext` 显式开启（否则带 token 也 403）；token 失败限速（指数退避）。
- **掩码代理（serve/remote）**：`vaulty-keeper serve --addr 0.0.0.0:8970` 在 host 起掩码代理，`vaulty-keeper remote list/get/compare` 在隔离域（Docker 容器/独立账号/VM）内读配置。代理**无条件掩码**——即使 `set --plain` 标记安全的 key 也不回明文；全部 /api 端点要 token（0600 写 `~/.vaulty/bridge-token`）+ 失败限速。对"故意对抗的同用户 AI"，这是唯一真正能防住的方式：把 AI 放进摸不到密钥/密文的隔离域（见 README「容器隔离部署」，docker-compose 已内置不挂载密钥目录/cap_drop/no-new-privileges）。
- **DB 隧道（db/serve）**：`vaulty-keeper db add` 只加密数据库 URL（独立 DB 密钥 `VAULTY_KEEPER_DB_KEY` + `~/.vaulty/db.json`，0600），并为每条连接生成**专属隧道 token**；`serve` 为每条连接起 TCP 隧道，在握手阶段把真实凭据注入（PG trust 风格 / MySQL 认证应答替换 / Redis 代发 AUTH），之后纯字节转发。客户端只需隧道 token（`db connect <name>` 打印；旧连接回退全局 `VAULTY_KEEPER_BRIDGE_TOKEN`；PG/MySQL 的 username 字段 / Redis 的 AUTH 首命令），**不需要真实账号密码**；`db regen <name>|--all` 轮换 token（旧 token 立即失效）；DSN 永不离开 host、不进日志/回包。隧道监听地址跟随 `--addr`，`0.0.0.0` 时靠 token 门控兜底。只读靠注册只读账号实现，代理不强制。
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
bin/vaulty-keeper db list / remote dblist ...     # 只列连接名/类型/端口（不返回 URL）
bin/vaulty-keeper db connect <name>            # 打印带 token 的完整客户端命令（--container 用 host.docker.internal）
bin/vaulty-keeper db test <name>                 # 验证注册的连接可用（AI 安全，不打印 URL）；失败提示 db add 同名修复（端口不变）
bin/vaulty-keeper db show <name>                 # 打印解密后的真实 URL（TTY-only，与 reveal 同门禁）
bin/vaulty-keeper db add <name>                   # 注册连接（URL 从 stdin 读，加密落盘）

DB 隧道用法（AI 侧）：`db list`（或 `remote dblist`）拿到连接名 + 隧道端口后，用原生客户端连代理端口，token 用 `db connect <name>` 打印的连接专属 token（旧连接回退 `VAULTY_KEEPER_BRIDGE_TOKEN`）：
  psql "postgresql://$TOKEN@host.docker.internal:15432/appdb"   # token 放 user 字段，数据库名/账号密码一律用注册 URL 里的
  mysql -h host.docker.internal -P 15435 -u "$TOKEN" -px
  redis-cli -a "$TOKEN" -p 15434
AI 永远看不到真实 URL/凭据；不要试图从 db.json、serve 日志或任何回包中找 DSN。Mongo 未接入代理。serve 热加载：db add/rm/regen 后隧道自动开/关（每 2 秒同步 db.json），不用重启。
本地人工验证：`./scripts/dbtest.sh`（Docker 起 pg/mariadb/redis + 起 serve + 全量正/负向测试；`--clean` 收尾）。
图解：`docs/db-proxy-architecture.md`（Docker 里是什么/凭据存哪/三库认证注入/安全边界/时序）。
用法示例：`docs/db-proxy-examples.md`（多连接/各客户端/容器 AI/权限/脚本，全部实测过）。

明文命令 —— `apollo reveal`、`apollo export`、`apollo edit`、`apollo list/compare --reveal`、`aes decrypt` 会把明文打到 stdout，永久进入会话日志。**这些命令只在交互式终端（TTY）可用；脚本/AI 环境一律拒绝，加 `--yes` 也无法放行**——所以 AI 永远拿不到明文，即使被诱导要求也不会成功。判断两个环境某 key 是否一致用 `compare`（掩码 + 长度即可判断），不要 get 明文。

`apollo get` 在非 TTY 下对未标记安全的 key 只输出 `*** (n chars)`；需要 AI 读取的确定安全 key（如 APP_NAME）用 `set --plain` 或 `mark --plain` 显式放行。

红线：

- **密钥不进 AI 环境**：快照密钥走系统密钥库（`vaulty-keeper apollo init` 创建；macOS Keychain / Windows 凭据管理器）、敏感值密钥同理（`vaulty-keeper sensitive init` 创建）、数据库密钥同理（`vaulty-keeper db init` 创建，env 兜底 `VAULTY_KEEPER_DB_KEY`），不要在 AI 会话里 `export VAULTY_KEEPER_APOLLO_KEY` / `VAULTY_KEEPER_SENSITIVE_KEY` / `VAULTY_KEEPER_DB_KEY`——AI 拿到敏感值密钥就能自行解密所有快照的敏感值、拿到 DB 密钥就能解出全部数据库 URL。`VAULTY_KEEPER_AES_KEY` / `VAULTY_KEEPER_AES_IV` 同理，不要作为 `--key`/`--iv` 命令行参数传给命令（会出现在 `ps` 与 shell history）。注意：与 AI 同权限的进程本身就能读系统密钥库与 `~/.vaulty/aes.json`，这条红线防的是**额外扩散**（env/参数/日志），不是"同用户进程读取"；要防"故意对抗"的同用户 AI，用 Docker 容器隔离（见 README「容器隔离部署」）。
- **明文不可得**：明文命令在非交互环境一律拒绝（即使 `--yes`），不要尝试用 `--yes`、伪造 TTY、或替代命令（如 `aes decrypt`）获取明文；需要判断一致性用 `compare`。
- **Web UI 带访问令牌**：`vaulty-keeper ui` 启动时打印带 `?t=<token>` 的 URL，写操作（导入/增删改/导出/解密/明文编辑）都要这个令牌；**明文接口默认禁用**（需 `--allow-plaintext` 才开，否则带 token 也 403）。不要替用户执行会输出明文的 UI 操作（curl API），即使拿到了 token——明文只在用户本人浏览器里确认后可见。
- **`--plain` 防误标守卫**：`set --plain` / `mark --plain` 命中敏感规则（password/token/secret/JWT/带凭据 URI）的 key 时，非 TTY 一律拒绝、TTY 需二次确认。AI 不要尝试用 `--plain` 放行敏感 key 给自己读明文。
- 快照目录：`--dir` 或 `VAULTY_KEEPER_APOLLO_DIR`，默认 `~/.vaulty/apollo/`。
- 非 TTY 下 `apollo rm` 需 `--yes`、`apollo import` 覆盖已有快照需 `--force`；不要绕过。
- 无参数 `vaulty-keeper` 显示完整命令 usage，并自动完成首次初始化：创建数据目录（`~/.vaulty/`、`~/.vaulty/apollo/`，0700）并生成 AES key/iv 列表的 `default` 条目（`~/.vaulty/aes.json`，0600），检测三把加密密钥（快照/敏感值/数据库）是否已初始化——缺失时 TTY 询问初始化、非 TTY 打印提示（交互菜单已移除，手动操作走 `vaulty-keeper ui` 或直接子命令）。

## 开发

- 改 Go 代码后跑 `go test ./...`；改动涉及并发/终端时加跑 `go test -race ./...` 和 `go vet ./...`。
- 加密快照格式、解析规则等见 `README.md`「验证」与对应包注释，改前先读。

# vaulty-keeper 安全模型

> 中文 | [English](security-model.md)
>
> 安全边界的统一维护依据。各指南引用本页，不再重复完整模型。2026-09-07 按当前工作树核对源码；文档更正不构成测试证据（见[验证状态](#8-验证状态)）。

## 1 · 威胁模型一览

| 层级 | 机制 | 实际能防什么 |
|---|---|---|
| 0 · 同用户进程 | 系统密钥存储（Keychain / 凭据管理器 / Secret Service）与 0600 文件 | 对同用户进程（含 AI shell）**没有防护**。防的是其他用户、其他机器和意外明文，不是故意对抗的同用户进程。同用户进程可以像工具自身一样读取密钥（`security find-generic-password -w`、读 `~/.vaulty/aes.json`）。 |
| 1 · 掩码 + stdin TTY 门禁 | 非 TTY 读取默认掩码（除非显式 safe）；明文出口要求 stdin TTY | 防脚本/AI 管道中的意外泄露；不验证真人身份，也不保护 stdout。TTY 下 `get` 可直接输出明文。 |
| 2 · 掩码代理（`serve`/`remote`） | 快照 API 值永远掩码（含 safe 标记 key）；全部 `/api` 端点需要 bridge token | 只连桥的客户端读不到快照明文。但 bridge token **不只是元数据**：它同时授权 PG/MySQL/Redis 隧道。 |
| 3 · 独立域（容器/独立账号/VM） | 密钥与密文在该域可达范围之外；不挂载、降权 | 能防故意对抗的同用户 AI——前提是域内摸不到密钥/密文且网络暴露受控。 |

层级 0 意味着工具自身保证无法对抗故意对抗的同用户进程。其下的防御是操作规则与防误操作，不是对那种对手的承诺。

## 2 · 加密落盘：范围与例外

**加密落盘**：已存储的快照 value（AES-256-GCM，每项独立随机 nonce）和注册的数据库 URL/隧道 token（`~/.vaulty/db.json`），均 0600。独立配置时三把密钥互相独立（快照密钥 / 敏感值密钥 / DB 密钥）。

**不在落盘保证范围内**（设计上或使用上存在明文位置）：

- AES key/IV 列表 `~/.vaulty/aes.json` 是明文 JSON（0600）。
- 导入源、编辑器临时文件（及编辑器备份）、导出/下载、剪贴板、终端输出与进程内存。
- 喂给 stdin 的上游 shell 命令：程序消费 stdin 不会抹掉 shell history 里的 `printf 'URL'` / `echo 'URL'`。
- 数据库隧道返回的业务数据不做脱敏或加密。

新格式敏感快照值使用敏感值密钥；旧格式敏感密文可能仍回退快照密钥（`internal/apollo/store.go`）。双密钥隔离只针对独立加密的新数据。

密钥解析：非空环境变量优先于系统密钥库（`internal/apollo/keyring.go`）；错误的覆盖值不会自动回退。密钥必须是解码后 32 字节的 Base64。重新生成密钥可能使已有数据无法解密——先由人工私下检查密钥来源。

## 3 · TTY 门禁是防误操作，不是真人身份

`isTerminal()` 只检查 **stdin** 是否为 TTY（`internal/cli/cli.go`），不检查操作者或 stdout。

- 明文出口（`apollo reveal`/`export`/`edit`、`list|compare --reveal`、`aes decrypt`、`db show`）要求 stdin TTY；`--yes` 不能绕过。这拒绝的是脚本/AI 管道，不代表有人在终端前。
- TTY 下 `apollo get` 直接输出明文（`internal/cli/cli.go`）；stdout 可以被重定向。
- 同用户进程无论 TTY 与否都能拿到密钥材料（层级 0）。

**给 agent 的操作规则**：不对真实秘密调用明文出口、不伪造 TTY，并把带 token 的输出当凭据处理。

## 4 · 输出授权：safe 标记与 secret 分类是两回事

两个独立维度：

- **secret 分类**（`--secret`，按 key 名/带凭据 URI/JWT 形状自动识别）决定用哪把密钥加密。导入/set 时持久化；读取不改写。
- **safe 标记**（`set --plain` / `mark --plain`）是显式用户决策，授权非 TTY 读取与 UI GET 输出明文（`internal/apollo/store.go:36,119`）。自动识别项默认不是 safe。

safe 值是**获得授权的明文输出**，不只是"非敏感"分类。因此 GET 可以返回显式 safe 值；UI GET 还会在无 UI token 时返回可用的数据库连接 token/链接（`internal/ui/db.go`）——这授予的是数据库访问权，不只是掩码元数据。

`--plain` 防误标守卫：对命中敏感规则的 key 打 safe 标记时，非 TTY 一律拒绝、TTY 需二次确认。

## 5 · Token 与生命周期

| Token | 门控 | 范围 | 轮换 / 撤销 |
|---|---|---|---|
| UI token（每次 `ui` 启动新生成 128 位） | UI 非 GET 操作 | 仅本机 UI | 进程结束即失效，无共享状态。明文路由还需 `--allow-plaintext`，否则带 token 也 403。 |
| Bridge token（`~/.vaulty/bridge-token`，0600） | `serve` 全部 `/api` 端点 | 快照掩码读取**以及** PG/MySQL/Redis 新旧连接的隧道访问（`postgres.go`/`mysql.go`/`redis.go` 的 `tokenOKAny(user, globalToken, connToken)`） | 每次 `serve` 启动重新生成。失败检查每次递增 50 ms，上限 2 秒（线性，非指数退避）。 |
| 数据库专属隧道 token（每连接 128 位） | 对应一条注册连接 | PG/MySQL/Redis 接受专属或全局 token；MongoDB **只接受**专属 token（无全局兜底） | `db regen` 轮换；旧 token 对**新**连接失效。已有会话不终止。全局 token 不受影响。 |
| 同名 `db add` | — | 省略端口时保留原隧道端口，但生成新 token 并把 `enabled` 重置为 false（`internal/dbproxy/store.go`） | 重新注册后需重新分发客户端链接；若要监听须再次显式开启。 |

`db add`（含同名覆盖）写入 `enabled: false`。既无 `enabled` 也无 `disabled` 的旧文件视为开启（旧 omitempty 默认）。`db on/off` 切换持久化的监听状态（watcher 约 2 秒同步），不承诺终止已有会话。因此 `db regen` 和 `db off` 都不是即时会话撤销。

## 6 · 容器与网络边界

- 附带的 `docker-compose.yml` **不挂载** `~/.vaulty`、系统密钥存储、`~/.ssh` 或 Docker socket；使用非 root 用户、`cap_drop: ALL` 和 `no-new-privileges`。这只是起点，不是测得的防护率（Docker 逃逸与守护进程权限仍在）。
- Compose **不限制**所有出口必须经桥。entrypoint 对 bridge token 只打印 `<set>`/`<unset>` 占位标记，从不输出 token 本身（`docker/agent-entrypoint.sh`）；token 经环境变量进入容器，项目挂载、历史与日志仍可能暴露它。不要分享这些日志。
- bridge token 授予 PG/MySQL/Redis 隧道访问权，交付它就是交付数据库访问权，不只是元数据。项目挂载、持久化历史与日志可能暴露凭据。
- 隧道监听地址跟随 `serve --addr`；`0.0.0.0` 会把明文 HTTP 和隧道暴露给可达网络。token 检查不加密传输。使用 `127.0.0.1` 或防火墙受限接口。
- 代理不强制只读（需要只读就注册只读账号），也不脱敏业务数据。

## 7 · 各协议限制

- **PG**：注册 URL 的 `sslmode` 由代理自身处理（`require`/`verify-ca`/`verify-full` 强制 TLS；`prefer`/`allow` 先试 TLS 再回退明文；`disable`/缺省为明文）。这不是 libpq：上述三档强制 TLS 共用一条 Go TLS 拨号（校验主机名；直接 TLS，不是 PostgreSQL SSLRequest）。前端对 SSLRequest 回答拒绝，隧道 URI 上 `sslmode=require` 会失败。细节：[postgres-tunnel-guide.zh-CN.md](tunnel/postgres-tunnel-guide.zh-CN.md)。**Redis**：后端 TLS 用 `rediss://`。
- **MySQL**：`?tls=true` 会声明 `CLIENT_SSL`，将宿主到后端连接升级为 TLS 再认证与转发，并支持 `tlsCAFile` 信任私有/自签 CA。不要关闭真实后端要求的 TLS。证据状态见[验证状态](#验证状态)。细节：[mysql-tunnel-guide.zh-CN.md](tunnel/mysql-tunnel-guide.zh-CN.md)。
- **MongoDB 8**：固定单端点、用户 `vaulty` + 专属 token 作 SCRAM 密码、`authSource=admin`、`directConnection=true&retryWrites=false`、无全局兜底；双端认证 + 持续命令白名单（不是认证后裸转发）。视图、时序、事务、可重试写入、`comment`/`collation`/`create`/`createIndexes` 不开放。仅错误码 13 无法区分代理策略与后端角色拒绝。完整细节与限制：[mongodb-tunnel-guide.zh-CN.md](tunnel/mongodb-tunnel-guide.zh-CN.md)。
- 客户端到代理的传输是明文；使用本机或受控隔离的可信网络。

## 8 · 验证状态

此处只记录、不重跑：带日期的 MongoDB 8.0.13 standalone/固定副本集矩阵（单测/race/vet/build、原生 Go 驱动、容器内 `mongosh`）见 [mongodb-tunnel-guide.zh-CN.md](tunnel/mongodb-tunnel-guide.zh-CN.md#验证状态)，是历史结果——不是本次文档工作期间的新运行。

仍然**未验证**：真实 MongoDB TLS（只测过假后端证书/主机名）、人工交互 `db shell`、自动化独立复审。MySQL TLS **已修复**（C01：`CLIENT_SSL` 能力位 + TLS 升级连接贯穿认证与转发），但原生 TLS 证据**在仓库不可复现**（只有假后端单测；一次性真实 TLS 查询未固化为集成测试）。`scripts/dbtest.sh` **现已隔离**（C02 完成）：每次运行独立临时目录/容器、PID 与标签跟踪、假 HOME 与合成密钥，`--clean` 只清理自身登记资源；历史版本的宽泛 pkill 与真实 HOME token 覆盖已不适用。

文档更正不能声称通过编译/测试/浏览器/运行时/生产验证，除非在对应源码修订上实际执行并记录了该证据。

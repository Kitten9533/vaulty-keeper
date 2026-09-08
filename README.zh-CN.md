# vaulty-keeper

> 中文 | [English](README.md)

个人 AI 工具箱（Go 单二进制）。快照值、注册数据库 URL 和隧道 token 加密存储，但不覆盖所有本地文件：AES key/IV 列表是明文 JSON，导入源、导出/下载及编辑器临时文件可能含明文。`vaulty-keeper ui` 提供仅本机监听的快照、AES 和数据库连接 Web UI。系统密钥存储及可选原生客户端依赖平台设施。

本页负责安装与入门；[文档索引](docs/README.md)链接现行指南。[安全模型](docs/security-model.zh-CN.md)是安全边界的统一维护依据，[SECURITY.md](SECURITY.zh-CN.md) 说明安全报告方式，[CONTRIBUTING.md](CONTRIBUTING.zh-CN.md) 说明构建与贡献方式。

## 文档导航

每份面向用户的文档都是英文默认 + 顶部链接的 `.zh-CN.md` 配对版。数据库隧道是主打功能，其实现与用法见下方三篇 `db-*` 指南。

| 想做什么 | 看这里 |
|---|---|
| 完整命令参考（apollo / aes / 其他） | [docs/cli-reference.zh-CN.md](docs/cli-reference.zh-CN.md) |
| DB 隧道：架构、时序、凭据注入 | [docs/db-proxy-architecture.zh-CN.md](docs/db-proxy-architecture.zh-CN.md) |
| DB 隧道：使用示例与夹具 | [docs/db-proxy-examples.zh-CN.md](docs/db-proxy-examples.zh-CN.md) |
| PostgreSQL 隧道：设置、选项、排错 | [docs/tunnel/postgres-tunnel-guide.zh-CN.md](docs/tunnel/postgres-tunnel-guide.zh-CN.md) |
| MySQL 隧道：设置、TLS、排错 | [docs/tunnel/mysql-tunnel-guide.zh-CN.md](docs/tunnel/mysql-tunnel-guide.zh-CN.md) |
| Redis 隧道：设置、选项、排错 | [docs/tunnel/redis-tunnel-guide.zh-CN.md](docs/tunnel/redis-tunnel-guide.zh-CN.md) |
| MongoDB 8 隧道：选项、限制、验证矩阵 | [docs/tunnel/mongodb-tunnel-guide.zh-CN.md](docs/tunnel/mongodb-tunnel-guide.zh-CN.md) |
| 容器 / agent 隔离 | [docs/container-isolation.zh-CN.md](docs/container-isolation.zh-CN.md) |
| Web UI：页面、字段、确认流程 | [docs/ui-guide.zh-CN.md](docs/ui-guide.zh-CN.md) |
| Apollo 快照：实现讲解 | [docs/apollo-snapshot-guide.zh-CN.md](docs/apollo-snapshot-guide.zh-CN.md) |
| 安全边界（统一维护依据） | [docs/security-model.zh-CN.md](docs/security-model.zh-CN.md) |
| 构建、测试、贡献 | [CONTRIBUTING.zh-CN.md](CONTRIBUTING.zh-CN.md) |
| 安全报告 | [SECURITY.zh-CN.md](SECURITY.zh-CN.md) |

## 快速开始

**方式一：通过 npm 安装**（推荐；需要 Node.js 18+）：

```sh
npm install -g vaulty-keeper
```

npm 渠道通过包管理器下载预编译二进制，而不是浏览器，因此 macOS Gatekeeper 与 Windows SmartScreen 不会将其标记为"下载的文件"——不会出现"无法验证开发者 / Windows 已保护你的电脑"提示。安装的是与下方 Releases 相同的 Go 二进制。

**方式二：下载预编译二进制**（无需安装 Go 或 Node.js）：从 [Releases](https://github.com/Kitten9533/vaulty-keeper/releases) 下载对应平台的压缩包（macos/linux × x86_64/arm64，windows × x86_64），解压后把 `vaulty-keeper` 放到 PATH（Windows 为 `vaulty-keeper.exe`，压缩包名包含版本）。每次发布还会附带 `sha256sums.txt`，内含各压缩包的校验和。

二进制未做代码签名，因此**浏览器下载**的压缩包在首次运行时可能被拦截：

- **macOS** — Gatekeeper 提示"无法打开，因为无法验证开发者"（Apple Silicon 上可能提示"Apple 无法检查其是否包含恶意软件"）：右键点按二进制选择"打开"，或一次性移除隔离属性 `xattr -cr /path/to/vaulty-keeper`。
- **Windows** — SmartScreen 提示"Windows 已保护你的电脑"/未知发布者：点击"更多信息"→"仍要运行"，或在 PowerShell 中一次性解除阻止 `Unblock-File vaulty-keeper.exe`。

通过 npm 安装或源码构建不会触发这些提示，因为文件不会带有浏览器下载标记。

本 README 描述当前源码工作区。最新发布是 **v0.8.0**（2026-09-07）：包含 MongoDB 8 隧道支持，压缩包内含两版 README、LICENSE、AGENTS.md 和当前 `docs/` 指南（索引、安全模型与五份指南，中英双语）。由当前工作区构建的压缩包还会额外打包 CONTRIBUTING.md、SECURITY.md 及较新的 `cli-reference`、`container-isolation` 和逐协议隧道（`postgres` / `mysql` / `redis`）指南，该打包尚未随发布发布。历史实现记录归档在 git tag `docs-superpowers-archive`，不在本树或压缩包内。更早的归档如 **0.6.0** 不含 `docs/`；如需离线阅读，请在[源码仓库](https://github.com/Kitten9533/vaulty-keeper)选择对应 tag 的 `docs/`。本工作区指南不是早于 v0.8.0 的二进制的能力证据。

以下初始化由人工在宿主执行，会创建本地密钥/状态；不是隔离测试，也不是让 agent 访问真实秘密的指令：

```sh
vaulty-keeper apollo init      # 首次：生成快照密钥（macOS Keychain / Windows 凭据管理器 / Linux Secret Service）
vaulty-keeper sensitive init   # 首次：生成敏感值密钥
vaulty-keeper ui               # 长驻服务，后续命令另开终端执行
```

**方式三：源码构建**（需要 Go 1.26+、Git 和 Make；`make test` 还需要 Node.js）：

```sh
git clone https://github.com/Kitten9533/vaulty-keeper.git
cd vaulty-keeper
make build          # → bin/vaulty-keeper
make install        # 软链到 ~/.local/bin/vaulty-keeper
make test           # 单测（含 Java↔Go 互操作向量）
```

确保 `~/.local/bin` 在 PATH 中，或使用 `bin/vaulty-keeper`。维护者可用 `make release` 交叉编译发布包，但它会**删除并重建 `release/`**，不是快速开始检查，也不会自动发布。下文 shell 示例使用 POSIX 语法；Windows 可使用 WSL/Git Bash，或自行调整为 PowerShell 语法。

## 手动操作

直接运行 `vaulty-keeper`（无参数）显示全部命令与用法；运行时会自动初始化首次使用所需的基础设施——创建数据目录（`~/.vaulty/`、`~/.vaulty/apollo/`，0700）并生成 AES key/iv 列表的 `default` 条目（`~/.vaulty/aes.json`，0600），检测密钥初始化状态——快照与敏感值密钥总是检查，DB 密钥仅在 DB 存储已存在（`~/.vaulty/db.json`）时才检查，否则由首次 `db init` 创建；缺失时在终端询问并一键初始化（非 TTY 只打印提示）。需要手动增删改时推荐用 `vaulty-keeper ui`（本地 Web UI，覆盖全部快照与 AES 功能），或直接敲下面的子命令。

```sh
vaulty-keeper            # 显示完整命令树
vaulty-keeper <cmd> -h   # 每个子命令的完整帮助（语法 + 参数）
```

## 本地 Web UI

```sh
vaulty-keeper ui
vaulty-keeper ui --dir /path/to/snapshots --port 8080
vaulty-keeper ui --no-open
vaulty-keeper ui --allow-plaintext    # 显式开启明文接口（见下）
```

- 仅监听 `127.0.0.1`，不会暴露到局域网。
- 随机访问令牌门控非 GET 操作。**请打开启动时打印的完整 URL**，例如 `http://127.0.0.1:8080/?t=<token>`；读取不要求 UI token。即使显式指定 `--port`，占用时也可能顺延，以打印端口为准。持有 token 不代表调用者是真人。
- **明文接口默认禁用**（`reveal`/`export`/明文编辑/AES 解密/真实 DB URL 未开启时返回 403，即使带 token）；需用 `--allow-plaintext` 重启开启。这不是全面的无明文保证：快照 GET 返回显式 safe 值，且 **GET `/api/db/connect` 无需 UI token 就能返回可用隧道 token/链接**。本机调用者可能获得数据库访问权限，而不只是掩码元数据。
- 启动时会打印警示：带 token 的 URL **不要发给 AI/脚本、不要进日志或 shell history**。
- 提供快照浏览/搜索/增删改、导入、环境对比、明文编辑、导出（下载或复制）、AES 加解密（手动 key/iv）、快照密钥与敏感值密钥初始化、**数据库隧道**（注册/测试连接、生成各客户端链接、重新生成隧道 token、`--allow-plaintext` 下查看真实 URL）。
- 确认流程因操作而异：AES 转换和查看 URL 直接发请求；请求中的 `confirm: true` 字段不等于一次独立人工确认。明文响应使用 `Cache-Control: no-store`，不能阻止截取、复制到剪贴板或下载。
- 应用不主动将快照内容存入浏览器持久化存储；已显示的值、下载、扩展和浏览器/系统截取不在此保证内。实际导航与字段以 UI 指南为准。
- UI **默认英文**，可切换中文；浏览器偏好及尽力而为的共享文件同步可能与 CLI 不一致（见下「语言（UI 与 CLI）」）。
- macOS 启动时会尝试复用受支持浏览器中匹配的 UI 标签页，再回退为新开；`--no-open` 禁止打开浏览器。
>
> 逐页讲解 UI 功能与用法见 **[`docs/ui-guide.zh-CN.md`](docs/ui-guide.zh-CN.md)**（[English](docs/ui-guide.md)）。

## 语言（UI 与 CLI）

工具提供中英文 UI/CLI 文本，共享偏好文件，但解析规则不同：

- **Web UI** 优先使用浏览器 `localStorage`；没有本地选择时尝试共享设置，再默认英文。切换会尝试写入 `~/.vaulty/prefs.json`（0600），但可能同步失败，例如缺少有效 UI token。
- **CLI** 本地化命令树及许多帮助/运行时消息，但不保证与浏览器已保存的语言一致。
- `vaulty-keeper lang` 打印当前语言；`vaulty-keeper lang zh` 或 `vaulty-keeper lang en` 写入共享偏好（非 TTY 也可用）。
- `VAULTY_KEEPER_LANG=en|zh` 环境变量优先级最高。
- 解析顺序：`VAULTY_KEEPER_LANG` → `~/.vaulty/prefs.json` → 默认 `en`。

示例假设没有 `VAULTY_KEEPER_LANG` 覆盖，初始偏好为英文：

```sh
vaulty-keeper lang            # → language: en
vaulty-keeper lang zh         # 写入共享中文偏好
vaulty-keeper lang            # → 语言：zh
vaulty-keeper help            # 帮助树此时也是中文
```

部分提示、flag 描述、shell 补全描述和底层库错误仍为英文。

## CLI 一览

完整命令参考（apollo 快照工具、aes 加解密、其他命令）见 **[\`docs/cli-reference.zh-CN.md\`](docs/cli-reference.zh-CN.md)**（[English](docs/cli-reference.md)）。每条命令都能用 \`vaulty-keeper <cmd> -h\` 自查。

| 领域 | 命令 |
|---|---|
| 快照 | \`apollo init\` · \`sensitive init\` · \`apollo import/list/get/set/unset/mark/compare/reveal/edit/export/rm\` |
| AES（Java CryptoUtil 兼容） | \`aes list\` · \`aes gen-key\` · \`aes add\` · \`aes encrypt\` · \`aes decrypt\` |
| Web UI | \`ui\` |
| 掩码代理 | \`serve\` · \`remote list/get/compare\` |
| DB 隧道 | \`db init/add/list/test/connect/show/rm/shell/regen/on/off\` |
| 其他 | \`completion\` · \`lang\` · \`version\` |

非 TTY 输出默认对未显式标记 safe 的 key 掩码；明文出口要求 stdin TTY（防误操作门禁，不是真人认证）。下一节是数据库隧道——主打功能；精确 flag 用 \`vaulty-keeper <cmd> -h\` 查询。
## 数据库隧道代理（AI 使用隧道凭据查询）

> 完整的 ASCII 图解（Docker 里是什么 / 凭据存哪 / 三库认证注入 / 安全边界 / 时序）见 **[`docs/db-proxy-architecture.zh-CN.md`](docs/db-proxy-architecture.zh-CN.md)**（[English](docs/db-proxy-architecture.md)）。
> 多连接/原生客户端/容器/权限示例及夹具前提见 **[`docs/db-proxy-examples.zh-CN.md`](docs/db-proxy-examples.zh-CN.md)**（[English](docs/db-proxy-examples.md)）；请查看各示例的证据和版本范围。
> 逐协议操作指南：**[PostgreSQL](docs/tunnel/postgres-tunnel-guide.zh-CN.md)** · **[MySQL](docs/tunnel/mysql-tunnel-guide.zh-CN.md)** · **[Redis](docs/tunnel/redis-tunnel-guide.zh-CN.md)**（各自配套英文版）。
> MongoDB 8 固定端点的命令感知隧道、完整 URL 选项、安全边界及验证矩阵见 **[MongoDB 隧道指南](docs/tunnel/mongodb-tunnel-guide.zh-CN.md)**（[English](docs/tunnel/mongodb-tunnel-guide.md)）。下方旧图及示例范围为 PG/MySQL/Redis。

让容器/隔离域里的 AI 用**原生客户端**（psql / mysql / redis-cli / mongosh）和隧道凭据查询数据库，而非获得真实后端 URL。URL 在宿主通过独立 DB 密钥（`VAULTY_KEEPER_DB_KEY` / 系统密钥库）加密存储。`serve` 为每条连接起 TCP 隧道并认证后端。PG/MySQL/Redis 随后转发原始字节；MongoDB 持续按帧执行命令白名单并重建控制回包。业务数据不脱敏，详见各协议安全边界。

```
[Docker 容器：AI agent]
  psql "postgresql://$PG_TOKEN:x@host.docker.internal:15432/appdb"
  mysql -h host.docker.internal -P 15435 -u "$MYSQL_TOKEN" -px --ssl-mode=DISABLED
  redis-cli -h host.docker.internal -a "$REDIS_TOKEN" -p 15434
        ▼ TCP
[Host: vaulty-keeper serve --addr 0.0.0.0:8970]
  HTTP 掩码桥（原有）+ 每连接一个 TCP 隧道（校验 token → 用解密 URL 连真实库 → 注入真实凭据 → 转发）
        ▼
  真实数据库
```

图为示意，不是夹具准备流程：各 token 和端口须对应已注册连接。PG/MySQL/Redis 转发上游业务回包/错误，可能暴露后端元数据，不是通用响应脱敏器。

**人工宿主入门：合成 PostgreSQL**

需要 Docker、PATH 中当前源码构建的二进制，以及宿主原生 `psql`。示例向宿主默认 DB store 写入演示注册，不要复用已有连接名。预留后端端口 **25432**、隧道 **15432**、桥接 **8970**；自动分配不检查 OS 端口占用。固定演示容器名须未使用，重名时 Docker 会拒绝，不会删除已有容器。下文行内凭据全部为合成数据，不是真实秘密的输入模式。

宿主终端 1：

```sh
vaulty-keeper db init   # 仅首次使用，不要强制重新生成已有密钥
docker run -d --name vaulty-readme-pg --rm \
  -e POSTGRES_USER=app -e POSTGRES_PASSWORD=synthetic-demo-pass -e POSTGRES_DB=appdb \
  -p 127.0.0.1:25432:5432 postgres:17.6-alpine
docker exec vaulty-readme-pg pg_isready -U app -d appdb
```

等待 `pg_isready` 报告接受连接，再在终端 1 继续：

```sh
printf '%s\n' 'postgres://app:synthetic-demo-pass@127.0.0.1:25432/appdb?sslmode=disable' \
  | vaulty-keeper db add readme-orders --port 15432
vaulty-keeper db list
vaulty-keeper serve --addr 127.0.0.1:8970   # 长驻，等待监听成功输出
```

宿主终端 2：

```sh
vaulty-keeper db connect readme-orders --cmd
```

在终端 2 执行打印的 `psql` 命令，输入 `SELECT 1;`（预期结果 `1`），再输入 `\q`。打印的 token 是访问凭据。可选生命周期命令同样在终端 2 执行：

```sh
vaulty-keeper db regen readme-orders   # 重新分发生成的新客户端命令
vaulty-keeper db off readme-orders    # watcher 下次同步时停止监听
vaulty-keeper db on readme-orders
```

清理本演示时，仅在终端 1 用 Ctrl-C 停止自己启动的 `serve`，再在终端 2 执行：

```sh
vaulty-keeper db rm readme-orders --yes
docker stop vaulty-readme-pg   # --rm 删除这个演示容器
```

真实注册可由人工执行 `vaulty-keeper db add <name>`，在 stdin 提示后粘贴 URL；当前输入会**在终端回显**。管道避免 URL 进入 vaulty-keeper 的 argv，但字面 `printf 'URL'` 仍会进入上游 shell history。使用可信输入源，避免录制终端；agent 不得为此读取真实 URL。需要全量 `regen`/`on`/`off` 时用 `--all` 替代连接名，不要照抄 `name [--all]`。

- 类型从 URL scheme 自动识别：`postgres://`/`postgresql://`、`mysql://`、`redis://`/`rediss://`、`mongodb://`
- **同类可配多个**：每个连接独立名称和隧道端口，受可用端口/资源限制。`db add` 可显式指定或自动分配端口，在宿主逐个获取客户端命令。
- **宿主与容器**：`db connect <name> --container` 必须在持有本地 DB store/key 的宿主执行，仅向获授权客户端交付生成的命令/token。无密钥容器通过 `remote dblist`（或 `db list` 回退）取元数据，不能用 `db connect` 生成专属 token。不要为此挂载宿主密钥。`--container` 只改变打印地址，不改变监听；容器访问需要可达且受限的宿主接口。
- **Watcher 前提**：`serve` 仅在启动时 DB store 已存在且密钥可用才启动监听同步。bridge-only 启动后首次注册数据库，需要重启 `serve` 并重新分发新的全局 token。已启动的 watcher 每 2 秒同步增删/on/off，新连接读取 token 变化。修改已有端口可能需要重启监听（`off`，等关闭后再 `on`）。
- **隧道默认开启**；`db off <name>` 关闭单个监听，`db off --all` 关闭全部，在 watcher 下次同步时生效。`db on` 重新启用；列表显示关闭状态，UI 提供逐行开启/关闭按钮。
- `vaulty-keeper db connect <name>` 打印**可直接运行的带 token 命令**（psql/mysql/redis-cli/mongosh）；`--container` 使用 `host.docker.internal`，`--host` 选择主机，`--cmd` 仅输出一行。PG/MySQL 以 token 为用户、密码占位 `x`；Redis 以 token 为密码、用户占位 `x`；MongoDB 使用**用户 `vaulty`、专属 token 作为 SCRAM-SHA-256 密码、`authSource=admin`**，并带 `directConnection=true&retryWrites=false`。生成的 mongosh 命令只把隧道 URI/token 放入 argv，不含后端 URI；token 是供获授权 agent 使用的访问凭据，不是无害公开数据。
- `vaulty-keeper db regen <name>`（或 `db regen --all`）轮换 128 位专属 token，随后重新分发生成的链接；既有会话保留，全局 token 不受影响。**新旧 PG/MySQL/Redis 连接都接受全局或专属 token**；CLI 优先打印专属 token 不代表禁用了全局访问。MongoDB 仅接受专属 token。
- 同名 `db add` 未指定端口时保留原端口，但生成新 token 并恢复 enabled。之后须重新分发链接并检查暴露范围。UI enabled/off 是保存的配置，不是监听/后端健康状态；`Broken` 表示注册 URL 解密失败，token 解密可在 Resolve 时单独失败。
- **凭据注入**：PG 使用 trust 风格前端认证；MySQL 替换认证应答（`mysql_native_password` / `caching_sha2_password`）；Redis 代发后端 `AUTH`；MongoDB 使用注册的后端 SHA-256/SHA-1 凭据/认证库独立认证。隧道客户端不需要真实密码。
- **后端 TLS**：PG 由客户端库处理 `sslmode`，Redis 使用 `rediss://`。**MySQL `?tls=true` 会与后端协商 TLS**（C01 修复：已有单测；曾对 `require_secure_transport=ON` 的 MySQL 8 做过一次性原生 TLS 查询，经隧道可见 `Ssl_cipher` 非空、TLSv1.3，但该证据未被集成测试固化——依赖前请对真实 TLS 后端复测）；如需信任私有/自签 CA，加 `tlsCAFile=<路径>`。MongoDB 实现了 `tls=true`/`ssl=true` 和可选 `tlsCAFile` 的证书/主机名验证，但真实 MongoDB TLS 仍未验证。客户端到代理为明文，仅用于 localhost 或隔离可信网络。
- **只读控制**：代理层不强制只读，用只读账号的 URL 注册即天然只读
- `vaulty-keeper db shell <name>` 在宿主直接打开已安装的原生客户端（stdin-TTY 门禁）。密码使用子进程环境变量；MySQL/Redis 主机和 MySQL 用户仍可能进入 argv。MongoDB 通过临时子进程环境变量传入后端 URI，再由启动脚本移除。此流程不是脱敏隧道访问。
- **MongoDB 8** 支持单固定端点上的常见读取、要求服务端写入确认的 CRUD（不是人工审批）、已审查只读聚合及游标。后端账号需要 `listCollections` 权限以检查普通集合；不支持视图/时序、事务、可重试写入、SRV/故障转移、压缩或完整管理/GUI 兼容。常见不支持的选项/命令包括 `comment`、`collation`、`create` 和 `createIndexes`。对允许的集合使用有界读取，例如 `db.getCollection('orders').find({}).limit(5)`。[指南](docs/tunnel/mongodb-tunnel-guide.zh-CN.md)维护完整密码提示命令、注册后端 URL 与客户端 URI 的区别及排错；仅错误码 13 不能区分代理策略和后端角色拒绝。

**安全边界**

此处仅为摘要；[安全模型](docs/security-model.zh-CN.md)维护完整边界，包括 UI token 暴露和协议限制。

- 隧道监听地址跟随 `--addr`，默认 `127.0.0.1`；容器访问需要可达接口。明文监听仅限隔离可信网络。PG/MySQL 校验用户字段 token，Redis 校验 AUTH，支持专属/全局 token；MongoDB 校验虚拟用户密码中的专属 token，无全局兜底。Mongo 客户端认证前只提供有限 hello/监控，不提供业务命令。
- 不用的监听用 `db off` 关闭，需要时用 `db on` 恢复，状态持久化。token 轮换约束新连接，关闭监听不承诺终止已建立会话。
- 注册 URL 加密存储。Mongo 认证/控制回包及代理错误/日志不含后端凭据/主机，但业务文档不改写。可信 DBA 定义变更和上游 Mongo 日志不在代理保证内；人工直连 `db shell` 不是脱敏隧道会话。
- 专属隧道 token 为 128 位随机，可由 `db regen` 轮换，只交给获授权 agent/工具。全局 bridge token 兜底适用于 PG/MySQL/Redis，不适用于 MongoDB；持有隧道 token 即可在后端角色及代理策略范围内访问数据库。

## AI / 脚本使用安全指引

### 安全模型总览

**默认掩码和 stdin-TTY 门禁减少意外泄露，但不认证真人，也不隔离同用户进程。** [安全模型](docs/security-model.zh-CN.md)为统一依据，以下仅为入口摘要和操作者检查清单。

| 层 | 机制 |
|---|---|
| 静态加密 | 快照值及注册 DB URL/token 加密；元数据、明文 AES key/IV JSON、输入/导出/编辑器文件不在此范围。独立新加密使用快照/敏感值密钥；旧格式敏感密文有快照密钥回退。 |
| 信任边界 | 系统密钥存储不是同用户进程隔离边界。宿主密钥/密文应在不可信 agent 的权限之外；容器挂载、权限和网络访问同样需要审查。 |
| 掩码代理 | 快照 API 值始终掩码，包括 safe 值。桥接 list/compare JSON 含长度/指纹；`remote get` 只打印掩码。全局 token 同时授权 PG/MySQL/Redis 隧道访问。 |
| AI 读 | 非 TTY 本地读取仅放行显式 safe 值。TTY `get` 输出明文；显式明文命令检查 stdin TTY，不检查调用者身份/stdout。agent 不得对真实秘密使用这些出口或伪造 TTY。 |
| AI 写 | 写入会加密保存值，但能替换/删除数据和改变可见性，须有任务授权；导入覆盖及敏感转 safe 另有门禁。 |
| DB 隧道 | 加密注册 URL 和专属 token；PG/MySQL 以 token 为用户，Redis 以 token 为密码，MongoDB 使用用户 `vaulty` + token 密码，无全局兜底。MongoDB 持续按命令分帧并脱敏控制回包，不脱敏业务数据。`db regen` 影响新连接；`db on/off` 切换持久化监听状态，不保证终止既有会话。范围与证据见 [Mongo 指南](docs/tunnel/mongodb-tunnel-guide.zh-CN.md)。 |
| Web UI | 仅 loopback；非 GET 操作要求 UI token，显式明文路由还要求 `--allow-plaintext`。GET 无需 UI 认证即可返回 safe 值及可用 DB token。失败 token 检查是有上限的线性延迟，不是指数退避。 |
| 指纹 | 同密钥 HMAC-SHA256，对归一化值计算并截断为 8 字节；无密钥时抵抗离线猜测，不是字节相等证明。长度为 UTF-8 字节数。 |
| 判断一致性 | 用掩码 `compare`；需要指纹时使用桥接 list/compare JSON。不要只为比较而读取真实明文。 |

非 TTY 支持取决于具体命令：明文出口刻意拒绝，写操作可能需要显式 flag。`--json` 也非通用（包括 `apollo compare --json` 无差异时的文本结果）。stdout 明文可能进入对话上下文、会话日志和同步系统。含 token 的命令也必须按凭据对待。

**日常读取及获授权写入（不是统一授权）**
- `apollo list <env> --appid xx [--json]` — 非 TTY 未放行值显示 `*** (n chars)`
- `apollo compare <a> <b> --appid xx --appid-to yy [--json]` — 非 TTY 未放行值掩码 + 长度
- `apollo get <env> <key> --appid xx` — 非 TTY 下未放行值掩码，TTY 可能输出明文
- `apollo set/unset/mark`、`init`、`rm --yes` — 改变状态，先核对范围、值及覆盖/删除影响
- `remote list|get|compare` — 经掩码代理读，**永远只有掩码**（即使 key 标记为安全）
- `db list` / `remote dblist` — 只列连接名/类型/端口，**不返回 URL**
- `db add` — 写入加密 URL 和新 token；stdin 不清除上游历史或终端回显。同名注册重置 token/enabled 状态。真实 URL 应由可信人工提供，agent 不得自行读取。

**需要放行给 AI 的 key**：先显式标记为安全，AI 才看得到明文（例如 `APP_NAME`、`LOG_LEVEL` 这类确定无敏感内容的值）：
- `apollo set <env> <key> <value> --appid xx --plain`（改值时同时标记）
- `apollo mark <env> <key> --appid xx --plain`（不改值，只标记）

**防误标守卫**：`set --plain` / `mark --plain` 时若 key 名或值命中敏感规则（password/token/secret/JWT/带凭据 URI），**非 TTY 一律拒绝**、TTY 下需二次确认——防止把敏感 key 误标成安全而泄漏给 AI。

**明文出口（stdin-TTY 门禁；agent 不得用于真实秘密）**
- `apollo reveal <env> <key> --appid xx` → 解密后的明文
- `apollo export <env> --appid xx` → 全量明文，`--copy` 也会打印
- `apollo edit <env> --appid xx` → 明文编辑器文件及整份快照替换
- `apollo list/compare --reveal` → 明文
- `aes decrypt` → 明文
- `db show` / `db shell` → 真实 URL 或后端直连，不是脱敏隧道会话

`--yes` 不绕过 stdin-TTY 门禁，但不能由此推断 AI 永远拿不到明文：TTY 不代表身份，stdout 可重定向，safe 值刻意可见，同用户进程可能访问密钥。agent 不得绕过这些操作边界；人工须考虑日志、临时文件、编辑器备份、导出和剪贴板副本。

其他注意事项：
- **不要向 agent 提供真实加密密钥**：包括 `VAULTY_KEEPER_APOLLO_KEY`、`VAULTY_KEEPER_SENSITIVE_KEY`、`VAULTY_KEEPER_DB_KEY`、`VAULTY_KEEPER_AES_KEY` 和 `VAULTY_KEEPER_AES_IV`。不放入 agent 环境、argv 或 history。默认系统存储和 0600 AES JSON 不隔离同用户进程，必要时使用独立权限域。这是操作规则，不是密钥存储强制实现的承诺。
- CLI `import` 在 TTY 下覆盖前询问，脚本必须显式 `--force`。获授权替换仍会丢弃省略条目及已有标记，须按全量替换复核。
- 比较环境时使用掩码 `compare`；长度相同不足以判断值相同。桥接指纹在不读取明文的情况下提供同密钥归一化比较信号。

## 验证

测试覆盖指针与隔离 DB 夹具脚本（`scripts/dbtest.sh`、`scripts/mongotest.sh`）见 [CONTRIBUTING.zh-CN.md](CONTRIBUTING.zh-CN.md)；MongoDB 带日期验证矩阵见 [MongoDB 指南](docs/tunnel/mongodb-tunnel-guide.zh-CN.md#验证状态)。代码变化后须针对确切源码版本运行相关检查。

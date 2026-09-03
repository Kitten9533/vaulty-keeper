# vaulty-keeper

个人 AI 工具箱（Go 单二进制，无运行时依赖）。所有值在本地磁盘上都是加密存储，密钥不进配置中心、不进明文文件。`vaulty-keeper ui` 提供本地 Web UI 覆盖全部快照与 AES 工具（仅本机监听）。

## 快速开始

**方式一：下载预编译二进制**（推荐，无需安装 Go）——从 [Releases](https://github.com/<your-org>/vaulty-keeper/releases) 下载对应平台的压缩包（darwin/linux/windows × amd64/arm64），解压后把 `vaulty-keeper` 放到 PATH：

```sh
vaulty-keeper apollo init      # 首次：生成快照密钥（macOS Keychain / Windows 凭据管理器）
vaulty-keeper sensitive init   # 首次：生成敏感值密钥
vaulty-keeper ui               # 打开本地 Web UI
```

**方式二：源码构建**（需要 Go 1.26+）：

```sh
git clone <repo-url>
make build          # → bin/vaulty-keeper
make install        # 软链到 ~/.local/bin/vaulty-keeper
make test           # 单测（含 Java↔Go 互操作向量）
make release        # 交叉编译全平台发布包到 release/
```

## 手动操作

直接运行 `vaulty-keeper`（无参数）显示全部命令与用法；运行时会自动初始化首次使用所需的基础设施——创建数据目录（`~/.vaulty/`、`~/.vaulty/apollo/`，0700）并生成 AES key/iv 列表的 `default` 条目（`~/.vaulty/aes.json`，0600），检测加密密钥（快照/敏感值/数据库）是否已初始化，缺失则在你的终端询问并一键初始化（非 TTY 只打印提示）。需要手动增删改时推荐用 `vaulty-keeper ui`（本地 Web UI，覆盖全部快照与 AES 功能），或直接敲下面的子命令。

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
- 启动时生成随机访问令牌，URL 形如 `http://127.0.0.1:8080/?t=<token>`：所有写操作（导入/增删改/导出/解密/明文编辑）都要求携带该令牌，本机其他进程（如 AI agent）无法通过 curl 直接导出明文。**请用启动时打印的完整 URL 打开**；只敲 `localhost:8080` 时读操作可用，但写操作会被拒绝。
- **明文接口默认禁用**（`reveal`/`export`/明文编辑/AES 解密/AES 密钥列表在未开启时一律返回 403，即使带 token）；需要明文时用 `--allow-plaintext` 重启。这样即使 token 泄露给 AI，默认配置下也拿不到明文。
- 启动时会打印警示：带 token 的 URL **不要发给 AI/脚本、不要进日志或 shell history**。
- 覆盖全部功能：快照浏览/搜索/增删改、导入、环境对比、明文编辑、导出（下载或复制）、AES 加解密（手动 key/iv）、快照密钥与敏感值密钥初始化、**数据库隧道**（注册/测试连接、生成各客户端链接、重新生成隧道 token、`--allow-plaintext` 下查看真实 URL）。
- 明文出口（显示 / 明文编辑 / 导出 / AES 解密）均需二次确认后才显示，响应 `Cache-Control: no-store`，浏览器不持久化明文。
- 浏览器端不持久化快照内容。

## vaulty-keeper apollo — Apollo 快照工具

> 讲解 Apollo 快照的实现（加密文件结构 / 双密钥分工 / 敏感识别 / 掩码与指纹 / 显式放行）与实测示例见 **[`docs/apollo-snapshot-guide.md`](docs/apollo-snapshot-guide.md)**。

Apollo Open API 不可用时的替代方案：从 Apollo 门户**复制键值对**，导入加密快照，AI/脚本安全地读取、对比、修改。快照默认存 `~/.vaulty/apollo/<name>.json`（`--dir` 或环境变量 `VAULTY_KEEPER_APOLLO_DIR` 覆盖）。

```sh
vaulty-keeper apollo init                          # 首次：生成快照密钥（系统密钥库，如 macOS Keychain / Windows 凭据管理器）
vaulty-keeper sensitive init                       # 首次：生成敏感值密钥（独立于快照密钥）
vaulty-keeper apollo import prod.txt --appid xx    # 解析粘贴内容；--appid 必填；--name 省略时自动取文件名；已存在时需 --force 覆盖
vaulty-keeper apollo import - --name prod --appid xx   # 从 stdin 读（旧写法 --app-id 仍兼容）
 vaulty-keeper apollo list                          # 列出快照（环境 + AppID）
 vaulty-keeper apollo list prod --appid xx          # 默认全部掩码 *** (长度)；--reveal 显示明文（仅 TTY）
 vaulty-keeper apollo list prod --appid xx --json   # JSON 输出（AI 友好）
 vaulty-keeper apollo get prod --appid xx SOME_KEY  # 非 TTY 下只对标记为安全的 key 输出明文，其余掩码
 vaulty-keeper apollo set prod --appid xx SOME_KEY value
 vaulty-keeper apollo set prod --appid xx SOME_KEY value --plain    # 显式标记为安全：AI/脚本可读明文
 vaulty-keeper apollo set prod --appid xx SOME_KEY value --secret   # 显式标记为敏感：始终掩码
 vaulty-keeper apollo mark prod --appid xx SOME_KEY --plain|--secret  # 不改值，只翻转安全/敏感标记
 vaulty-keeper apollo unset prod --appid xx SOME_KEY
 vaulty-keeper apollo compare prod test --appid xx --appid-to yy   # added/removed/changed，默认全部掩码
 vaulty-keeper apollo compare prod test --json
 vaulty-keeper apollo reveal prod --appid xx SECRET_TOKEN          # 显示敏感值明文（仅 TTY）
 vaulty-keeper apollo reveal prod --appid xx app.fs.oss.secret-key --key <aes> --iv <aes>   # 解密外部 AES 密文（仅 TTY）
 vaulty-keeper apollo edit prod --appid xx         # $EDITOR 打开明文编辑，保存后自动重新加密（仅 TTY）
 vaulty-keeper apollo export prod --appid xx       # 解密全量输出，供粘贴回 Apollo（仅 TTY）
 vaulty-keeper apollo export prod --appid xx --copy # 直接复制到剪贴板（pbcopy）（仅 TTY）
 vaulty-keeper apollo rm prod --appid xx           # 删除快照（TTY 确认；非 TTY 需 --yes）
 ```

> 明文命令（`reveal`/`export`/`edit`/`list|compare --reveal`/`aes decrypt`）**只在交互式终端可用**；AI/脚本环境一律拒绝，加 `--yes` 也无法放行。
>
> **反转默认（reverse default）**：`get`/`list`/`compare` 在 AI/脚本（非 TTY）环境下默认**全部掩码**，不靠 key 名猜测——除非某个 key 被显式标记为安全（`set --plain` 或 `mark --plain`）。这样即使有大量未知名字的 key，AI 也拿不到任何明文；需要 AI 读取的少量已知安全 key 才单独放行。

快照以「环境 + AppID」为唯一键，存储为 `{env}__{appid}.json`；旧版无 AppID 的 `{env}.json` 仍可读取（不指定 `--appid` 时访问）。

解析规则：

- 每行 `KEY = value`，按第一个 `=` 分割，两侧去空格（value 可含 `=`）。
- 空行、行首 `#` 的整行（单行/多行注释）跳过。
- 一行内粘在一起的多个 `KEY = ` 条目自动拆分并警告（如 `A = 1B = 2`）。
- key 校验 `[A-Za-z_][A-Za-z0-9_.-]*`，非法行跳过并警告。

两把密钥（都在 Keychain，均可环境变量覆盖，均不进明文文件）：

- **快照密钥**（`VAULTY_KEEPER_APOLLO_KEY`，`apollo init` 创建）：加密所有非敏感值。
- **敏感值密钥**（`VAULTY_KEEPER_SENSITIVE_KEY`，`sensitive init` 创建）：加密所有敏感值（password/token/secret/...）。敏感值只有这把密钥能解开，`reveal`/`--reveal` 显示明文靠它，AI 进程拿不到它就无法读取敏感值明文。文件权限 0600，值为 AES-256-GCM + 每条独立随机 nonce。

敏感识别（默认掩码，`--reveal` 显示；`--reveal` 仅 TTY 可用）：

- **key 名命中**：`password|passwd|pwd|token|secret|salt|credential|private|access[_-]?key|secret[_-]?key|api[_-]?key`（不区分大小写）
- **值带凭据的 URI/DSN**：key 名含 `uri|url|dsn|connection|endpoint|addr|address`，且值形如 `scheme://user[:password]@host`（如 `mongodb://root:pw@...`）
- **JWT**：值形如 `eyJ...` 三段式 base64url（如 `SUPABASE_SERVICE_ROLE_KEY`、`NEXT_PUBLIC_SUPABASE_ANON_KEY`）

宁多掩不漏掩，TTY 下 `--reveal` 可补救。

## vaulty-keeper aes — AES 加解密（Java CryptoUtil 兼容）

用于解密 Apollo 里 OSS AK/SK 这类**值本身就是 CryptoUtil 密文**的配置。算法对齐 `CryptoUtil.java`：AES/GCM/NoPadding、tag 128 bits、key 为 UTF-8 字节（16/24/32）、iv 为 UTF-8 字节直接作 GCM IV、密文为 Base64。

key/iv 统一存在 `~/.vaulty/aes.json`（0600）的**命名列表**里，格式为数组 `[{name, secret-key, iv}, ...]`（旧版单对象 `{key, iv}` 自动迁移为 `default` 条目）。CLI 用 `--name` 引用；Web UI 的 AES 工具与快照"显示"解密均为**手动输入 key/iv**（不读取列表）。

```sh
# 列出 / 新增 / 删除条目
vaulty-keeper aes list
vaulty-keeper aes gen-key --name oss              # 生成并保存到 aes.json
vaulty-keeper aes add --name oss --key <k> --iv <i>   # 手动保存条目

# 用列表条目加解密（decrypt 仅 TTY 输出明文）
vaulty-keeper aes encrypt --name oss 'hello'
vaulty-keeper aes decrypt --name oss '<base64>'

# 或手动指定 / 环境变量（避免进 shell history）
vaulty-keeper aes encrypt --key <k> --iv <i> 'hello'
VAULTY_KEEPER_AES_KEY=<k> VAULTY_KEEPER_AES_IV=<i> vaulty-keeper aes decrypt '<base64>'

# 解密外部 AES 密文值（仅 TTY）
vaulty-keeper apollo reveal prod app.fs.oss.secret-key --key <k> --iv <i>
```

输入可走 `--file`、参数或 stdin。`decrypt` 输出明文，**仅交互式终端可用**（脚本/AI 环境一律拒绝）。

## 其他

```sh
vaulty-keeper ui                              # 启动本地 Web UI（默认 127.0.0.1:8080，占用时自动顺延）
vaulty-keeper serve --addr 0.0.0.0:8970       # 掩码代理（host 持有密钥时对容器/隔离域开放）
vaulty-keeper remote list|get|compare ...     # 通过掩码代理读（形态与 apollo 子命令一致）
vaulty-keeper db <init|add|list|test|connect|show|rm|shell|regen> ... # 加密数据库连接 + 隧道（见「数据库隧道代理」）
vaulty-keeper completion zsh | source /dev/stdin   # 或 bash / fish，加到 shell 配置
vaulty-keeper version
```

## 容器隔离部署（防"故意对抗"AI，跨 macOS / Windows）

默认安全模型防的是"守规矩的 AI"；对**故意不读文档、主动拿密钥**的 AI，唯一可靠的办法是把它放进一个**摸不到密钥和密文**的隔离域。方案用 Docker 统一（macOS/Windows 的 Docker 都是 Linux VM）：

```
[Docker 容器：codex / claude / opencode / pi]
      │  vaulty-keeper remote list|get|compare（只拿掩码）
      ▼
[Host：持有密钥]
      vaulty-keeper serve --addr 0.0.0.0:8970   ← 掩码代理，永不回明文
      ▼
      系统密钥库 + ~/.vaulty/（容器永远看不到）
```

### Host 侧：启动掩码代理

```sh
vaulty-keeper serve --addr 0.0.0.0:8970     # 打印 token 并写入 ~/.vaulty/bridge-token
```

- 只输出掩码：`*** (n chars)` + 长度 + 指纹，**即使 `set --plain` 标记为安全的 key 也不回明文**
- 所有 `/api` 端点都要 token（0600 写入 `~/.vaulty/bridge-token`），失败限速（指数退避）
- `0.0.0.0` 是为了让 Docker VM 能通过 `host.docker.internal` 访问；token 门控 + 只回掩码，暴露到局域网也可接受（只想本机用就绑 `127.0.0.1`，但容器将连不上）

### 容器侧：agent 隔离域

```sh
# 构建镜像（host 先 make build，二进制会被拷进镜像）
docker build -t vaulty-keeper-agent:local .

# 启动（token 从 host 读，只值掩码）
export VAULTY_KEEPER_BRIDGE_TOKEN="$(cat ~/.vaulty/bridge-token)"
export VAULTY_KEEPER_PROJECT_DIR=/path/to/your/project   # 只挂载项目目录
docker compose up -d

# 进容器跑 agent；读配置用 vaulty-keeper remote（命令形态与本地一致）
docker compose exec agent codex
docker compose exec agent vaulty-keeper remote list prod --appid xx

# 容器里同样可以用 DB 隧道（见「数据库隧道代理」）：连 host.docker.internal 的隧道端口
docker compose exec agent bash -c 'psql "postgresql://$VAULTY_KEEPER_BRIDGE_TOKEN@host.docker.internal:15432/appdb" -c "SELECT 1;"'
```

隔离要点（`docker-compose.yml` 已内置）：

- **不挂载** `~/.vaulty`、系统密钥库、`~/.ssh`、docker socket → 容器拿不到密钥和密文
- 非 root 用户 + `cap_drop: ALL` + `no-new-privileges`
- 容器只通过 `VAULTY_KEEPER_BRIDGE_ADDR` / `VAULTY_KEEPER_BRIDGE_TOKEN` 连掩码代理
- 装 agent CLI：`VAULTY_KEEPER_INSTALL_AGENTS='@openai/codex @anthropic-ai/claude-code opencode-ai'`（进容器时自动 npm 安装到用户目录）
- **持久化**：`agent-home` 命名卷挂到 `/home/agent`，已装的 CLI 与 agent 会话历史跨容器重建保留；想完全重置就 `docker volume rm vaulty-keeper_agent-home`
- **Linux 兼容**：compose 已加 `extra_hosts: host.docker.internal:host-gateway`（macOS/Windows 的 Docker Desktop 本就提供，无影响）

### 为什么这样能防住"故意对抗"AI

容器里的 AI 就算不读任何文档：拿不到系统密钥库（VM 隔离）、拿不到快照文件（未挂载）、拿不到密钥（不传 env）；唯一出口是掩码代理，而它**只回掩码**。明文在密码学意义上无法离开 host。

**注意 Docker 本身不是绝对隔离**：`cap_drop ALL` + `no-new-privileges` 已大幅收窄容器逃逸面，但 Docker daemon 是 root 服务，容器逃逸是真实攻击面。对**极高威胁等级**（专业对抗 / 合规审计）应升级到独立 macOS 账号、VM 或云沙箱（见下节）；Docker 方案适合"防止绝大多数 AI 主动拿密钥"的场景。

### Windows 用户

- 同一套 compose/镜像；Windows 版 Docker Desktop 底层是 WSL2，`host.docker.internal` 同样可用
- 密钥存 **Windows 凭据管理器**（`vaulty-keeper apollo init` / `sensitive init` 自动适配，无需 `security` 命令）
- 交互式菜单 / 明文命令需要真实控制台 TTY（`isTTY` 按 `GetConsoleMode` 检测）；脚本 / 代理环境自动掩码

### 不用 Docker 的替代用法

`serve` + `remote` 本身与 Docker 无关，隔离域可以是任何「摸不到密钥和密文」的环境：

**① 本机直接跑（无隔离，防"守规矩"的 AI）**

```sh
vaulty-keeper serve --addr 127.0.0.1:8970    # 终端1：host 起代理（持有密钥）
export VAULTY_KEEPER_BRIDGE_ADDR=http://127.0.0.1:8970
vaulty-keeper remote list prod --appid xx    # 终端2：只拿掩码
```

AI 与你在同一账号下时，靠的是掩码 + TTY 门禁；对会主动读密钥的 AI 不设防。

**② 独立 macOS 账号（真隔离，替代 Docker）**

```sh
sudo sysadminctl -addUser ai -password '<pw>' -admin no   # 一次性创建
# 启动 agent（token 只值掩码，进 ai 会话无害）：
sudo -u ai env VAULTY_KEEPER_BRIDGE_ADDR=http://127.0.0.1:8970 \
  VAULTY_KEEPER_BRIDGE_TOKEN="$(cat ~/.vaulty/bridge-token)" codex
```

`ai` 账号的 Keychain 里没有你的密钥、读不了 `~/.vaulty/`（0700），隔离效果与 Docker 相当；代价是要管理账号、git 凭据与文件权限。

**③ 远程机器 / WSL2**

agent 放另一台机器或 Windows WSL2，host 的 `vaulty-keeper serve --addr 0.0.0.0:8970` 走网络可达（token 门控 + 只回掩码）。

## 数据库隧道代理（AI 查库，DSN 不暴露）

> 完整的 ASCII 图解（Docker 里是什么 / 凭据存哪 / 三库认证注入 / 安全边界 / 时序）见 **[`docs/db-proxy-architecture.md`](docs/db-proxy-architecture.md)**。
> 大量实测过的用法示例（多连接/各客户端/容器 AI/权限/脚本）见 **[`docs/db-proxy-examples.md`](docs/db-proxy-examples.md)**。

让容器/隔离域里的 AI 用**原生客户端**（psql / mysql / redis-cli）查询数据库并拿到数据，但数据库连接 URL（地址/账号/密码）**绝不暴露给 AI**。URL 只以密文形式存在 host 的 vaulty-keeper 里（独立 DB 密钥，`VAULTY_KEEPER_DB_KEY` / 系统密钥库），`serve` 为每条连接起一个 TCP 隧道，在握手阶段注入真实凭据后纯字节转发。

```
[Docker 容器：AI agent]
  psql "postgresql://$TOKEN@host.docker.internal:15432/appdb"   # token 放 user 字段
  mysql -h host.docker.internal -P 15435 -u "$TOKEN" -px         # token 放 username 字段
  redis-cli -a "$TOKEN" -p 15434                                  # token 放 AUTH
        ▼ TCP
[Host: vaulty-keeper serve --addr 0.0.0.0:8970]
  HTTP 掩码桥（原有）+ 每连接一个 TCP 隧道（校验 token → 用解密 URL 连真实库 → 注入真实凭据 → 转发）
        ▼
  真实数据库
```

**用法**

```sh
vaulty-keeper db init                                                          # 首次：生成 DB 密钥
printf 'postgres://app:pass@db.example.com:5432/orders' \
  | vaulty-keeper db add orders [--port 15432]                                 # URL 走 stdin，不进 argv/history
vaulty-keeper db list                                                          # orders (postgres) :15432
vaulty-keeper db regen orders                                                  # 轮换该连接的隧道 token，旧 token 立即失效
vaulty-keeper db regen --all                                                   # 轮换所有连接的隧道 token
vaulty-keeper serve --addr 0.0.0.0:8970                                        # 同时起掩码桥 + 隧道
```

- 类型从 URL scheme 自动识别：`postgres://`/`postgresql://`、`mysql://`、`redis://`/`rediss://`
- **同类可配多个**：每个连接一个名字 + 一个独立隧道端口，数量不限（如 3 个 MySQL：`mysql-orders`/`mysql-billing`/`mysql-reporting`，`db add` 时各指定/自动分配端口），`db connect <name>` 逐个取命令
- 容器/隔离域内用 `vaulty-keeper db list`（无本地 store 时自动经桥读取）或 `vaulty-keeper remote dblist` 查隧道端口，再用原生客户端连（`$TOKEN` 取 `vaulty-keeper db connect <name>` 打印的连接专属 token；旧连接无专属 token 时回退全局 `VAULTY_KEEPER_BRIDGE_TOKEN`）
- **热加载**：`serve` 每 2 秒同步 `db.json`——`db add`/`db rm`/`db regen` 后隧道自动开/关，**不用重启 serve**
- `vaulty-keeper db connect <name>` 直接打印**带 token 的完整客户端命令**（psql/mysql/redis-cli，token 已填好），`--container` 换成 `host.docker.internal`，`--host` 指定其他主机、`--cmd` 只输出单行命令
- `vaulty-keeper db regen <name>|--all` 轮换隧道 token：每条连接有**专属 token**（128 位随机，随 URL 一起加密落盘），`db add` 时自动生成；token 泄露时单独轮换即可，全局 bridge token 不受影响
- **凭据注入**：PG 假 server 直接放行（trust 风格，token 在 user 字段）；MySQL 握手时把真实密码的认证应答换进去（支持 `mysql_native_password` / `caching_sha2_password`）；Redis 代理代发真实 `AUTH`。客户端永远不需要真实密码
- **TLS**：PG 按 URL 的 `sslmode`（require/verify-ca/verify-full/prefer）、MySQL 用 `?tls=true`、Redis 用 `rediss://` 连接真实库；客户端↔代理为本机/局域网明文
- **只读控制**：代理层不强制只读，用只读账号的 URL 注册即天然只读
- `vaulty-keeper db shell <name>` 可在 host 上直接打开原生客户端（TTY-only，凭据走环境变量不进 argv）
- Mongo 未支持（无成熟 Go 代理库；host 上用 `vaulty-keeper db shell` 或直接 mongosh 自用）

**安全边界**

- 隧道监听地址跟随 `--addr`：默认 `127.0.0.1`；容器要连需 `0.0.0.0`（局域网可达），**由 token 门控兜底**——token 在 PG/MySQL 的 username 字段、Redis 的 AUTH 首命令里校验（连接专属 token 或全局 bridge token 任一匹配），无 token 的局域网用户连上即被断开
- 真实 URL/凭据只存在于 host 内存，db.json 无明文、日志不记录、任何回包不含
- 隧道 token 为连接专属（128 位随机，随 URL 一并加密落盘），`db regen` 可轮换；旧连接回退全局 bridge token（掩码桥用，128 位随机）+ 失败限速；token 泄露给 AI 是设计内的（AI 本就该能用），防的是"无 token 的第三方"

### 手动验证（Docker 一键）

`scripts/dbtest.sh` 用 Docker 起 postgres + MySQL(8.4，含模拟 shop 业务库) + redis 三个容器、注册连接、起 `serve`、跑全量正/负向测试并保持运行：

```sh
make build
./scripts/dbtest.sh          # 启动并测试；测完环境保持运行，打印连接方式
./scripts/dbtest.sh --clean  # 收尾：停 serve、删容器
```

也可以手动分步验（环境就绪后）：

```sh
TOKEN=$(cat ~/.vaulty/bridge-token)   # serve 每次重启会换 token，先取

# ① 本机直接测 Redis（token 放 AUTH，不碰真实密码）
redis-cli -p 15434 -a "$TOKEN" --no-auth-warning ping
redis-cli -p 15434 -a "$TOKEN" --no-auth-warning set k v && redis-cli -p 15434 -a "$TOKEN" --no-auth-warning get k

# ② 模拟容器内 AI：原生客户端经 host.docker.internal 走隧道（token 放 user/username）
docker run --rm postgres:17.6-alpine psql "postgresql://$TOKEN@host.docker.internal:15432/appdb" -c "SELECT 1;"
docker run --rm mysql:8.4 mysql -h host.docker.internal -P 15435 -u "$TOKEN" -pxxx --ssl-mode=DISABLED -e "SELECT COUNT(*) FROM shop.orders;"
#   （15435=caching_sha2 用户、15436=mysql_native_password 用户，两种认证都覆盖）

# ③ 负向：错 token 一律拒绝
redis-cli -p 15434 -a WRONG --no-auth-warning ping        # → ERR authentication required

# ④ 隧道信息 / 掩码桥（不配 DB 密钥也能读）
export VAULTY_KEEPER_BRIDGE_ADDR=http://127.0.0.1:8972 VAULTY_KEEPER_BRIDGE_TOKEN="$TOKEN"
vaulty-keeper remote dblist      # 连接名/类型/端口

# ⑤ 交互式 db shell（TTY-only；redis 本机即用，PG/MySQL 需装 psql/mysql）
vaulty-keeper db shell cache

# ⑥ 审计日志（成功 authenticated / 拒绝 invalid bridge token；无 DSN）
cat /tmp/vaulty-keeper-dbtest-serve.log | grep dbproxy:
```

## AI / 脚本使用安全指引

### 安全模型总览

一句话：**明文出口只在用户本人终端可用（AI/脚本环境一律拒绝，`--yes` 也无法放行），掩码默认反转**。对"会主动读取密钥的同用户 AI"本工具不承诺防护（见信任边界）。

| 层 | 机制 |
|---|---|
| 静态加密 | 快照所有值 AES-256-GCM 落盘（0600，无明文）；两把独立密钥：快照密钥（非敏感值）+ 敏感值密钥（敏感值），都在系统密钥库（macOS Keychain / Windows 凭据管理器） |
| 信任边界 | 系统密钥库**不防同用户进程**（实测：同 UID 进程可无弹窗 `security find-generic-password -w` 读出两把密钥）；它防的是其他用户/其他机器/意外明文。对**故意对抗**的同用户 AI，用「容器隔离部署」把 AI 放进摸不到密钥的隔离域 |
| 掩码代理 | `vaulty-keeper serve`（host 持有密钥）+ `vaulty-keeper remote`（容器/隔离域内）——容器侧只拿到 `*** (n chars)` + 长度 + 指纹，**即使 `set --plain` 标记安全的 key 也不回明文**；token 门控 + 限速 |
| AI 读 | **反转默认**：`get`/`list`/`compare` 非 TTY 下默认全部掩码 `*** (n chars)`，不靠 key 名猜测；只有 `set --plain` / `mark --plain` 显式标记为安全的 key 才输出明文。明文出口（reveal、export、edit、`--reveal`、`aes decrypt`）**非交互终端一律拒绝，即使 `--yes`**——只在用户本人 TTY 可用 |
| AI 写 | `set`/`unset`/`mark`/`import` 安全（写入即加密），无需 `--yes` |
| DB 隧道 | `vaulty-keeper db add` 只加密 URL（独立 DB 密钥 + `~/.vaulty/db.json`，0600），并为每条连接生成**专属隧道 token**；`serve` 起 TCP 隧道在握手注入真实凭据，客户端用专属 token（旧连接回退 bridge token，PG/MySQL username 字段 / Redis AUTH）；`db regen` 可轮换 token；DSN 永不离开 host、不进日志/回包 |
| Web UI | 仅 127.0.0.1 + 随机 token 门控写操作/明文出口，GET 只返回掩码（未标记安全的 key 一律掩码）；**明文接口（reveal/export/明文编辑/AES 解密）默认禁用**，需 `--allow-plaintext` 显式开启，否则带 token 也返回 403；token 失败限速（指数退避） |
| 防破解 | 指纹为 HMAC-SHA256（密钥为快照密钥），密钥不泄露时无法离线枚举弱值匹配掩码指纹；token 为 128 位随机 |
| 判断一致性 | 用 `compare`（掩码 + 长度 + 指纹），不要 `get` 明文 |

vaulty-keeper 的所有子命令在非 TTY（脚本 / AI agent）下均可正常使用，且 `--json` 输出为 AI 友好格式。但明文一旦出现在 stdout，就会进入对话上下文、会话日志（如 `~/.codex`、终端 scrollback），可能被持久化或同步。请区分安全与危险命令：

**安全（默认掩码，放心给 AI/脚本用）**
- `apollo list <env> --appid xx [--json]` — 未标记安全的 key 显示 `*** (n chars)`，不靠 key 名猜测
- `apollo compare <a> <b> --appid xx --appid-to yy [--json]` — 未标记安全的值掩码 + 长度
- `apollo get <env> <key>` — 未标记安全的 key 输出 `*** (n chars)`
- `apollo set/unset/mark`、`init`、`rm --yes` — 写入/删除类，安全
- `remote list|get|compare` — 经掩码代理读，**永远只有掩码**（即使 key 标记为安全）
- `db list` / `remote dblist` — 只列连接名/类型/端口，**不返回 URL**
- `db add` — 只写入（加密），安全；URL 从 stdin 读，不进 argv/shell history

**需要放行给 AI 的 key**：先显式标记为安全，AI 才看得到明文（例如 `APP_NAME`、`LOG_LEVEL` 这类确定无敏感内容的值）：
- `apollo set <env> <key> <value> --plain`（改值时同时标记）
- `apollo mark <env> <key> --plain`（不改值，只标记）

**防误标守卫**：`set --plain` / `mark --plain` 时若 key 名或值命中敏感规则（password/token/secret/JWT/带凭据 URI），**非 TTY 一律拒绝**、TTY 下需二次确认——防止把敏感 key 误标成安全而泄漏给 AI。

**危险（输出明文；只在交互式终端（TTY）可用，AI/脚本环境一律拒绝，加 `--yes` 也无法放行）**
- `apollo reveal <env> <key>` → 解密后的明文
- `apollo export <env>` → 全量明文
- `apollo list/compare --reveal` → 明文
- `aes decrypt` → 明文

明文命令在非交互终端（脚本 / AI agent）下**无条件拒绝**，即使显式加 `--yes` 也不输出——AI 被诱导要求也无法拿到明文。明文只在用户本人终端（TTY）可见，且会进入终端会话记录，用完注意清理。

其他注意事项：
- **密钥不进 AI 环境**：快照密钥走 macOS Keychain（`vaulty-keeper apollo init`），敏感值密钥走 Keychain（`vaulty-keeper sensitive init`），不要在 AI 会话里 `export VAULTY_KEEPER_APOLLO_KEY` / `VAULTY_KEEPER_SENSITIVE_KEY`——AI 拿到快照密钥能解非敏感值，拿到敏感值密钥能解全部敏感值。`VAULTY_KEEPER_AES_KEY` / `VAULTY_KEEPER_AES_IV` 同理，不要作为 `--key`/`--iv` 命令行参数传（会出现在 `ps` 与 shell history）。注意：与 AI 同权限的进程可以读取 `~/.vaulty/aes.json`（明文 AES key/iv）与 Keychain 项（`security find-generic-password -w`），真正的隔离是把密钥放在 AI 进程读不到的地方（不同账号/沙箱）。
- `import` 在快照已存在时会拒绝覆盖（TTY 下询问确认）；脚本 / AI 需要覆盖时显式加 `--force`，避免静默丢失旧快照。
- 需要判断两个环境某 key 是否一致时用 `compare`（掩码 + 长度即可判断），不要 get 明文。

## 验证

- `internal/aesx`: 与 `tools/javaref/CryptoUtil.java`（Java 8 参考实现）生成的向量逐字节对齐（GCM 确定性），另覆盖 key 长度校验、错误 key/iv、非法 base64。
- `internal/apollo`: 真实粘贴样例（含合并行）、注释、首个 `=`、URL 参数不误拆、快照加密落盘（文件无明文、权限 0600）、diff、敏感识别。
- `internal/cli`: 参数混排、import 自动取名、reveal（敏感值明文 / 显式 --key/--iv 解密外部密文 / 多 key JSON）、edit（假编辑器脚本）、list/compare JSON、gen-key 可用性、aes --name 列表、completion。
- 重新生成 Java 向量：`cd tools/javaref && javac CryptoUtil.java && java CryptoUtil encrypt <key> <iv> <plaintext>`

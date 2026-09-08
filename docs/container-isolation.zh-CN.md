# 容器隔离部署（防"故意对抗"AI，跨 macOS / Windows）

> 中文 | [English](container-isolation.md)

如何用仓库提供的 Docker 配置把 AI agent 与宿主密钥/密文隔离，以及不用 Docker 的替代方案。安装与快速开始见 [README](../README.zh-CN.md)；统一安全边界见[安全模型](security-model.zh-CN.md)。

掩码和 TTY 门禁不能约束恶意同用户进程。隔离必须把密钥和密文放在 agent 不可访问的位置；仓库 Docker 配置是一种起点（Docker Desktop 使用 Linux VM），但不强制限制网络出口，也不阻止访问已授权的数据库内容。详见[安全模型](security-model.zh-CN.md)。

```
[Docker 容器：codex / claude / opencode / pi]
      │  vaulty-keeper remote list|get|compare（只拿掩码）
      ▼
[Host：持有密钥]
      vaulty-keeper serve --addr 0.0.0.0:8970   ← 快照 API 掩码值；DB 隧道返回数据
      ▼
      系统密钥库 + ~/.vaulty/（提供的 compose 不挂载这些路径）
```

## Host 侧：启动掩码代理

人工宿主终端 1：保持 `serve` 运行。使用可信且由防火墙限制的接口；下方绑定所有接口。需要隧道时先注册数据库再启动。

```sh
vaulty-keeper serve --addr 0.0.0.0:8970     # 打印 token 并写入 ~/.vaulty/bridge-token
```

- 快照 API 的值始终掩码，**即使 `set --plain` 标记为 safe 也不回明文**。JSON list/compare 响应包含长度/指纹；`remote get` 只打印掩码。
- 所有 `/api` 端点都要 token（0600 写入 `~/.vaulty/bridge-token`）；失败检查每次增加 50 ms 延迟，上限 2 秒。该 token 也授权新旧 PG/MySQL/Redis 隧道连接，不是无害的元数据 token。
- `0.0.0.0` 使 Docker 可访问宿主，也把明文 HTTP 和隧道监听暴露给可达网络。token 检查不加密传输，也不使局域网暴露变安全；必须用网络控制限制访问。仅宿主使用时绑定 `127.0.0.1`。

## 容器侧：agent 隔离域

人工宿主终端 2，从仓库目录执行：选择不含秘密文件的项目目录。以下 token 交付是一项授权决定；入口脚本只打印 `<set>`/`<unset>` 占位标记，从不输出 token 本身。镜像在 Docker 内构建 Go，默认不安装可选 agent CLI 或数据库客户端。

```sh
# 在 Docker 内从源码构建，无需宿主先 make build
docker build -t vaulty-keeper-agent:local .

# 人工宿主交付：授权快照元数据及 PG/MySQL/Redis 数据库访问
export VAULTY_KEEPER_BRIDGE_TOKEN="$(cat ~/.vaulty/bridge-token)"
export VAULTY_KEEPER_PROJECT_DIR="$PWD"   # 当前仓库，挂载前检查内容
docker compose up -d

# 无需安装 agent CLI 即可使用，列出宿主桥接服务的快照
docker compose exec agent vaulty-keeper remote list
docker compose exec agent vaulty-keeper remote dblist
```

需要 `codex` 时，在创建容器前设置 `VAULTY_KEEPER_INSTALL_AGENTS='@openai/codex'`，单独完成其登录/配置，再执行 `docker compose exec agent codex`。原生数据库命令还要求客户端环境安装 `psql`、MySQL `mysql`、`redis-cli` 或 `mongosh`。由宿主执行 `db connect <name> --container` 生成命令，仅交付获授权的隧道凭据，不交付宿主加密密钥。

隔离要点（`docker-compose.yml` 已内置）：

- **不显式挂载** `~/.vaulty`、系统密钥库、`~/.ssh` 或 Docker socket。不要选择含这些文件的项目目录，也不要通过环境变量传入加密密钥而破坏隔离。
- 非 root 用户 + `cap_drop: ALL` + `no-new-privileges`
- `VAULTY_KEEPER_BRIDGE_ADDR` / `VAULTY_KEEPER_BRIDGE_TOKEN` 配置桥接访问；compose **没有**把桥接服务设为唯一网络目的地。
- 装 agent CLI：`VAULTY_KEEPER_INSTALL_AGENTS='@openai/codex @anthropic-ai/claude-code opencode-ai'`（进容器时自动 npm 安装到用户目录）
- **持久化**：`agent-home` 命名卷挂到 `/home/agent`，CLI 与会话历史跨重建保留。实际卷名取决于 Compose 项目；确需清空历史和已安装工具时，先解除容器使用，再仅删除核实过的该卷。
- **Linux 兼容**：compose 已加 `extra_hosts: host.docker.internal:host-gateway`（macOS/Windows 的 Docker Desktop 本就提供，无影响）

## 隔离能做什么、不能做什么

挂载、权限和凭据遵守上述边界时，容器没有直接访问宿主密钥存储或快照文件的路径。但它仍可能读取挂载项目中的秘密、使用 bridge token 连接 PG/MySQL/Redis、查询业务数据，或把可访问的数据发送到网络。这些属于独立权限，不是加密失效。

**Docker 本身不是绝对隔离**：减少 capability 和 `no-new-privileges` 可降低攻击面，但 daemon 权限和容器逃逸仍是风险。更强威胁需要另行评估账号/VM/沙箱及网络控制；此处配置没有可量化的防护成功率证据。

## Windows 用户

- 同一套 compose/镜像；Windows 版 Docker Desktop 底层是 WSL2，`host.docker.internal` 同样可用
- 密钥存 **Windows 凭据管理器**（`vaulty-keeper apollo init` / `sensitive init` 自动适配，无需 `security` 命令）
- 明文 CLI 门禁检查 stdin 控制台状态（Windows 用 `GetConsoleMode`），不认证真人；当前没有交互菜单。非 TTY 本地读取掩码未放行值，桥接快照读取始终掩码。

## 不用 Docker 的替代用法

`serve` + `remote` 本身与 Docker 无关，隔离域可以是任何「摸不到密钥和密文」的环境：

**① 本机直接跑（无隔离，防"守规矩"的 AI）**

终端 1 执行 `vaulty-keeper serve --addr 127.0.0.1:8970` 并保持运行。同一宿主终端 2 执行：

```sh
export VAULTY_KEEPER_BRIDGE_ADDR=http://127.0.0.1:8970
vaulty-keeper remote list   # 未设环境覆盖时读取宿主 token 文件
```

AI 与你在同一账号下时，靠的是掩码 + TTY 门禁；对会主动读密钥的 AI 不设防。

**② 独立 macOS 账号（真隔离，替代 Docker）**

通过 macOS 账号设置创建标准非管理员 `ai` 账号，并检查文件权限，不要把真实密码写入 shell 命令。另行安装 `codex` 后，人工宿主可显式委托 bridge token：

```sh
sudo -u ai env VAULTY_KEEPER_BRIDGE_ADDR=http://127.0.0.1:8970 \
  VAULTY_KEEPER_BRIDGE_TOKEN="$(cat ~/.vaulty/bridge-token)" codex
```

独立账号不应持有宿主密钥，也不应可读宿主 0700 的 `~/.vaulty/`。需验证权限和其他共享文件；委托的 token 仍授权 PG/MySQL/Redis 访问。账号凭据、agent 安装及文件权限须自行管理。

**③ 远程机器 / WSL2**

将 agent 放在独立受控机器/VM，并把桥接/隧道可达性限制在可信网络。仅使用 WSL2 不保证与 Windows 宿主文件隔离。token 门控的快照掩码不保护明文传输，也不脱敏数据库结果。

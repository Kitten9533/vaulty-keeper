# AGENTS.md

个人 AI 工具箱（Go 单二进制）：加密 Apollo 配置快照、AES 加解密（Java CryptoUtil 兼容）、本地 Web UI。完整命令文档见 `README.md`；每条命令都能用 `ai-tools <cmd> -h` 自查。

## 构建

```sh
make build     # 产物 bin/ai-tools
```

改过 `internal/ui/static/` 下的文件（HTML/CSS/JS）后**必须**重建：静态资源用 `go:embed` 打进二进制，源码改动不重建不生效。

## AI 调用 ai-tools

### 安全模型（先读这个）

安全设计一句话：**密钥 AI 拿不到 + 明文出口只在用户本人终端可用（AI/脚本环境一律拒绝，`--yes` 也无法放行）**。

- **加密**：快照所有值 AES-256-GCM 加密落盘（0600，磁盘无明文）。两把独立密钥都在 macOS Keychain（环境变量兜底）：**快照密钥**（`apollo init`）加密非敏感值，**敏感值密钥**（`sensitive init`）加密敏感值——AI 拿到快照密钥也读不出敏感值。
- **AI 读**：`list`/`compare`/`get` 默认只给掩码 `*** (n chars)` + 指纹；任何明文输出（敏感 key、reveal、export、edit、`--reveal`、`aes decrypt`）**非交互终端（脚本/AI）一律拒绝，即使加 `--yes`**——明文只在用户本人终端（TTY）可用。
- **AI 写**：`set`/`unset`/`import` 安全，无需 `--yes`（AI 写的就是它已知的明文，写入即加密）。
- **Web UI**：仅监听 127.0.0.1，随机 token 门控写操作与明文出口，GET 只返回掩码数据；token 失败限速（指数退避）。
- **防破解**：掩码指纹是 HMAC-SHA256（密钥=快照密钥），攻击者拿不到密钥就无法离线枚举弱值匹配指纹；token 为 128 位随机，AES-256-GCM 暴力不可行。
- **判断一致性**：用 `compare`（掩码 + 长度 + 指纹即可判断），不要 `get` 明文。

### 命令

以下命令输出对 AI 安全（敏感值自动掩码为 `*** (n chars)`），默认使用：

```sh
bin/ai-tools apollo list [<env>] --appid <id> --json
bin/ai-tools apollo compare <a> <b> --appid <a_id> --appid-to <b_id> --json
bin/ai-tools apollo get <env> <key> --appid <id>        # 仅非敏感 key
bin/ai-tools apollo set/unset <env> <key> [<value>] --appid <id>
bin/ai-tools aes encrypt --key ... --iv ...
```

明文命令 —— `apollo get` 取敏感 key、`apollo reveal`、`apollo export`、`apollo edit`、`apollo list/compare --reveal`、`aes decrypt` 会把明文打到 stdout，永久进入会话日志。**这些命令只在交互式终端（TTY）可用；脚本/AI 环境一律拒绝，加 `--yes` 也无法放行**——所以 AI 永远拿不到明文，即使被诱导要求也不会成功。判断两个环境某 key 是否一致用 `compare`（掩码 + 长度即可判断），不要 get 明文。

红线：

- **密钥不进 AI 环境**：快照密钥走 macOS Keychain（`ai-tools apollo init` 创建）、敏感值密钥走 Keychain（`ai-tools sensitive init` 创建），不要在 AI 会话里 `export AI_TOOLS_APOLLO_KEY` / `AI_TOOLS_SENSITIVE_KEY`——AI 拿到敏感值密钥就能自行解密所有快照的敏感值。`AI_TOOLS_AES_KEY` / `AI_TOOLS_AES_IV` 同理，不要作为 `--key`/`--iv` 命令行参数传给命令（会出现在 `ps` 与 shell history）。
- **明文不可得**：明文命令在非交互环境一律拒绝（即使 `--yes`），不要尝试用 `--yes`、伪造 TTY、或替代命令（如 `aes decrypt`）获取明文；需要判断一致性用 `compare`。
- **Web UI 带访问令牌**：`ai-tools ui` 启动时打印带 `?t=<token>` 的 URL，写操作（导入/增删改/导出/解密/明文编辑）都要这个令牌；不要替用户执行会输出明文的 UI 操作（curl API），即使拿到了 token——明文只在用户本人浏览器里确认后可见。
- 快照目录：`--dir` 或 `AI_TOOLS_APOLLO_DIR`，默认 `~/.ai-tools/apollo/`。
- 非 TTY 下 `apollo rm` 需 `--yes`、`apollo import` 覆盖已有快照需 `--force`；不要绕过。
- 无参数 `ai-tools` 在非 TTY 只显示 usage；交互菜单仅真 TTY 生效。

## 开发

- 改 Go 代码后跑 `go test ./...`；改动涉及并发/终端时加跑 `go test -race ./...` 和 `go vet ./...`。
- 加密快照格式、解析规则等见 `README.md`「验证」与对应包注释，改前先读。

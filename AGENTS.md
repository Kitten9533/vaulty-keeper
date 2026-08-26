# AGENTS.md

个人 AI 工具箱（Go 单二进制）：加密 Apollo 配置快照、AES 加解密（Java CryptoUtil 兼容）、本地 Web UI。完整命令文档见 `README.md`；每条命令都能用 `ai-tools <cmd> -h` 自查。

## 构建

```sh
make build     # 产物 bin/ai-tools
```

改过 `internal/ui/static/` 下的文件（HTML/CSS/JS）后**必须**重建：静态资源用 `go:embed` 打进二进制，源码改动不重建不生效。

## AI 调用 ai-tools

以下命令输出对 AI 安全（敏感值自动掩码为 `*** (n chars)`），默认使用：

```sh
bin/ai-tools apollo list [<env>] --appid <id> --json
bin/ai-tools apollo compare <a> <b> --appid <a_id> --appid-to <b_id> --json
bin/ai-tools apollo get <env> <key> --appid <id>        # 仅非敏感 key
bin/ai-tools apollo set/unset <env> <key> [<value>] --appid <id>
bin/ai-tools aes encrypt --key ... --iv ...
```

明文命令 —— `apollo get` 取敏感 key、`apollo reveal`、`apollo export`、`apollo edit`、`apollo list/compare --reveal`、`aes decrypt` 会把明文打到 stdout，永久进入会话日志。非 TTY（脚本/AI）下这些命令**必须显式加 `--yes` 才输出明文**，否则直接报错。只在用户明确要求时加 `--yes` 执行，取完即从上下文删除。判断两个环境某 key 是否一致用 `compare`（掩码 + 长度即可判断），不要 get 明文。

红线：

- **密钥不进 AI 环境**：快照密钥优先走 macOS Keychain（`ai-tools apollo init` 创建），不要在 AI 会话里 `export AI_TOOLS_APOLLO_KEY`——AI 拿到它就能自行解密所有快照。`AI_TOOLS_AES_KEY` / `AI_TOOLS_AES_IV` 同理，不要作为 `--key`/`--iv` 命令行参数传给命令（会出现在 `ps` 与 shell history）。
- **Web UI 带访问令牌**：`ai-tools ui` 启动时打印带 `?t=<token>` 的 URL，写操作（导入/增删改/导出/解密/明文编辑）都要这个令牌；不要替用户执行会输出明文的 UI 操作（curl API），除非用户明确要求并给了 token。
- 快照目录：`--dir` 或 `AI_TOOLS_APOLLO_DIR`，默认 `~/.ai-tools/apollo/`。
- 非 TTY 下 `apollo rm` 需 `--yes`、`apollo import` 覆盖已有快照需 `--force`；不要绕过。
- 无参数 `ai-tools` 在非 TTY 只显示 usage；交互菜单仅真 TTY 生效。

## 开发

- 改 Go 代码后跑 `go test ./...`；改动涉及并发/终端时加跑 `go test -race ./...` 和 `go vet ./...`。
- 加密快照格式、解析规则等见 `README.md`「验证」与对应包注释，改前先读。

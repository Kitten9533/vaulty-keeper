# ai-tools

个人 AI 工具箱（Go 单二进制，零外部依赖）。所有值在本地磁盘上都是加密存储，密钥不进配置中心、不进明文文件。`ai-tools ui` 提供本地 Web UI 覆盖全部快照与 AES 工具（仅本机监听）。

## 安装

```sh
make build          # → bin/ai-tools
make install        # 软链到 ~/.local/bin/ai-tools（已在 PATH）
make test           # 单测（含 Java↔Go 互操作向量）
```

## 交互模式（推荐手动操作）

直接在终端运行 `ai-tools`（无参数），进入 chalk 风格彩色对话菜单：

```sh
ai-tools
```

- **彩色界面**：标题/选项/提示符/成功/失败/对比结果均有配色；`NO_COLOR=1` 或非 TTY 时自动关闭。
- **快捷键**：
  - `↑`/`↓` 或 `k`(上)/`j`(下) 移动高亮，`回车` 确认
  - `1`–`14` 数字直选（`10`–`14` 两位数会自动组合）
  - `h` 帮助，`q`/`Ctrl-C`/`Ctrl-D` 退出
- **自定义 key/iv**（选项 13）：手动输入 AES key/iv，保存到 `~/.ai-tools/aes.json`（0600）；之后 AES 加密/解密/一键解密自动用它作默认值（回车即用，可重新输入覆盖），下次启动自动加载。
- **搜索式选择器**：所有"输入快照名/key"的地方都是 fzf 风格选择器——顶部输入框实时过滤（不区分大小写），第一项永远是 `✏️ 自己输入`（输入内容回车即用），后面列出全部可选项；`↑/↓` 或 `j/k` 选择、数字直选、回车确认、空输入回车取消当前步骤。
- **多行粘贴**：导入和 AES 加密支持多行粘贴，单独一行输入 `end`（或 Ctrl-D）结束。
- 输入 `cancel` 随时取消，快照名可直选已有快照或输入新名称。

非 TTY（脚本/AI）下无参数仍显示 usage；子命令输出仅在 TTY 着色，管道/JSON 保持纯文本。

## 本地 Web UI

```sh
ai-tools ui
ai-tools ui --dir /path/to/snapshots --port 8080
ai-tools ui --no-open
```

- 仅监听 `127.0.0.1`，不会暴露到局域网。
- 启动时生成随机访问令牌，URL 形如 `http://127.0.0.1:8080/?t=<token>`：所有写操作（导入/增删改/导出/解密/明文编辑）都要求携带该令牌，本机其他进程（如 AI agent）无法通过 curl 直接导出明文。**请用启动时打印的完整 URL 打开**；只敲 `localhost:8080` 时读操作可用，但写操作会被拒绝。
- 覆盖全部功能：快照浏览/搜索/增删改、导入、环境对比、明文编辑、导出（下载或复制）、AES 加解密与 key/iv 管理、快照密钥初始化。
- 明文出口（reveal / 明文编辑 / 导出 / AES 解密）均需二次确认后才显示，响应 `Cache-Control: no-store`，浏览器不持久化明文。
- 浏览器端不持久化快照内容。

## ai-tools apollo — Apollo 快照工具

Apollo Open API 不可用时的替代方案：从 Apollo 门户**复制键值对**，导入加密快照，AI/脚本安全地读取、对比、修改。快照默认存 `~/.ai-tools/apollo/<name>.json`（`--dir` 或环境变量 `AI_TOOLS_APOLLO_DIR` 覆盖）。

```sh
ai-tools apollo init                          # 首次：生成快照密钥（macOS Keychain）
ai-tools apollo import prod.txt --app-id xx   # 解析粘贴内容；--app-id 必填；--name 省略时自动取文件名；已存在时需 --force 覆盖
ai-tools apollo import - --name prod --app-id xx   # 从 stdin 读
ai-tools apollo list                          # 列出快照（环境 + AppID）
ai-tools apollo list prod --appid xx          # 敏感 key 值显示 *** (长度)，--reveal 显示明文
ai-tools apollo list prod --appid xx --json   # JSON 输出（AI 友好）
ai-tools apollo get prod --appid xx PASSWORD_SALT
ai-tools apollo set prod --appid xx SOME_KEY value
ai-tools apollo unset prod --appid xx SOME_KEY
ai-tools apollo compare prod test --appid xx --appid-to yy   # added/removed/changed，敏感值默认掩码
ai-tools apollo compare prod test --json
ai-tools apollo reveal prod --appid xx imile.fs.oss.secret-key   # 一键解密（读同一快照的 imile.fs.aes.*）；非 TTY 加 --yes
ai-tools apollo edit prod --appid xx         # $EDITOR 打开明文编辑，保存后自动重新加密
ai-tools apollo export prod --appid xx       # 解密全量输出，供粘贴回 Apollo；非 TTY 加 --yes
ai-tools apollo export prod --appid xx --copy # 直接复制到剪贴板（pbcopy）
ai-tools apollo rm prod --appid xx           # 删除快照（TTY 确认；非 TTY 需 --yes）
```

快照以「环境 + AppID」为唯一键，存储为 `{env}__{appid}.json`；旧版无 AppID 的 `{env}.json` 仍可读取（不指定 `--appid` 时访问）。

解析规则：

- 每行 `KEY = value`，按第一个 `=` 分割，两侧去空格（value 可含 `=`）。
- 空行、行首 `#` 的整行（单行/多行注释）跳过。
- 一行内粘在一起的多个 `KEY = ` 条目自动拆分并警告（如 `A = 1B = 2`）。
- key 校验 `[A-Za-z_][A-Za-z0-9_.-]*`，非法行跳过并警告。

密钥：`AI_TOOLS_APOLLO_KEY`（base64，32 字节）优先，否则 macOS Keychain。文件权限 0600，值为 AES-256-GCM + 每条独立随机 nonce。

敏感识别（默认掩码，`--reveal` 显示）：`password|passwd|pwd|token|secret|salt|credential|private|access[_-]?key|secret[_-]?key|api[_-]?key`（不区分大小写）。宁多掩不漏掩，`--reveal` 可补救。

## ai-tools aes — AES 加解密（Java CryptoUtil 兼容）

用于 Apollo 里 OSS AK/SK 的加密/解密（`imile.fs.aes.iv` + `imile.fs.aes.secret-key`）。算法对齐 `CryptoUtil.java`：AES/GCM/NoPadding、tag 128 bits、key 为 UTF-8 字节（16/24/32）、iv 为 UTF-8 字节直接作 GCM IV、密文为 Base64。

```sh
# 一键解密（无需手动拼装 key/iv）
ai-tools apollo reveal prod imile.fs.oss.secret-key
ai-tools apollo reveal prod imile.fs.oss.secret-key imile.fs.oss.access-key-id

# 等价的手动拼装
ai-tools aes decrypt --key "$(ai-tools apollo get prod imile.fs.aes.secret-key)" \
                     --iv "$(ai-tools apollo get prod imile.fs.aes.iv)" \
                     <<< "$(ai-tools apollo get prod imile.fs.oss.secret-key)"

# 生成新 key/iv（上线新配置前加密用）
ai-tools aes gen-key
ai-tools aes gen-key --bytes 32 --iv-bytes 12
```

key/iv 也可走环境变量 `AI_TOOLS_AES_KEY` / `AI_TOOLS_AES_IV`（避免进 shell history）。输入可走 `--file`、参数或 stdin。

## 其他

```sh
ai-tools ui                              # 启动本地 Web UI（默认 127.0.0.1:8080，占用时自动顺延）
ai-tools completion zsh | source /dev/stdin   # 或 bash / fish，加到 shell 配置
ai-tools version
```

## AI / 脚本使用安全指引

ai-tools 的所有子命令在非 TTY（脚本 / AI agent）下均可正常使用，且 `--json` 输出为 AI 友好格式。但明文一旦出现在 stdout，就会进入对话上下文、会话日志（如 `~/.codex`、终端 scrollback），可能被持久化或同步。请区分安全与危险命令：

**安全（敏感值自动掩码，放心给 AI/脚本用）**
- `apollo list <env> --appid xx [--json]` — 敏感 key 显示 `*** (n chars)`
- `apollo compare <a> <b> --appid xx --appid-to yy [--json]` — 敏感值掩码 + 长度
- `apollo get` 只取非敏感 key、`apollo set/unset`、`init`、`rm --yes`

**危险（输出明文；非 TTY 下必须显式 `--yes` 才放行，否则报错）**
- `apollo get <env> SECRET_TOKEN --yes` → 明文落 stdout
- `apollo reveal <env> <key> --yes` → 解密后的明文
- `apollo export <env> --yes` → 全量明文
- `apollo list/compare --reveal --yes` → 明文
- `aes decrypt --yes` → 明文

`--yes` 门槛意味着：脚本 / AI **无法意外**拿到明文，只有显式加 `--yes` 才会输出——即便如此，明文仍会永久进入会话日志，非必要不用，用完即从上下文删除。

其他注意事项：
- **密钥不进 AI 环境**：快照密钥优先走 macOS Keychain（`ai-tools apollo init`），不要在 AI 会话里 `export AI_TOOLS_APOLLO_KEY`——AI 拿到它就能自行解密全部快照。`AI_TOOLS_AES_KEY` / `AI_TOOLS_AES_IV` 同理，不要作为 `--key`/`--iv` 命令行参数传（会出现在 `ps` 与 shell history）。注意：与 AI 同权限的进程可以读取 `~/.ai-tools/aes.json`（明文 AES key/iv）与 Keychain 项（`security find-generic-password -w`），真正的隔离是把密钥放在 AI 进程读不到的地方（不同账号/沙箱）。
- `import` 在快照已存在时会拒绝覆盖（TTY 下询问确认）；脚本 / AI 需要覆盖时显式加 `--force`，避免静默丢失旧快照。
- 需要判断两个环境某 key 是否一致时用 `compare`（掩码 + 长度即可判断），不要 get 明文。

## 验证

- `internal/aesx`: 与 `tools/javaref/CryptoUtil.java`（Java 8 参考实现）生成的向量逐字节对齐（GCM 确定性），另覆盖 key 长度校验、错误 key/iv、非法 base64。
- `internal/apollo`: 真实粘贴样例（含合并行）、注释、首个 `=`、URL 参数不误拆、快照加密落盘（文件无明文、权限 0600）、diff、敏感识别。
- `internal/cli`: 参数混排、import 自动取名、reveal（快照内 AES 配置 / 显式 --key/--iv / 多 key JSON）、edit（假编辑器脚本）、list/compare JSON、gen-key 可用性、completion。
- 重新生成 Java 向量：`cd tools/javaref && javac CryptoUtil.java && java CryptoUtil encrypt <key> <iv> <plaintext>`

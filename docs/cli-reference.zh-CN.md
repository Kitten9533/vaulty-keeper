# vaulty-keeper CLI 参考

> 中文 | [English](cli-reference.md)

`vaulty-keeper` 的完整命令参考：Apollo 快照工具、AES 加解密辅助命令、数据库隧道与杂项命令。安装与快速开始见 [README](../README.zh-CN.md)；用法讲解见[文档索引](README.zh-CN.md)。本文是**参考，不是脚本**：`<...>`、`[...]`、`a|b` 表示占位/可选项，不能原样当 shell 输入。请替换文件名、环境名、AppID 和 key。

## vaulty-keeper apollo — Apollo 快照工具

讲解 Apollo 快照的实现（加密文件结构 / 双密钥分工 / 敏感识别 / 掩码与指纹 / 显式放行）与实测示例见 **[`apollo-snapshot-guide.zh-CN.md`](apollo-snapshot-guide.zh-CN.md)**（[English](apollo-snapshot-guide.md)）。

Apollo Open API 不可用时的替代方案：从 Apollo 门户复制键值对并导入加密快照。AI/脚本访问遵循 [README 安全使用指南](../README.zh-CN.md#安全使用指南ai-与脚本) 的掩码及写入权限边界。快照默认存于 `~/.vaulty/apollo/`（`--dir` 或环境变量 `VAULTY_KEEPER_APOLLO_DIR` 覆盖）。

完成人工宿主密钥初始化（见 README）后，以下完整示例仅用**合成值**，在新的临时目录创建两份快照。所有命令在同一终端执行，保留目录变量：

```sh
DEMO_SNAP_DIR=$(mktemp -d)
printf '%s\n' 'APP_NAME = demo' 'LOG_LEVEL = info' 'SECRET_TOKEN = synthetic-prod' \
  | vaulty-keeper apollo import - --dir "$DEMO_SNAP_DIR" --name prod --appid demo
printf '%s\n' 'APP_NAME = demo' 'LOG_LEVEL = debug' 'SECRET_TOKEN = synthetic-test' \
  | vaulty-keeper apollo import - --dir "$DEMO_SNAP_DIR" --name test --appid demo
vaulty-keeper apollo list prod --dir "$DEMO_SNAP_DIR" --appid demo --json </dev/null
vaulty-keeper apollo compare prod test --dir "$DEMO_SNAP_DIR" --appid demo --appid-to demo --json </dev/null
```

对比会报告 `LOG_LEVEL` 和 `SECRET_TOKEN` 变化，值为掩码。这使用宿主密钥存储，不是隔离密钥夹具。

```sh
vaulty-keeper apollo init                          # 首次：生成快照密钥（系统密钥库，如 macOS Keychain / Windows 凭据管理器 / Linux Secret Service）
vaulty-keeper sensitive init                       # 首次：生成敏感值密钥（独立于快照密钥）
vaulty-keeper apollo import prod.txt --appid xx    # 解析粘贴内容；--appid 必填；--name 省略时自动取文件名；已存在时需 --force 覆盖
vaulty-keeper apollo import - --name prod --appid xx   # 从 stdin 读（旧写法 --app-id 仍兼容）
vaulty-keeper apollo list                          # 列出快照（环境 + AppID）
vaulty-keeper apollo list --json                   # catalog JSON：{snapshots:[{name, app_id}, ...]}
vaulty-keeper apollo list test merdi --names --json # 只打 key 名，不解密；第二个位置参数是 appid
vaulty-keeper apollo list prod --appid xx          # 非 TTY 未放行值掩码；--reveal 要求 stdin TTY
vaulty-keeper apollo list prod --appid xx --json   # JSON 值（AI 友好）
vaulty-keeper apollo get prod --appid xx SOME_KEY  # 非 TTY 下只对标记为安全的 key 输出明文，其余掩码
vaulty-keeper apollo set prod --appid xx SOME_KEY value
vaulty-keeper apollo set prod --appid xx SOME_KEY value --plain    # 显式标记为安全：AI/脚本可读明文
vaulty-keeper apollo set prod --appid xx SOME_KEY value --secret   # 敏感分类；不放行默认非 TTY 输出
vaulty-keeper apollo mark prod --appid xx SOME_KEY --plain|--secret  # 不改值，只翻转安全/敏感标记
vaulty-keeper apollo unset prod --appid xx SOME_KEY
vaulty-keeper apollo compare prod test --appid xx --appid-to yy   # added/removed/changed，默认掩码规则见下
vaulty-keeper apollo compare prod test --appid xx --appid-to yy --json
vaulty-keeper apollo reveal prod --appid xx SECRET_TOKEN          # 显示敏感值明文（仅 TTY）
vaulty-keeper apollo reveal prod --appid xx app.fs.oss.secret-key --key <aes> --iv <aes>   # 解密外部 AES 密文（仅 TTY）
vaulty-keeper apollo edit prod --appid xx         # $EDITOR 打开明文编辑，保存后自动重新加密（仅 TTY）；也接受 `edit prod merdi`
vaulty-keeper apollo export prod --appid xx       # 解密全量输出，供粘贴回 Apollo（仅 TTY）；也接受 `export prod merdi`
vaulty-keeper apollo export prod --appid xx --copy # 先打印，再用 macOS pbcopy 复制（仅 TTY）
vaulty-keeper apollo rm prod --appid xx           # 删除快照（TTY 确认；非 TTY 需 --yes）；也接受 `rm prod merdi --yes`
```

> 明文命令（`reveal`/`export`/`edit`/`list|compare --reveal`/`aes decrypt`）要求 **stdin 为 TTY**，`--yes` 不绕过此检查。这是防误操作门禁，不是真人认证，也不检查 stdout。TTY 下 `get` 可以直接输出明文。agent 不得调用真实秘密的明文出口或伪造 TTY。
>
> **反转默认**：非 TTY 下 `get`/`list`/`compare` 对未显式 safe（`set --plain` 或 `mark --plain`）的值掩码。safe 是允许输出明文的授权，不只是非敏感分类。普通 TTY list/compare 也可能显示非敏感值。

CLI `compare --json` 在无变化时仍输出文本消息，不是 JSON。需要桥接指纹时用 `remote list <env> --appid <id> --json`；`remote get` 只打印掩码值字符串。尽管 CLI 标作 `chars`，长度实际是 UTF-8 **字节数**。指纹使用同一快照 HMAC 密钥对归一化值计算并截断为 8 字节，是高置信比较信号，不是原始字节完全相同的证明。CLI 和 HTTP JSON 结构不同。

CLI 导入覆盖已有快照需要 TTY 确认或非 TTY 显式 `--force`；UI 导入重名环境/AppID 返回 409。导入替换和整份编辑都会重建条目、重新分类，不保留全部 safe/secret 标记。省略或无法解析的条目可能消失；保存后复核解析警告（并非所有编辑路径都会展示）、key 集合及分类。CLI 编辑还会创建明文临时文件，编辑器可能另留备份。

快照以「环境 + AppID」为唯一键，存储为 `{env}__{appid}.json`；旧版无 AppID 的 `{env}.json` 仍可读取（不指定 `--appid` 时访问）。

解析规则：

- 每行 `KEY = value`，按第一个 `=` 分割，两侧去空格（value 可含 `=`）。
- 空行、行首 `#` 的整行（单行/多行注释）跳过。
- 一行内以大写字母开头粘在一起的多个 `KEY = ` 条目自动拆分并警告（如 `A = 1B = 2`；只认全大写 key）。
- key 校验 `[A-Za-z_][A-Za-z0-9_.-]*`，非法行跳过并警告。

两把快照密钥（通常存于系统密钥库；非空环境变量覆盖优先）：

- **快照密钥**（`VAULTY_KEEPER_APOLLO_KEY`，`apollo init` 创建）：加密所有非敏感值。
- **敏感值密钥**（`VAULTY_KEEPER_SENSITIVE_KEY`，`sensitive init` 创建）：独立于快照密钥加密新写入的敏感值。旧格式敏感密文仍可能回退快照密钥解密；双密钥隔离承诺适用于独立新加密的数据。文件权限 0600，加密值使用 AES-256-GCM 和每条独立随机 nonce。

**Linux**：初始化需要可用的桌面 Secret Service（例如 gnome-keyring / kwallet）。无头宿主可由可信操作者通过受控秘密注入提供 `VAULTY_KEEPER_APOLLO_KEY`、`VAULTY_KEEPER_SENSITIVE_KEY` 和 `VAULTY_KEEPER_DB_KEY`，每项都须 Base64 解码后恰好 32 字节。即使 keyring 可用，环境覆盖仍优先；无效/错误覆盖不会自动回退 keyring。重新生成密钥前先检查来源，否则可能使已有数据无法解密。`openssl rand -base64 32` 会打印新秘密，真实密钥不要在 agent/录制会话中生成。将密钥写入 shell profile 会形成明文文件，即使权限 0600 也不属于加密存储。

敏感分类（导入/新 set 时使用并持久化；读取不改写，已有条目无 flag 的 `set` 保留分类）：

- **key 名命中**：`password|passwd|pwd|token|secret|salt|credential|private|access[_-]?key|secret[_-]?key|api[_-]?key`（不区分大小写）
- **值带凭据的 URI/DSN**：key 名含 `uri|url|dsn|connection|endpoint|addr|address`，且值形如 `scheme://user[:password]@host`（如 `mongodb://root:pw@...`）
- **JWT**：值形如 `eyJ...` 三段式 base64url（如 `SUPABASE_SERVICE_ROLE_KEY`、`NEXT_PUBLIC_SUPABASE_ANON_KEY`）

`MONGODB_URI` 不是名称直接命中敏感规则，而是示例中的带凭据值使其敏感。分类选择加密行为；显式 **safe** 授权独立控制普通非 TTY/UI 明文输出。不要用 `--plain` 暴露真实秘密。

## vaulty-keeper aes — AES 加解密（Java CryptoUtil 兼容）

用于解密 Apollo 里 OSS AK/SK 这类**值本身就是 CryptoUtil 密文**的配置。算法对齐 `CryptoUtil.java`：AES/GCM/NoPadding、tag 128 bits、key 为 UTF-8 字节（16/24/32）、iv 为 UTF-8 字节直接作 GCM IV、密文为 Base64。

key/iv 存在 `~/.vaulty/aes.json`（0600）的**明文命名列表**里，格式为数组 `[{name, secret-key, iv}, ...]`（旧版单对象 `{key, iv}` 读取为 `default` 条目）。CLI 用 `--name` 引用；Web UI 的 AES 工具与快照"显示"解密均为**手动输入 key/iv**（不读取列表）。快照存储加密与值本身的外部 CryptoUtil 加密是两层。UI 的外部 AES 字段只在 reveal 失败后出现，不是始终可展开的高级选项。

**新加密不得对不同消息重复使用同一 key/IV**。命名条目保留 IV，Java 兼容不代表可安全反复使用；解密必须使用原配对。下文是语法参考，不是可反复执行的真实秘密流程：字面密钥/行内环境赋值可能进入 shell history 和进程检查，stdin 不会清除上游 shell 命令。`aes gen-key` 即使非 TTY 也会打印生成的 key/IV。

```sh
# 列出 / 生成 / 添加条目（参考备选操作，不是连续脚本）
vaulty-keeper aes list
vaulty-keeper aes gen-key --name oss              # 生成、打印秘密并保存明文 aes.json
vaulty-keeper aes add --name oss --key <k> --iv <i>   # 手动保存条目

# 用列表条目加解密（decrypt 仅 TTY 输出明文）
vaulty-keeper aes encrypt --name oss 'hello'
vaulty-keeper aes decrypt --name oss '<base64>'

# 仅语法：手动参数 / 行内 env 会暴露真实秘密
vaulty-keeper aes encrypt --key <k> --iv <i> 'hello'
VAULTY_KEEPER_AES_KEY=<k> VAULTY_KEEPER_AES_IV=<i> vaulty-keeper aes decrypt '<base64>'

# 解密外部 AES 密文值（仅 TTY）
vaulty-keeper apollo reveal prod app.fs.oss.secret-key --appid xx --key <k> --iv <i>
```

输入可走 `--file`、参数或 stdin。`decrypt` 输出明文且要求 stdin TTY，管道传密文不会绕过门禁；确有需要时由人工在终端使用文件/参数。输入文件与输出各有自己的明文生命周期。

## 数据库隧道

加密连接存储与 TCP 隧道。精确 flag 见 `vaulty-keeper db -h` 和 `vaulty-keeper db <cmd> -h`。逐协议操作：[PostgreSQL](tunnel/postgres-tunnel-guide.zh-CN.md) / [MySQL](tunnel/mysql-tunnel-guide.zh-CN.md) / [Redis](tunnel/redis-tunnel-guide.zh-CN.md) / [MongoDB](tunnel/mongodb-tunnel-guide.zh-CN.md)。架构与夹具：[db-proxy-architecture](db-proxy-architecture.zh-CN.md)、[db-proxy-examples](db-proxy-examples.zh-CN.md)。安全边界：[security-model](security-model.zh-CN.md)。

```sh
vaulty-keeper db init
vaulty-keeper db add <name> [--port <port>] [--test]   # URL 从 stdin 读；隧道默认关闭，需 db on
vaulty-keeper db list [--json]
vaulty-keeper db test <name>
vaulty-keeper db connect <name> [--container] [--cmd] [--host <host>]
vaulty-keeper db regen <name>|--all
vaulty-keeper db on|off <name>|--all
vaulty-keeper db show <name>     # 仅 TTY：解密后的 URL
vaulty-keeper db shell <name>    # 仅 TTY：直接后端客户端
vaulty-keeper db rm <name> [--yes]
```

`db show` / `db shell` 是明文出口（stdin TTY）。Agent 使用 `list` / `test` / `connect` / `on` / `off` / `regen`。

## 其他

```sh
vaulty-keeper ui                              # 启动本地 Web UI（默认 127.0.0.1:8080，占用时自动顺延）
vaulty-keeper serve --addr 0.0.0.0:8970       # 掩码代理（host 持有密钥时对容器/隔离域开放）
vaulty-keeper remote list|get|compare ...     # 通过掩码代理读（形态与 apollo 子命令一致）
vaulty-keeper completion zsh | source /dev/stdin   # 或 bash / fish，加到 shell 配置
vaulty-keeper lang [en|zh]    # 查看或设置共享的 UI/CLI 语言
vaulty-keeper version
```

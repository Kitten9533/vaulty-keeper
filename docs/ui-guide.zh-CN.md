# vaulty-keeper Web UI · 功能与使用指南

> 中文 | [English](ui-guide.md)
>
> 本地 Web UI（`vaulty-keeper ui`）提供快照管理、AES 加解密及数据库隧道管理。本文描述当前 UI 流程与限制，不代表覆盖每一条 CLI 命令。
> 配套：[命令参考](../README.zh-CN.md)、[Apollo 快照](apollo-snapshot-guide.zh-CN.md)、[DB 架构](db-proxy-architecture.zh-CN.md)、[安全模型](security-model.zh-CN.md)、[文档索引](README.md)。

---

## 1 · 启动与访问

```sh
vaulty-keeper ui                    # 默认端口，被占用时自动顺延到下一个空闲端口
vaulty-keeper ui --port 8123        # 起始端口；被占用时也会顺延
vaulty-keeper ui --allow-plaintext  # 额外开启明文接口（导出 / 解密 / 明文编辑 / 查看真实 URL）
```

- 启动后打印 `http://127.0.0.1:<port>/?t=<token>` 并自动打开默认浏览器。macOS 会先在 Chrome/Arc/Edge/Brave/Chromium/Opera/Safari 中寻找已打开的 vaulty-keeper UI 标签页（带 `?t=` 的 loopback URL），优先将该标签页导航到新 URL，而不是新开标签页。
- **只监听 127.0.0.1**；token 每次启动随机生成（128 位）。**不要把带 token 的 URL 发给 AI/脚本，也不要贴进日志或 shell history**。
- **明文接口默认禁用**（顶部会有黄色横幅提示），需要 `--allow-plaintext` 重启生效；未开启时导出/解密/明文编辑/显示真实 URL 即使带 token 也会 403。
- 使用浏览器时保持 UI 进程运行；其他命令另开终端执行。UI 管理 DB 配置，但不会自行启动 TCP 隧道（§5）。
- UI 的静态资源用 `go:embed` 打进二进制：修改 `internal/ui/static/` 后需运行 `make test`（含 Node UI 检查）和 `make build` 才生效。

### Windows 启动方式

Windows 发布包名包含版本，例如 `vaulty-keeper-0.6.0-windows-x86_64.zip`；解压后得到 `vaulty-keeper.exe`，命令使用 `.exe` 后缀：

```powershell
# PowerShell / CMD，进入解压目录后：
.\vaulty-keeper.exe ui                   # 默认端口，被占用时自动顺延
.\vaulty-keeper.exe ui --port 8123       # 起始端口；被占用时也会顺延
.\vaulty-keeper.exe ui --allow-plaintext # 额外开启明文接口
```

- 启动同样打印 `http://127.0.0.1:<port>/?t=<token>`，并自动用默认浏览器打开。
- **PowerShell 里运行当前目录下的程序要加 `.\` 前缀**（直接敲 `vaulty-keeper.exe ui` 会提示找不到命令）；CMD 不需要。
- 快照、敏感值和 DB 密钥使用 **Windows 凭据管理器**，但非空环境变量覆盖优先；数据位于 `%USERPROFILE%\.vaulty\`（快照在 `.vaulty\apollo\`，DB 连接在 `.vaulty\db.json`）。独立的 AES key/IV 列表是明文 JSON，不是凭据管理器条目。
- 各平台共用 API 门禁（§8），浏览器启动和凭据库实现不同。当前工作区的功能（含 Mongo 增补）不代表旧发布二进制已经包含。

## 2 · 界面总览

布局分三块：

- **左侧栏**：顶部「Import snapshot」按钮；中间是快照列表（按环境名分组，可折叠，每条显示 appid、条目数、更新时间，hover 出删除按钮）；下方是 Tools 导航（AES encrypt/decrypt、Database tunnels、Settings）。
- **顶栏**：面包屑（当前快照）+ 语言切换开关（EN / 中文）。
- **四个视图**（点左侧导航切换）：

| 视图 | 干什么 |
|---|---|
| Snapshots（默认） | 浏览/搜索/编辑快照条目、导入、对比、导出 |
| AES encrypt/decrypt | 手动 key/iv 的 AES-GCM 加解密（Java CryptoUtil 兼容） |
| Database tunnels | 注册 PostgreSQL/MySQL/Redis/MongoDB 连接、管理隧道、生成带 token 的客户端链接 |
| Settings | 查看初始化状态并初始化快照/敏感值密钥，不显示密钥值 |

## 3 · 快照管理（Snapshots）

### 3.1 导入快照

点左侧「Import snapshot」（或主区 CTA 按钮），在对话框里：

1. **Environment**：环境名（如 `prod`）。
2. **App ID（必填）**：Apollo 应用 ID（如 `merdi-portal`）。环境/AppID 组合必须未被使用；对话框阻止重名，API 也以 HTTP 409 拒绝。UI 没有确认覆盖路径。
3. 粘贴 `KEY = value` 配置文本（多行）。
4. 点 **Preview**：检查解析出的 key 名、敏感标记及跳过/拆分行的警告，再点 **Import**。警告可能含原始输入，应把预览视为可能包含明文的内容。

快照 value 加密存到 `~/.vaulty/apollo/{env}__{appid}.json`（0600），名字/元数据可读，输入文本/剪贴板仍是明文。自动检测的分类会持久化，但新导入条目默认都是 `safe=false`。CLI 覆盖规则不同：需 TTY 确认或非 TTY `--force`，且整份替换而非合并。

### 3.2 浏览与搜索

- 点左侧栏快照进入；表格有 **Key**、**Value** 两列及行内操作，没有每条的指纹或更新时间列；侧栏/上下文显示快照时间。
- 顶部搜索框按 **key 或可见值** 过滤。
- **没有 `safe=true` 的条目都会掩码**，包括 `secret=false` 的条目。显式 safe 值不需 `--allow-plaintext` 就显示明文。此处 UI/API 的 `sensitive` 表示 `!safe`，不是选择加密密钥的存储分类 `secret`；快照摘要的敏感计数则使用 `secret`。
- 显示为 `chars` 的长度实际是 UTF-8 字节数。指纹在对比视图中提供，普通条目表格没有；见 [Apollo 指纹说明](apollo-snapshot-guide.zh-CN.md)。

### 3.3 修改 / 删除条目与新增 key

点表格里任意一行打开「Edit item」对话框：

- 显式 safe 值：显示当前明文，修改后 Save。
- 掩码值（`!safe`）：编辑器不加载原明文。输入新值即替换；**空串或纯空白保存会保持原值**。
- 点 **Delete** 删除该条目（需确认）。
- 保存时将 UI 掩码标记作为 `secret` 提交：替换掩码条目会写成 `secret=true, safe=false`，即使之前是 `secret=false`；保存可见条目会写成 `secret=false, safe=true`。此 API 不执行 CLI 的 `--plain` 启发式守卫。不要将可见 safe 值替换成秘密，否则普通 GET 仍可读取。
- 行编辑对话框的 key 是固定标签，没有新增/改名输入框。新增 key 需用整份明文编辑（§3.5）或 CLI `apollo set`；保留已有条目分类时使用不带分类 flag 的 CLI `set`。

### 3.4 环境对比（两两 / 跨环境 / 单 key）

- **Compare environments**：对比当前快照与另一个快照，列出 added / removed / changed，diff 结果可过滤、可复制。
- **Compare across environments**：勾选 2 个以上快照，横向对比每个 key 在各环境的取值；结果可 **Copy as table（Tab 分隔）**、**Copy CSV**，或生成 **Diff report**（含统计：总 key 数、差异数、敏感值差异数）。
- **单 key 对比**：使用行内 **Compare this key** 按钮，只查所选快照的同一 AppID（含空的旧版 AppID），不是所有 AppID。缺失 key 显示为 absent。

对比可能包含显式 safe 明文；其他条目保持掩码并提供字节长度和指纹。指纹是同一快照密钥下归一化值的截断 HMAC，不绝对证明原始字节相等。两两对比的变化值仅在两边都 safe 时显示明文；多快照/单 key 对比按每个条目自己的 safe 标记投影。

### 3.5 导出与明文编辑（需 `--allow-plaintext`）

- **Export config**：打开警告对话框，选择 **Copy to clipboard** 或 **Export**（浏览器下载）才请求整份明文 `KEY = value`。当前下载文件虽以 `.json` 命名，内容却是文本，不是加密快照 JSON。剪贴板历史和下载文件可能保留秘密。
- **Plaintext-edit all**：打开对话框就立即请求全文明文，没有额外的加载确认。保存会**替换整份快照**、重新检测 `secret` 并将 `safe` 重置为 false；省略/解析失败的条目可能消失。完全无法解析时拒绝保存，但部分解析警告目前会被丢弃。保存前检查全文，保存后复核 key、数量和分类；需保留已有标记时用 CLI 单条编辑。

两者都需 UI token 和 **`--allow-plaintext`**。请求字段 `confirm:true` 由 JavaScript 填入，不证明独立的人工确认或身份。不要把真实数据的明文操作交给 agent。

### 3.6 显示单值明文（Reveal，需 `--allow-plaintext`）

点 **Reveal**，再点对话框内的 **Show**。默认用配置的快照/敏感值密钥解开快照包装，显示存储的 value。如果 value 自身是外部 AES 密文，成功 reveal 显示的是该密文，不是外部明文。

手动 key/IV 字段**只在 reveal 请求失败后出现**，不是随时可展开的选项。同时填写两个覆盖值后重试，会先解开快照包装再解外部密文，不能修复缺失/错误的快照密钥。成功 reveal 后如需解外部密文，人工可在 AES 工具（§4）中使用原外部 key/IV。不要故意破坏密钥配置来显示高级字段。

## 4 · AES 加解密

左侧 Tools → **AES encrypt/decrypt**：

1. **AES key**：16/24/32 字节 UTF-8 字符串。
2. **IV**：非空 UTF-8 字节串。key/IV 长度按字节而非 Unicode 字符计数；UI 会去除两者首尾空白。
3. 输入框填明文或 base64 密文 → 点 **Encrypt** / **Decrypt** → 结果在下方，可 **Copy result**。

与 Java `CryptoUtil` 兼容（AES/GCM/NoPadding，Base64 密文含认证 tag）。**Decrypt 需 `--allow-plaintext`**，否则 403。Encrypt/Decrypt 直接发送受 token 门控的请求，没有二次确认弹窗；输入/结果可能含秘密。这里手动填写的外部 AES 密钥不是 Base64 编码的 32 字节快照/DB 密钥。

**绝不能用同一 AES-GCM key/IV 加密不同消息。** 同一 key 下，每次新加密使用新的唯一 IV；解密使用原配对。兼容 Java 不代表反复用固定 IV 加密是安全的。此 UI 不管理 CLI 持久化的 AES key/IV 列表；CLI `aes gen-key` 会打印生成的秘密，不属于掩码读取。

## 5 · 数据库隧道（Database tunnels）

### 5.1 初始化 DB 密钥

进入视图时检查 DB 密钥。非空 `VAULTY_KEEPER_DB_KEY` 优先于系统密钥库，必须是恰好 32 字节密钥的标准 Base64 编码。密钥不可用时显示 **Initialize database key**。生成前先检查是否因无效覆盖或密钥库不可用导致：无效覆盖不会回退，生成不会迁移已有数据或修复覆盖值。

### 5.2 注册连接

「New connection」卡片里填：

- **Name**：连接名（如 `mysql-orders`）。
- **Tunnel port（可选）**：留空从 15432 起自动分配，仅跳过此存储中已注册的端口，不探测 OS 占用；需要可复现客户端配置时填写明确可用的端口。
- **Database URL**：支持 `postgres://`、`mysql://`、`redis://`、`mongodb://` 等 scheme。例如 `postgres://demo:demo@localhost:5432/appdb` 是**合成语法示例**，不代表已准备数据库。

MongoDB 8 接受单固定端点和普通用户名/密码认证。遵循[注册 URL 白名单](mongodb-tunnel-guide.zh-CN.md#注册-url)，而非通用驱动的全部选项：注册 `retryWrites` 会被拒绝，但生成的客户端 URI 包含 `retryWrites=false`。后端账号需要 `listCollections` 权限来验证普通集合；不支持视图/时序和完整管理/GUI 自省。连接测试成功不代表所有业务集合权限均已验证。

**注册和测试输入 URL 时，在 POST 前加密 URL**：浏览器先从 `/api/db/pubkey` 获取服务端 ECDH 公钥，派生 AES-GCM 密钥并加密 URL；私钥只在 UI 进程内存中，每次启动重生。此范围不包括通过 loopback HTTP 返回解密明文的 **View URL**，也不覆盖全部后续数据库流量，不能当作通用 TLS 保证。

- **Test connection**：先用填的 URL 试连一下（不落库）。
- **Register connection**：加密落盘到 `~/.vaulty/db.json`（0600），并为该连接生成专属隧道 token。
- 同名注册会替换连接、生成新 token 并恢复默认开启。端口留空时保留原端口。需重新分发连接信息，必要时再次关闭隧道。

MySQL 隧道 `?tls=true` 会与后端协商 TLS，并用升级后的连接完成认证与转发（私有/自签 CA 加 `tlsCAFile=<路径>`）；该修复已有单测，一次性原生 TLS 查询通过但未被集成测试固化。直连 **Test connection** 会执行同样的后端 TLS 升级，但不是完整隧道测试。各协议限制见 [DB 示例](db-proxy-examples.zh-CN.md)。

### 5.3 启动隧道服务

注册后，能访问相同 DB 存储/密钥的宿主终端需要持续运行 `vaulty-keeper serve --addr 127.0.0.1:8970`。只有 **serve 启动时** DB 存储已存在且密钥可解析，才会启动 watcher。若首次注册前 serve 仅启动了 bridge，注册后需重启。已运行的 watcher 约每两秒同步新增/删除/开关。UI enabled/disabled 是持久化配置，不是监听健康或客户端可连接的证明。

需单独安装所选原生客户端。UI 的 **Connect info** 使用 `127.0.0.1`，没有容器地址切换控件。容器使用时，在宿主 CLI 执行 `vaulty-keeper db connect <name> --container` 生成链接，仅交付获准的隧道凭据。`--container` 只将打印的地址改为 `host.docker.internal`，不改变监听地址；serve 需使用容器可达的绑定及适当防火墙限制。容器内 `remote dblist` 仅返回元数据；本地 `db connect` 仍需本地 DB 存储/密钥。不要为打通流程而挂载宿主密钥。见 [DB 示例](db-proxy-examples.zh-CN.md)。

### 5.4 连接列表与操作

表格列出 Name / Type / State / Port / Actions：

| 操作 | 作用 |
|---|---|
| **Test** | 用解密后的真实 URL **直连**数据库测试（不走隧道），验证注册连接可用 |
| **Connect info** | 按协议提供原始隧道链接及客户端选项：psql/libpq、DBeaver/DataGrip JDBC、pgAdmin4 字段、Redis Insight、redis-cli 或 mongosh；隧道 token 已填好 |
| **Regenerate** | 为新连接轮换此连接的专属 token（需确认），需重新分发链接；不撤销既有会话或全局 token；「Regenerate all」轮换全部专属 token |
| **开启隧道 / 关闭隧道** | 将目标状态存入 db.json；已运行的 serve watcher 在下次同步（通常约 2 秒）关闭/恢复监听，不强制终止已有会话 |
| **View URL** | 直接请求并显示解密的后端 URL，无二次确认弹窗；仅在 `--allow-plaintext` 时显示，且需要 UI token |
| **删除** | 删除连接（需确认）；已运行的 watcher 移除监听，不承诺终止既有会话 |

> **Broken** 表示存储的 URL 无法解密（如旧密钥或密文损坏），该行只提供删除。token 解密/Resolve 失败是另一类错误，可能没有 Broken 标记。先检查密钥来源；同名重注册对 token/开启状态的影响见前文。

PostgreSQL/MySQL/Redis 的新旧注册连接都接受当前全局 bridge token **或**连接专属 token。轮换专属 token 不会撤销全局 token 访问权。Mongo 的差异见下文。隧道链接隐藏后端认证凭据，但 token 赋予数据库访问权，查询结果也可能含秘密。

Mongo **Connect info** 使用用户 `vaulty`、专属 token 作为 SCRAM-SHA-256 密码、`authSource=admin`、`directConnection=true` 和 `retryWrites=false`，无全局 bridge token 兜底。这些仅含代理信息的链接供获授权 agent/工具使用，与 UI 访问 token 或 **View URL** 输出不同。生成的 mongosh 命令把隧道 token 放入 argv，不含真实后端 URI；应作为访问凭据保护，人工客户端也可使用密码提示输入。关闭监听不承诺终止既有会话。明文网络限制、业务数据不改写及已验证/待验证矩阵见 [Mongo 指南](mongodb-tunnel-guide.zh-CN.md)。

**证据范围：** 2026-09-07 实现记录报告，在基于 `7273bb21e8347777058a17fb95a16aa2a17a36dc` 的未提交 Mongo 工作树上（macOS 12.4 Intel），test/race/vet/build 及 MongoDB 8.0.13 standalone/固定副本集 Go 驱动/mongosh 检查已通过。这是历史证据，不是本次文档编辑后的重跑或人工浏览器验证。实际 Mongo TLS、人工 TTY `db shell` 和不可用的独立复审仍未验证；详细矩阵由 [Mongo 指南](mongodb-tunnel-guide.zh-CN.md) 维护。

## 6 · 设置（Settings）

查看初始化状态并初始化两把密钥，不显示密钥值：

- **Snapshot key**：快照密钥（加密非敏感值）。
- **Sensitive-value key**：敏感值密钥（加密敏感值）。

不可用时显示对应 **Generate** 按钮，已初始化显示状态；对应 `apollo init` / `sensitive init` 的同一把密钥。与 DB 密钥一样，非空环境覆盖优先于密钥库，需从 Base64 解码为 32 字节。错误状态不证明原密钥不存在，生成前先检查来源。新格式密钥隔离和旧格式快照密钥回退见 [Apollo 指南](apollo-snapshot-guide.zh-CN.md)。

## 7 · 语言切换与 CLI 同步

顶栏开关在 **English / 中文** 间切换。该浏览器的 `localStorage` 优先，只有没有本地选择时才尝试读取 `/api/prefs`。切换还会尝试通过 token 门控写入 `~/.vaulty/prefs.json`，但失败被静默忽略。已有浏览器选择不会持续跟随 CLI 变化。

CLI 优先级是 `VAULTY_KEEPER_LANG` → 共享 prefs → `en`。使用 `vaulty-keeper lang en` 或 `vaulty-keeper lang zh`（不是字面输入 `en|zh`）。UI 的 prefs 响应在每次请求时按服务端进程环境和当前共享文件解析；更改服务端启动时的环境覆盖需重启该进程。部分 flag 描述、提示和底层错误仍为英文。因此浏览器选择、服务端进程及有 env 覆盖的 CLI 可能不一致。

## 8 · API 与明文边界

- **Loopback 与 UI token**：只监听 127.0.0.1；非 GET API 请求需要每次启动生成的 token（`?t=` 或 `X-Auth-Token`）。token 失败延迟每次线性增加 50 ms，上限 2 秒，不是指数退避。
- **GET 不是仅掩码，也不受 token 门控**：快照查看/对比可能返回显式 safe 明文；`GET /api/db/connect?name=...` 不需要 UI token 或 `--allow-plaintext`，就可能返回可用 DB token 和填好 token 的链接。后端密码保密不代表这些访问凭据无害。
- **专门的明文接口**：reveal/export/edit/AES decrypt/真实 URL 查看需要 UI token 和 `--allow-plaintext`。确认流程各不相同（§3-5）；`confirm:true` 不是真人认证。CLI 的 stdin TTY 检查也不验证真人身份。
- **Origin 与缓存**：拒绝 `Origin: null` 及 host 与服务 host 不同的 Origin，允许没有 Origin 头的客户端。`Cache-Control: no-store` 提示不要缓存，但不能擦除下载、剪贴板历史、截图、日志或浏览器内存。

不要分享带 UI token 的 URL，也不要把明文操作交给 agent。只向获授权使用方交付 DB token。密钥保管、明文落盘例外（含 AES key/IV JSON）、同用户访问与容器限制统一由[安全模型](security-model.zh-CN.md)维护。

## 9 · 常见问题

| 现象 | 处理 |
|---|---|
| 改了 static 文件不生效 | 运行 `make test` 后 `make build`（go:embed 打进二进制） |
| 明文按钮点了没反应 / 403 | 检查当前 token 并以 `--allow-plaintext` 重启 UI；遵循各操作实际流程，不假定都有二次确认 |
| 语言和 CLI 不一致 | 检查 CLI env 覆盖、浏览器本地偏好及 token 门控的 prefs 写入是否成功（§7） |
| 想重新拿 token / 忘记 URL | 重启 `vaulty-keeper ui`，每次启动 token 都重新随机 |
| 连接显示 Broken | 先检查密钥来源/密文；同名重注册会替换 token 并恢复开启，仅未指定新端口时保留原端口 |
| UI 端口被占用 | 显式 `--port` 也是起始端口，以实际打印 URL 为准。DB 端口分配是另一套逻辑，不探测 OS 可用性 |
| DB 已开启却无法连接 | 检查 serve 启动时存储/密钥是否可用、绑定/防火墙及客户端安装；直连测试不能证明监听健康（§5.3） |

---

*开发提示：`internal/ui/static/`（index.html / app.js / app.css）是前端，`internal/ui/ui.go`、`internal/ui/db.go` 是 API 与门禁。*

# vaulty-keeper Web UI · 功能与使用指南

> 中文 | [English](ui-guide.md)
>
> 本地 Web UI（`vaulty-keeper ui`）覆盖全部快照、AES 加解密与数据库隧道功能。本文按页面逐个讲解：有哪些功能、怎么用、安全边界在哪。
> 配套：`README.zh-CN.md`（命令全表）、`docs/apollo-snapshot-guide.zh-CN.md`（快照实现）、`docs/db-proxy-architecture.zh-CN.md`（数据库隧道图解）。

---

## 1 · 启动与访问

```sh
vaulty-keeper ui                    # 默认端口，被占用时自动顺延到下一个空闲端口
vaulty-keeper ui --port 8123        # 指定端口
vaulty-keeper ui --allow-plaintext  # 额外开启明文接口（导出 / 解密 / 明文编辑 / 查看真实 URL）
```

- 启动后打印 `http://127.0.0.1:<port>/?t=<token>`，浏览器打开即可（会自动拉起默认浏览器）。
- **只监听 127.0.0.1**；token 每次启动随机生成（128 位）。**不要把带 token 的 URL 发给 AI/脚本，也不要贴进日志或 shell history**。
- **明文接口默认禁用**（顶部会有黄色横幅提示），需要 `--allow-plaintext` 重启生效；未开启时导出/解密/明文编辑/显示真实 URL 即使带 token 也会 403。
- UI 的静态资源用 `go:embed` 打进二进制：改过 `internal/ui/static/` 下的文件后必须 `make build` 重建才生效。

### Windows 启动方式

Windows 发布包是 `vaulty-keeper-windows-x86_64.zip`，解压后得到 `vaulty-keeper.exe`，用法与其它平台完全一致，只是文件名带 `.exe`：

```powershell
# PowerShell / CMD，进入解压目录后：
.\vaulty-keeper.exe ui                   # 默认端口，被占用时自动顺延
.\vaulty-keeper.exe ui --port 8123       # 指定端口
.\vaulty-keeper.exe ui --allow-plaintext # 额外开启明文接口
```

- 启动同样打印 `http://127.0.0.1:<port>/?t=<token>`，并自动用默认浏览器打开。
- **PowerShell 里运行当前目录下的程序要加 `.\` 前缀**（直接敲 `vaulty-keeper.exe ui` 会提示找不到命令）；CMD 不需要。
- 密钥存 **Windows 凭据管理器**（Credential Manager）；数据目录在 `%USERPROFILE%\.vaulty\`（快照在 `.vaulty\apollo\`、DB 连接在 `.vaulty\db.json`）。
- 其余行为与安全模型与其它平台完全一致（见第 8 节）。

## 2 · 界面总览

布局分三块：

- **左侧栏**：顶部「Import snapshot」按钮；中间是快照列表（按环境名分组，可折叠，每条显示 appid、条目数、更新时间，hover 出删除按钮）；下方是 Tools 导航（AES encrypt/decrypt、Database tunnels、Settings）。
- **顶栏**：面包屑（当前快照）+ 语言切换开关（EN / 中文）。
- **四个视图**（点左侧导航切换）：

| 视图 | 干什么 |
|---|---|
| Snapshots（默认） | 浏览/搜索/编辑快照条目、导入、对比、导出 |
| AES encrypt/decrypt | 手动 key/iv 的 AES-GCM 加解密（Java CryptoUtil 兼容） |
| Database tunnels | 注册数据库连接、管理隧道、生成带 token 的客户端链接 |
| Settings | 查看/初始化快照密钥与敏感值密钥 |

## 3 · 快照管理（Snapshots）

### 3.1 导入快照

点左侧「Import snapshot」（或主区 CTA 按钮），在对话框里：

1. **Environment**：环境名（如 `prod`）。
2. **App ID（必填）**：Apollo 应用 ID（如 `merdi-portal`）。重名快照会在输入时提示，覆盖需二次确认。
3. 粘贴 `KEY = value` 配置文本（多行）。
4. 点 **Preview**：先做解析预览（识别出多少个条目、敏感标记等），确认无误再点 **Import**。

导入后值全部加密落盘到 `~/.vaulty/apollo/{env}__{appid}.json`（0600），敏感值自动识别为 secret。

### 3.2 浏览与搜索

- 点左侧栏的快照进入；主区显示该快照的全部条目（key、掩码值、指纹/更新时间等）。
- 顶部搜索框按 **key 或可见值** 过滤。
- **敏感值默认显示掩码** `*** (n chars)`；判断一致性靠长度+指纹，不要靠猜。

### 3.3 新增 / 修改 / 删除条目

点表格里任意一行打开「Edit item」对话框：

- 普通值：显示当前明文，直接改后 Save。
- **敏感值：明文不可见**（有警告提示）。输入新值即替换；**留空保存 = 保持不变**。
- 点 **Delete** 删除该条目（需确认）。

### 3.4 环境对比（两两 / 跨环境 / 单 key）

- **Compare environments**：对比当前快照与另一个快照，列出 added / removed / changed，diff 结果可过滤、可复制。
- **Compare across environments**：勾选 2 个以上快照，横向对比每个 key 在各环境的取值；结果可 **Copy as table（Tab 分隔）**、**Copy CSV**，或生成 **Diff report**（含统计：总 key 数、差异数、敏感值差异数）。
- **单 key 对比**：在对比/表格界面点某个 key，可看该 key 在所有快照里的取值对比（掩码+指纹）。

全程不出现明文，敏感值始终掩码。

### 3.5 导出与明文编辑（需 `--allow-plaintext`）

- **Export config**：生成整份明文 `KEY = value`，可 **Copy to clipboard** 或 **Export**（浏览器下载）。导出前有警告：含敏感值，只在本机查看、不要转发。
- **Plaintext-edit all**：在编辑器里直接改整份配置，保存后整份重新加密写回。

两个都是明文出口：**默认禁用，需 `--allow-plaintext`**，且各自有二次确认。

### 3.6 显示单值明文（Reveal，需 `--allow-plaintext`）

点条目的「显示」后确认，把某个 key 的明文显示在对话框里。若值不是用系统敏感密钥加密的（比如外部 AES 密文），可展开高级选项手动填 AES key / IV 再试。

## 4 · AES 加解密

左侧 Tools → **AES encrypt/decrypt**：

1. **AES key**：16/24/32 字节 UTF-8 字符串。
2. **IV**：UTF-8 字节串。
3. 输入框填明文或 base64 密文 → 点 **Encrypt** / **Decrypt** → 结果在下方，可 **Copy result**。

与 Java `CryptoUtil` 兼容（AES/GCM/NoPadding）。**Decrypt 属于明文出口，需 `--allow-plaintext`**，否则会 403。

## 5 · 数据库隧道（Database tunnels）

### 5.1 初始化 DB 密钥

进入视图时检查 DB 密钥（`VAULTY_KEEPER_DB_KEY` / 系统密钥库）。未初始化时显示 **Initialize database key** 按钮，点一下即生成。

### 5.2 注册连接

「New connection」卡片里填：

- **Name**：连接名（如 `mysql-orders`）。
- **Tunnel port（可选）**：留空自动分配（基准 15432 递增，冲突顺延）。
- **Database URL**：`postgres://user:pass@host:5432/dbname`、`mysql://…`、`redis://:pass@…`。

**URL 不会以明文出现在网络上**：浏览器先从 `/api/db/pubkey` 拿服务端 ECDH 公钥，在浏览器内派生 AES-GCM 密钥加密 URL 后才 POST；对应的私钥只在 UI 进程内存里，每次启动重新生成。

- **Test connection**：先用填的 URL 试连一下（不落库）。
- **Register connection**：加密落盘到 `~/.vaulty/db.json`（0600），并为该连接生成专属隧道 token。

### 5.3 连接列表与操作

表格列出 Name / Type / State / Port / Actions：

| 操作 | 作用 |
|---|---|
| **Test** | 用解密后的真实 URL **直连**数据库测试（不走隧道），验证注册连接可用 |
| **Connect info** | 弹窗给出该连接的全部现成客户端链接：原始隧道链接、psql/libpq、DBeaver/DataGrip JDBC、pgAdmin4 字段、Redis Insight、redis-cli——token 已填好，直接复制执行 |
| **Regenerate** | 轮换该连接的隧道 token，**旧链接立即失效**（需确认）；顶部「Regenerate all」一次轮换全部 |
| **开启隧道 / 关闭隧道** | 按连接开关隧道（端口停止/恢复监听，`serve` 约 2 秒内生效），状态持久化到 db.json |
| **View URL** | 显示解密后的真实数据库 URL——**只在你浏览器里可见**；此按钮默认隐藏，需 `--allow-plaintext` 才出现 |
| **删除** | 删除连接，隧道随之消失（需确认） |

> 连接能解密但 token 校验失败等异常会标 **Broken**，此时只提供删除。AI 永远拿不到真实 URL/凭据；隧道用法的细节见 `docs/db-proxy-architecture.zh-CN.md` 与 `docs/db-proxy-examples.zh-CN.md`。

## 6 · 设置（Settings）

查看并初始化两把密钥的状态：

- **Snapshot key**：快照密钥（加密非敏感值）。
- **Sensitive-value key**：敏感值密钥（加密敏感值）。

未初始化时显示对应「Generate」按钮；已初始化显示状态。与 `apollo init` / `sensitive init` 是同一把密钥。

## 7 · 语言切换与 CLI 同步

顶栏开关在 **English / 中文** 间切换：按浏览器记在 `localStorage`，并同步写入共享的 `~/.vaulty/prefs.json`——CLI 输出与 UI 同语言。首次访问还会自动沿用 CLI 设置的共享语言；命令行用 `vaulty-keeper lang en|zh` 切换。

## 8 · 安全模型（为什么可以放心用）

- **只监听 127.0.0.1**；token 每次启动随机，写操作（导入/增删改/导出/解密/明文编辑/删除）全部要 token（URL `?t=` 或 `X-Auth-Token` 头），失败会指数退避限速。
- **GET 只返回掩码数据**：列表/查看/对比都是掩码+长度+指纹，不带明文。
- **明文接口默认禁用**：导出、解密、明文编辑、显示真实 URL 需 `--allow-plaintext` 才开，且各自有二次确认。
- **CSRF 防护**：跨源请求被拒；响应全部 `no-store`，浏览器不落缓存。
- **DB URL 传输加密**：注册时浏览器内 ECDH+AES-GCM 加密后才发送。

一句话：**没有 token 的写操作一律拒绝、明文出口默认关死、AI/脚本即使拿到 GET 接口也只有掩码**。不要把带 token 的 URL 发给 AI 或脚本。

## 9 · 常见问题

| 现象 | 处理 |
|---|---|
| 改了 static 文件不生效 | 需要 `make build` 重建（go:embed 打进二进制） |
| 明文按钮点了没反应 / 403 | UI 需要 `--allow-plaintext` 重启；每个明文操作还有二次确认 |
| 语言和 CLI 不一致 | 顶栏切换，或 `vaulty-keeper lang en|zh` |
| 想重新拿 token / 忘记 URL | 重启 `vaulty-keeper ui`，每次启动 token 都重新随机 |
| 连接显示 Broken | 一般是密钥对不上（`db.json` 里存了 key_id），在 CLI 用 `db add` 同名重注册 |
| 端口被占用 | 自动顺延到下一个空闲端口，或显式 `--port` |

---

*开发提示：`internal/ui/static/`（index.html / app.js / app.css）是前端，`internal/ui/ui.go`、`internal/ui/db.go` 是 API 与门禁。*

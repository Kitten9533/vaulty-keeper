# 为 vaulty-keeper 贡献

> 中文 | [English](CONTRIBUTING.md)

感谢考虑贡献。本文件说明如何构建、测试和提交改动。[文档索引](docs/README.md) 映射了文档结构；[安全模型](docs/security-model.md) 是安全边界的统一现行依据。安全问题请通过 [SECURITY.md](SECURITY.md) 报告，不要发公开 issue。

## 仓库结构

```
internal/aesx    AES-256-GCM 加解密（Java CryptoUtil 兼容）
internal/apollo  快照存储、双密钥加密、掩码、指纹
internal/app     应用编排
internal/bridge  serve/remote 共用的掩码代理
internal/cli     命令树与 TTY 门禁
internal/dbproxy 数据库隧道（PG/MySQL/Redis/MongoDB）
internal/i18n    en/zh 的 UI 与 CLI 文案
internal/ui      仅回环的 Web UI（静态资源 go:embed）
scripts/         前端/文档检查与隔离 DB 测试脚本
tools/javaref    AES 互操作向量的 Java 参考实现
```

## 环境要求

- Go 1.26+ 与 Make。
- Node.js 仅用于 `make test` 中的静态检查（`scripts/check-ui.mjs`、`scripts/check-docs.mjs`），构建不需要。
- DB 隧道测试脚本使用 Docker（`scripts/dbtest.sh`、`scripts/mongotest.sh`）；已隔离，不碰真实 `~/.vaulty` 或 keyring。

## 构建与测试

```sh
make build          # → bin/vaulty-keeper
make test           # 前端 + 文档静态检查，然后 go test ./...
make install        # 符号链接到 ~/.local/bin/vaulty-keeper
```

提交涉及并发、终端、前端或文档的改动前，跑完整检查：

```sh
go test -race ./...
go vet ./...
go vet -tags=mongointegration ./internal/dbproxy
bash -n scripts/dbtest.sh scripts/mongotest.sh
node scripts/check-ui.mjs
node scripts/check-docs.mjs
```

`git diff --check` 必须保持干净。

## 测试覆盖与 DB 夹具

以下是覆盖范围指针，不代表本次文档更新执行过测试；代码变化后须针对确切源码版本运行相关检查。MongoDB 带日期证据与剩余缺口统一维护在 [MongoDB 指南](docs/tunnel/mongodb-tunnel-guide.zh-CN.md) 中。

- `internal/aesx`: 与 `tools/javaref/CryptoUtil.java`（Java 8 参考实现）生成的向量逐字节对齐（GCM 确定性），另覆盖 key 长度校验、错误 key/iv、非法 base64。
- `internal/apollo`: 真实粘贴样例（含合并行）、注释、首个 `=`、URL 参数不误拆、快照加密落盘（文件无明文、权限 0600）、diff、敏感识别。
- `internal/cli`: 参数混排、import 自动取名、reveal（敏感值明文 / 显式 `--key`/`--iv` 解密外部密文 / 多 key JSON）、edit（假编辑器脚本）、list/compare JSON、gen-key 可用性、aes `--name` 列表、completion。
- 重新生成 Java 向量：`cd tools/javaref && javac CryptoUtil.java && java CryptoUtil encrypt <key> <iv> <plaintext>`

隔离 DB 夹具脚本（需要 Docker、Python 3 和已构建二进制；只用合成凭据与显式临时存储/test key，绝不碰真实 `~/.vaulty` 或 keyring）：

- **MongoDB**：入口为 `bash scripts/mongotest.sh --mongosh` 和 `bash scripts/mongotest.sh --replica-set --mongosh`。
- **`scripts/dbtest.sh` 已隔离重构，可以安全运行（C02 完成）。** 当前脚本按 PID 与容器标签跟踪自己启动的 serve 和容器，使用每次运行独立的临时目录与假 HOME、合成密钥，`--clean` 只清理它登记的资源——不再像历史版本那样宽泛 pkill serve 进程、删除固定容器（`aipg`、`aimysql8`、`aimariadb`、`airedis`）或覆盖真实 `~/.vaulty/bridge-token`。使用前先读脚本头注释；不要放进 CI。

历史脚本接口，**不是快速开始推荐**：

```sh
make build
./scripts/dbtest.sh          # 启动并测试；测完环境保持运行，打印连接方式
./scripts/dbtest.sh --clean  # 收尾：停 serve、删容器
```

实际夹具镜像为 PostgreSQL `postgres:17.6-alpine`、MySQL `dockerproxy.net/library/mysql:8.0`（历史本地镜像为 8.0.46，不是 8.4/MariaDB）及 Redis `redis:7`。后端、隧道与桥接端口每次运行动态分配（可用 `PGP`/`TUN_PG` 等环境变量覆盖），以脚本打印的连接方式为准；已准备数据及查询如下：

| 注册名 | 已准备数据 / 查询 |
|---|---|
| `pgdb` | `appdb.t`，`SELECT id,name FROM t ORDER BY id;` |
| `mysqltest` / `mysqlnative` | `shop.customers`、`products`、`orders`；`SELECT COUNT(*) FROM shop.orders;` |
| `cache` | 需认证的 Redis；`PING`、合成 `SET`/`GET` |

脚本使用独立 DB 目录/密钥，不是宿主默认 `db shell` 上下文。其中命令/日志是特定夹具的历史示例，不证明所有客户端/配置可用。原生客户端准备及正/负向查询见 [DB 示例指南](docs/db-proxy-examples.zh-CN.md)；分享日志前先检查，其中可能含上游元数据和访问 token。

## 代码约定

- 遵循现有包结构与命名；改动保持最小并修根因，不做无关重构。
- 修改 `internal/ui/static/*`（HTML/CSS/JS）后**必须**先 `make test` 再 `make build`：前端用 `go:embed` 打进二进制，`make test` 会在 Go 测试前跑 JS 语法 / DOM id / 变量遮蔽 / i18n key 检查。
- 前端代码不要遮蔽全局 i18n 函数 `t()`；会导致整页渲染中断，静态检查也会拦截。
- 行为改动要新增或更新 Go 测试；涉及并发或终端的改动加跑 `go test -race ./...`。
- 绝不把秘密带进代码、测试、文档或提交。测试只使用合成值与隔离临时存储，不用真实 `~/.vaulty`、keyring 条目或真实 DSN。

## 文档约定

- 面向用户的文档**英文默认 + `.zh-CN.md` 配对**，每份文件顶部有语言切换链接，两种语言内容一致。`AGENTS.md` 是唯一例外（agent 操作约束，仅中文）。
- 每个主题只有一个现行位置：安全模型只放 `docs/security-model.md`；指南链接它而不是复制。新增、移动或归档指南时更新 `docs/README.md`。`make test` 会跑 `scripts/check-docs.mjs`，还要求索引列出每份 `docs/**/*.md` 指南、Makefile 使用 `cp -R docs`（不压扁 `docs/tunnel/`），以及反引号中的 `docs/` / `scripts/` 路径真实存在。
- 历史实现记录（计划/设计）用 git tag 归档（如 `docs-superpowers-archive`），不提交进树；不要把归档记录改写成现行指令，也不要对其中的命令按真实数据重放。
- 行为变化要在同一次改动中同步更新对应文档，让文档描述当前源码而不是旧承诺。

## 发布流程（maintainer）

发布只由 maintainer 执行：

1. 在 `internal/cli/cli.go` 中提升 `Version`。
2. `make release` — 交叉编译归档到 `release/` 并生成 `release/sha256sums.txt`。它会**删除并重建 `release/`**，不要随便运行。
3. 推送 `v*` tag；CI 跑测试并发布 release 资产（tag release 已存在时幂等）。
4. npm 渠道（`npm/`）用同一批 release 归档经 `npm/scripts/build.mjs` 构建，由 maintainer 向官方 registry 发布。

具体门禁见 `.github/workflows/ci.yml`。

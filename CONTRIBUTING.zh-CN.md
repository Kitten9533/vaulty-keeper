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

## 代码约定

- 遵循现有包结构与命名；改动保持最小并修根因，不做无关重构。
- 修改 `internal/ui/static/*`（HTML/CSS/JS）后**必须**先 `make test` 再 `make build`：前端用 `go:embed` 打进二进制，`make test` 会在 Go 测试前跑 JS 语法 / DOM id / 变量遮蔽 / i18n key 检查。
- 前端代码不要遮蔽全局 i18n 函数 `t()`；会导致整页渲染中断，静态检查也会拦截。
- 行为改动要新增或更新 Go 测试；涉及并发或终端的改动加跑 `go test -race ./...`。
- 绝不把秘密带进代码、测试、文档或提交。测试只使用合成值与隔离临时存储，不用真实 `~/.vaulty`、keyring 条目或真实 DSN。

## 文档约定

- 面向用户的文档**英文默认 + `.zh-CN.md` 配对**，每份文件顶部有语言切换链接，两种语言内容一致。`AGENTS.md` 是唯一例外（agent 操作约束，仅中文）。
- 每个主题只有一个现行位置：安全模型只放 `docs/security-model.md`；指南链接它而不是复制。新增、移动或归档指南时更新 `docs/README.md`。
- 历史实现记录（计划/设计）用 git tag 归档（如 `docs-superpowers-archive`），不提交进树；不要把归档记录改写成现行指令，也不要对其中的命令按真实数据重放。
- 行为变化要在同一次改动中同步更新对应文档，让文档描述当前源码而不是旧承诺。

## 发布流程（maintainer）

发布只由 maintainer 执行：

1. 在 `internal/cli/cli.go` 中提升 `Version`。
2. `make release` — 交叉编译归档到 `release/` 并生成 `release/sha256sums.txt`。它会**删除并重建 `release/`**，不要随便运行。
3. 推送 `v*` tag；CI 跑测试并发布 release 资产（tag release 已存在时幂等）。
4. npm 渠道（`npm/`）用同一批 release 归档经 `npm/scripts/build.mjs` 构建，由 maintainer 向官方 registry 发布。

具体门禁见 `.github/workflows/ci.yml`。

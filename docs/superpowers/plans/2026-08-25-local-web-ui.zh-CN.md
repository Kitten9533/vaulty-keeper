# 本地 Web UI 实施计划

[English](2026-08-25-local-web-ui.md) | 中文

> **历史计划来源，2026-08-25；生命周期标注于 2026-09-07。不可执行或重放。** 保留原任务、代码、预期结果及收尾说明，不认定为当前契约或全部完成。原先要求调用 `subagent-driven-development` / `executing-plans` 的指令只属于当时会话，现已失效。现行操作与安全见 [UI 指南](../../ui-guide.zh-CN.md)、[Apollo 指南](../../apollo-snapshot-guide.zh-CN.md)。设计来源为[初版 UI](../specs/2026-08-25-claude-style-web-ui-design.zh-CN.md)；后继为[全量迁移](2026-08-25-full-ui-migration.zh-CN.md)及 [Env+AppID](../specs/2026-08-25-env-appid-and-snapshot-delete.zh-CN.md)。
>
> **保留职责：** 英文配对文件完整保存两种语言共享的历史代码、命令、测试、预期输出和嵌套 README 示例；本文翻译叙述并逐任务链接源码，不复制数百行代码。维护此对文档时必须保留这些来源和链接，不能用本文替换或删除英文长计划。代码内原中文字符串有意保留。复选框与 `Expected: PASS` 都是旧计划，不是现在的验证结果。可选 AppID、不提供 reveal、仅依敏感分类掩码等假设已过时；不得拿真实存储/密钥执行其人工明文检查。

**目标：** 新增 `vaulty-keeper ui`，提供仅 loopback 的 Claude 浅色本地界面，管理加密 Apollo 快照；普通浏览和对比不暴露敏感明文。

**架构：** 新建 `internal/ui`，暴露可测试 JSON handler 与阻塞式 loopback 服务启动器。将现有 Apollo 存储 API 适配为安全视图；HTML/CSS/JavaScript 嵌入 Go 二进制，只请求同源 API。CLI 仅解析 ui flags 并传入快照目录。

**技术栈：** Go 标准库 `net/http`、`net`、`embed`、`httptest`、`os/exec`，现有 Apollo 包及静态 HTML/CSS/原生 JavaScript。

## 文件结构

对应[完整文件表](2026-08-25-local-web-ui.md#file-structure)：

- 修改 `internal/apollo/store.go`：共享快照名校验、安全解密视图；对应测试防止敏感明文泄漏。
- 修改 `internal/apollo/parser.go`：导出现有 key 规则，供 API、CLI、导入复用；测试合法/非法 key。
- 新建 `internal/ui/ui.go`：配置、启动、loopback 监听、JSON helper、路由。
- 新建 `ui_test.go`：注入密钥、临时快照目录、httptest。
- 新建 `browser_darwin.go` / `browser_linux.go` / `browser_other.go`：分别使用 open、xdg-open、无动作返回。
- 新建 `static/index.html`：v6 语义骨架、对话框及可访问操作控件。
- 新建 `static/app.css`：已确认的 v6 Claude 浅色视觉及响应式布局。
- 新建 `static/app.js`：同源请求、列表/导入/浏览/编辑/删除/对比/确认导出及刷新即清空的最近工作。
- 修改 CLI 与测试：注册 ui、解析 `--dir` / `--port` / `--no-open`，验证参数但不打开浏览器。
- 修改 README：本地绑定、启动、首版范围及敏感值保证。

## API 契约

[历史路由与 JSON 错误样例](2026-08-25-local-web-ui.md#api-contract)完整保留。所有 `/api/*` 响应拟包含 `Cache-Control: no-store`。

- GET snapshots：名称、App ID、采集时间、总数、敏感数，不解密值。
- GET snapshots/{name}：安全条目；普通项有 value，敏感项为 `value:null` + length，不回明文/密文。
- POST import/preview：接收 `{text}`，返回 key、推断的敏感标记与警告，不写文件。
- POST snapshots：接收 `{name, app_id, text}`；拒绝非法名称、空解析、重名冲突，只创建加密快照。
- PUT snapshots/{name}/items/{key}：接收 `{value, secret:true|false|null}`，返回安全条目；敏感项不得预填。
- DELETE 对应 item：删除一项，返回 204。
- GET compare?from=&to=：安全对象表示新增/删除/变化；任一侧敏感时两侧只回存在性和长度。
- POST snapshots/{name}/export：要求 `{confirm:true}`，否则 400；浏览器确认后才请求 no-store 明文附件。
- 错误信封为 `{error:{code,message}}`，原样例使用 `invalid_snapshot_name`，说明名称应以字母/数字开头，只含字母、数字、点、横线、下划线。

## 任务 1：共享快照安全基础

[共享历史代码、测试与命令](2026-08-25-local-web-ui.md#task-1-add-shared-snapshot-safety-primitives)。文件：Apollo store/parser 及对应测试。

- [ ] **步骤 1：先写失败测试。** store_test 加 `TestValidateSnapshotName`、`TestSnapshotVisibleItemsMaskSensitiveValues`；parser_test 加 `TestValidateKey`。覆盖普通名与空值/路径穿越/空格，普通值可见而敏感值只有长度。完整输入和断言保留在源码块。
- [ ] **步骤 2：验证失败。** 原定向命令选中名称及视图测试，预期因 ValidateSnapshotName、VisibleItems、ValidateKey 未实现而编译失败。
- [ ] **步骤 3：最小实现。** store 加严格正则 `^[A-Za-z0-9][A-Za-z0-9_.-]*$`、`VisibleItem{Key,Sensitive,Value,Length}` 和 VisibleItems。只在 Apollo 内解密，视图不带密文；敏感项给长度，其他项给值。导出 ValidateKey，不改变解析器行为；parseOne 改调用它，保持两条路径一致。
- [ ] **步骤 4：Apollo 测试。** 原 `go test ./internal/apollo -v` 预期 PASS，不是执行记录。
- [ ] **步骤 5：CLI 路径校验。** snapPath 返回 `(string,error)`，join 前调用 ValidateSnapshotName；每个命令入口将错误转为对应 fail。import/list/set/unset/compare/reveal/edit/export 同步，避免 UI/CLI 名称规则不一致。
- [ ] **步骤 6：CLI 路径穿越回归。** 加 `TestApolloRejectsUnsafeSnapshotName`，合成 Base64 密钥与临时目录，`../outside` 应返回 1。
- [ ] **步骤 7：联合定向测试。** 同时选择名称、视图、key、路径穿越四种测试，预期 PASS；原选择器保留在链接代码中。

## 任务 2：可测试 Loopback 服务与安全读取

[共享历史代码、测试与命令](2026-08-25-local-web-ui.md#task-2-build-the-testable-loopback-ui-server-and-safe-read-apis)。文件：新 ui.go、ui_test.go。

- [ ] **步骤 1：handler 失败测试。** 使用加密临时 prod 与注入固定密钥，测试 `TestSnapshotViewMasksSensitiveValue`、`TestCompareMasksSensitiveValues`、`TestHandlerRejectsUnsafeSnapshotName`。helper 通过 NewSnapshot/Save 写入，NewHandler 注入目录和密钥函数。验证 200、no-store、禁止敏感明文，非法名 400。
- [ ] **步骤 2：确认失败。** 原定向选择器预期因 internal/ui 尚不存在而构建失败。
- [ ] **步骤 3：配置、handler、JSON。** Config 包含 Dir、SnapshotKey；提供 NewHandler 与 Start。nil 密钥函数默认 Apollo；私有 ServeMux，所有 API 写 JSON 前加 no-store。共享错误 writer 设置 JSON 类型和 error 信封。列表不解密；快照视图使用 VisibleItems，key 排序转数组。对比在服务端解密后投影为 `SafeValue{Present,Sensitive,Value,Length}` 与 `SafeChange{Key,Kind,Old,New}`；新增/删除缺侧标不存在，敏感变更两侧只保留长度。
- [ ] **步骤 4：仅 Loopback 启动。** 限制端口 0..65535，用 net.JoinHostPort 只绑定 127.0.0.1，输出实际 URL；监听建立后按 openBrowser 调 openURL。HTTP 服务在 ctx.Done 时关闭，不增加 0.0.0.0 选项。
- [ ] **步骤 5：系统浏览器启动。** macOS open，Linux xdg-open，其他平台以 `!darwin && !linux` 构建标签空操作返回。
- [ ] **步骤 6：UI 包测试。** 原命令预期 PASS。

## 任务 3：导入、条目修改与确认导出

[共享历史代码、测试与命令](2026-08-25-local-web-ui.md#task-3-add-import-item-mutation-and-confirmed-export-apis)。文件：ui.go、ui_test.go。

- [ ] **步骤 1：失败测试。** 导入预览有警告而不写入、重复名称 409、替换敏感项不回新明文、缺确认导出 400。原四个测试函数及输入均保留。
- [ ] **步骤 2：确认失败。** 对应选择器预期路由未注册导致失败。
- [ ] **步骤 3：预览/创建。** previewRequest 只有 Text，createSnapshotRequest 有 Name/AppID/Text。预览调用 ParseKV 返回 keys/sensitive/warnings，空解析 400 empty_import。创建校验名称、解析、Stat 检查 name.json；存在返回 409 snapshot_exists，否则逐项 Set 加密、Save，201 返回摘要。
- [ ] **步骤 4：条目修改。** updateItemRequest 含 Value 与可空 Secret。PUT 校验快照名/key，复用导出的 ValidateKey，不在 UI 复制正则；载入快照/密钥，Set/Save 后返回 VisibleItem，敏感 value 为 null。DELETE 载入并 Delete，false 返回 404 key_not_found，否则保存并 204。
- [ ] **步骤 5：确认导出。** exportRequest 含 Confirm；false 返回 400 export_confirmation_required。确认后载入、逐项解密、排序，输出 `KEY = value\n`；no-store、text/plain UTF-8、attachment 文件名 name.txt。不记录请求/响应体。
- [ ] **步骤 6：handler 全测。** 原 UI 包命令预期 PASS。

## 任务 4：嵌入已确认的 V6 界面

[共享历史代码、测试、调色板与命令](2026-08-25-local-web-ui.md#task-4-embed-the-approved-v6-web-interface)。文件：三个 static 文件及 ui.go。

- [ ] **步骤 1：静态骨架失败测试。** `TestRootServesEmbeddedWorkspace` 注入临时目录/合成密钥，根路径应 200 且包含 `id="app"`。
- [ ] **步骤 2：确认失败。** 原定向测试预期根路径尚未提供而失败。
- [ ] **步骤 3：语义 HTML。** aside 含产品标识、import、recent-work、snapshot-list、本地身份；header 含 breadcrumb 和更多操作；main#app 含 hero、snapshot-context、config-search、config-table、compare-panel、next-steps。提供可访问的导入/预览、编辑替换、删除、对比目标、导出确认及内联错误 dialog；引入 defer app.js 和 app.css。初始 HTML 不放真实值，加载后才请求 API。
- [ ] **步骤 4：V6 CSS。** 原 palette 完整保留在共享代码。桌面白色侧栏 248px，820px 以下隐藏；暖白画布、54px 顶栏、衬线标题、深炭文字、珊瑚活动导航/差异。图标搜索、等宽 key/value、右对齐值、浅分隔/悬停；敏感值掩码点加长度。桌面下一步卡片 sticky，移动端 static；低对比边框，无装饰投影。
- [ ] **步骤 5：浏览器逻辑。** 单一 state 包含 snapshots/active/snapshot/compare/recentWork。api helper fetch 并解析错误信封，非 2xx 抛 message。下列九项是原行为要求，不是现行操作。

1. 加载列表、渲染侧栏、选择首项、读取安全视图；最近工作仅存内存，成功后加导入/对比记录及本地时间戳，刷新丢弃，不用 localStorage/sessionStorage/cookies/API。
2. 普通值显示文字，敏感值显示掩码点和长度；不推测或缓存敏感明文。
3. 输入时在内存按 key 和普通可见值过滤。
4. 普通项编辑预填值；敏感项替换框为空密码框，提示无法显示当前值。
5. 保存 PUT，删除确认后 DELETE，均刷新当前快照。
6. 导入先预览警告/分类，再确认 POST。
7. 对比选择目标，渲染 +、-、~；敏感项只显示存在性/变化/长度。
8. 明确确认后才请求 `{confirm:true}`，拿 Blob 下载，不在页面显示完整明文。
9. 错误在画布/dialog 内显示，不 console.log 含用户数据的负载或错误。

- [ ] **步骤 6：嵌入静态文件。** embed 三个 static 文件，根路径/CSS/JS 使用正确类型，不混入快照值；HTML/API no-store，CSS/JS `private, max-age=3600`。
- [ ] **步骤 7：UI 测试与人工查看。** 原代码保留 go test 和临时快照目录启动命令，预期测试通过、输出 loopback URL、展示空工作区，Ctrl-C 收尾。临时快照目录并不自动隔离用户钥匙，当前不得重放。

## 任务 5：注册 UI 命令与文档

[共享历史代码、命令与完整 README 样例](2026-08-25-local-web-ui.md#task-5-register-the-ui-cli-command-and-document-it)。文件：cli.go、cli_test.go、README。

- [ ] **步骤 1：参数失败测试。** 为不启动服务的纯 parseUIPort helper 加 TestUIOptions；0 合法、70000 拒绝；必要时从 runUI 提取。
- [ ] **步骤 2：确认失败。** 原选择器预期 parseUIPort 不存在导致编译失败。
- [ ] **步骤 3：注册/实现 ui。** Run 增加 case，usage 记录 ui 的 dir/port/no-open；使用 flag.ContinueOnError，port 默认 0，通过 snapDir/parseUIPort 后调用 ui.Start。只在正常关闭返回 0，启动错误调用 fail；补 ui/context 等 import，更新 zsh/bash/fish 补全。
- [ ] **步骤 4：定向 CLI 测试。** UIOptions 与 UnsafeSnapshotName 选择器预期 PASS。
- [ ] **步骤 5：记录 Web UI。** 原计划在当时交互模式节后增加“本地 Web UI”：三个启动样例、仅绑定 127.0.0.1、首版不 reveal、导出先风险确认再本地下载、不持久化浏览器快照、API no-store。中文 README 原文完整留在英文文件四反引号外层块中；同时更新命令与补全文档。
- [ ] **步骤 6：自动检查。** 原 gofmt 文件表、`go test ./...`、`go vet ./...` 均保留，预期全为 0；不是已跑结果。
- [ ] **步骤 7：人工安全检查。** 原计划使用带醒目合成敏感值的临时快照，检查以下项目；不得使用真实存储/钥匙执行。

1. GET snapshots/prod 不含敏感明文或密文。
2. GET compare?from=prod&to=test 不含新旧敏感明文。
3. 敏感编辑框为空。
4. 接受确认前不开始导出。
5. 服务 URL 以 `http://127.0.0.1:` 开头。

## 收尾记录

[原收尾说明](2026-08-25-local-web-ui.md#completion-notes)保留。

- 2026-08-25 会话曾记录“仓库无初始提交、用户未要求 commit”；该描述与授权范围均已失效，不代表当前仓库状态。
- 当时要求不把 `.superpowers/brainstorm/` 预览制品纳入实现，它们是设计会话材料，不是产品资产。
- 当时要求完成前运行上述验证并报告真实结果；本归档不是运行许可，也不把预期通过写成实际通过。

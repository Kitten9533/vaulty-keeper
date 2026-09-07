# 全量 UI 迁移实施计划

[English](2026-08-25-full-ui-migration.md) | 中文

> **历史计划来源，2026-08-25；生命周期标注于 2026-09-07。不可执行或重放。** 完整保留应用层提取理由、拟议代码、测试及已知样例错误。原 `subagent-driven-development` / `executing-plans` 要求仅属于当时会话，现已失效。现行操作/安全见 [UI 指南](../../ui-guide.zh-CN.md)、[Apollo 指南](../../apollo-snapshot-guide.zh-CN.md)。设计见[全量迁移](../specs/2026-08-25-full-ui-migration-design.zh-CN.md)，身份后继见 [Env+AppID](../specs/2026-08-25-env-appid-and-snapshot-delete.zh-CN.md)。
>
> **保留职责：** 英文配对完整保存两种语言共享的历史代码与命令；本文翻译叙述并逐任务链接。必须保留每段代码/测试、预期结果、嵌套 README 和多余 `</style>` 的勘误，不能用本文替换或删除长计划。源码中的中文字符串有意保留。复选框和预期通过不是当前状态。TTY 菜单、可选 AppID、`/api/aes/config` 路由、暂缓 AI 明文边界、所有明文均确认的表述已过时，不得恢复。旧冒烟测试会写 Keychain、暴露生成的秘密，并非隔离夹具，不得运行。

**目标：** 将全部 Apollo/AES/密钥管理迁入仅 loopback 的 UI；UI 与保留的 CLI 共享 internal/app，CLI 命令、flags、输出格式保持逐字节一致。

**架构：** internal/app 作为单一领域逻辑层，包含快照、AES、aes.json、初始化。CLI 成为保留输出契约的薄适配；UI handler/嵌入前端增加 init/reveal/edit/aes API、AES 工具与设置侧栏。原计划要求 reveal/edit-load/export 使用 `confirm:true` 且浏览器不持久化。

**技术栈：** Go 标准库 net/http、embed、httptest，现有 Apollo/AES 包，原生 HTML/CSS/JS。

## 文件结构

[完整历史文件表](2026-08-25-full-ui-migration.md#file-structure)：

- 新建 internal/app 的 key.go/key_test.go，包装初始化/可用性。
- 新建 aes.go/aes_test.go，包含加解密、生成、aes.json。
- 新建 snapshot.go/snapshot_test.go，包含 import/get/set/delete/compare/export/reveal/edit。
- 修改 CLI cli.go 调用 app；interactive.go 的 aes.json 逻辑下沉；原则上现有 CLI 测试无需改动。
- 修改 UI ui.go/ui_test.go 增加路由与测试。
- 修改静态 HTML/CSS/JS：侧栏、AES、设置、reveal、整份编辑及样式/切换。
- 修改 README 说明全功能覆盖。

## 收尾记录

[原记录](2026-08-25-full-ui-migration.md#completion-notes)：

- 2026-08-25 曾记录仓库无初始提交、用户未要求 commit；这不是当前仓库事实或后续授权。
- 原计划不修改 internal/apollo、internal/aesx，它们已负责加密基础。
- 原计划因未隔离 Keychain 写入而不单测 InitKey，将它列入人工检查及 handler 非 Keychain 路径；清单不证明已通过。不得针对真实 Keychain 执行。

## 任务 1：App 包骨架与密钥操作

[共享历史代码、测试、命令](2026-08-25-full-ui-migration.md#task-1-create-the-internalapp-package-skeleton-and-key-operations)。文件：key.go/key_test.go。

- [ ] **步骤 1：失败测试。** TestKeyAvailableWithEnvKey 使用 Base64 合成 32 字节密钥及非法编码验证可用性；TestSnapshotKeyFromEnv 验证首字节 7 与长度 32。
- [ ] **步骤 2：确认失败。** 原定向 go test 预期 app 包尚不存在而构建失败。
- [ ] **步骤 3：实现。** 包不格式化输出，由调用方负责展示。InitKey(force) 包装 GenerateAndStoreKey，force 可重新生成；KeyAvailable 以 SnapshotKey 是否成功判定；SnapshotKey 包装 Apollo 解析。
- [ ] **步骤 4：定向测试。** 原两项测试预期 PASS。
- [ ] **步骤 5：包测试。** 原 `go test ./internal/app` 预期 PASS，当时还没有其他测试。

## 任务 2：App AES 操作

[共享历史代码、测试、命令](2026-08-25-full-ui-migration.md#task-2-create-the-internalapp-aes-operations)。文件：aes.go/aes_test.go。

- [ ] **步骤 1：失败测试。** AESConfigRoundTrip 通过临时目录及 VAULTY_KEEPER_AES_CONFIG 验证缺文件为空、保存后读回、清除后不存在；GenKey 验证合法 16/12 和非法 15/11；EncryptDecryptRoundTrip 包含中英文字符串。
- [ ] **步骤 2：确认失败。** 原选择器预期 AESConfigLoad、GenKey、Encrypt、Decrypt 未定义。
- [ ] **步骤 3：实现。** 原完整代码保留 AESConfig{Key,IV}、环境覆盖路径，否则 HOME/.vaulty/aes.json，取 HOME 失败回相对路径；缺文件为空，JSON 解析错误返回；保存建目录 0700、文件 0600、JSON 缩进加换行；清除不存在不报错。GenKey 只接受 key 16/24/32 字节与 IV 12/16，用 crypto/rand 从字母数字集生成；Encrypt/Decrypt 包装 Java CryptoUtil 兼容 aesx。
- [ ] **步骤 4：验证。** 原定向 AES 测试预期 PASS，不是通过证据。

## 任务 3：App 快照操作

[共享历史代码、测试、命令](2026-08-25-full-ui-migration.md#task-3-create-the-internalapp-snapshot-operations)。文件：snapshot.go/snapshot_test.go。

- [ ] **步骤 1：失败测试。** helper 用合成密钥与临时快照。TestImportGetSetDelete 测读写删除；Compare 测 added/removed/changed；ExportAndEditRoundTrip 检查排序全文及新增后持久化；Reveal 测快照内 AES 配置与显式覆盖，两套密钥/密文均为样本；ImportRejectsInvalidName 拒绝 ../escape。完整数据与断言留在源码。
- [ ] **步骤 2：确认失败。** 原选择器预期函数未定义。
- [ ] **步骤 3：实现。** Import 校验名称、ParseKV、拒绝空结果，NewSnapshot/Set/Save；存在性/覆盖由调用者决定。Get/Set/Delete 校验 key、载入快照；Set 保存后投影 VisibleItem，Delete 不存在返回 false。Compare 校验两名、加载并 Diff。Export 逐项解密、key 排序、生成 KEY=value 文本；EditLoad 复用 Export；EditApply 拒绝空解析，保留 Name/AppID 重新构造整份快照逐项 Set/Save。Reveal 的 key/IV 优先级为显式覆盖、环境、快照；缺配置或任一 target 校验/查找/解密失败即整体错误，不返回部分明文。load 校验名称后读取 name.json。所有函数、错误串和历史签名完整保留。
- [ ] **步骤 4：测试。** 原 app 包测试预期 PASS。
- [ ] **步骤 5：格式/vet。** 原 gofmt -l 预期无输出，go vet 预期 0。

## 任务 4：CLI AES 与交互配置改用 App

[共享历史替换代码与命令](2026-08-25-full-ui-migration.md#task-4-refactor-the-cli-aes-commands-and-interactive-aesjson-to-use-internalapp)。文件：cli.go、interactive.go；现有 CLI 测试保持绿。

- [ ] **步骤 1：导入 app。** 加在现有 aesx import 附近。
- [ ] **步骤 2：aesGenKey。** 用 app.GenKey 替换随机生成到函数末尾部分，保留 flags、尺寸检查、SECRET_KEY/IV 输出格式。移除不再使用的本地 keyCharset/randString 及 crypto/rand、math/big。
- [ ] **步骤 3：aesOp。** flags、env、file/args/stdin 输入流程不变，只把操作换为 app.Encrypt/Decrypt，保留错误与输出。aesx import 暂留至任务 6，因为 apolloReveal 仍用。
- [ ] **步骤 4：interactive.go。** 导入 app，删本地 aesConfigPath/loadAesConfig/saveAesConfig 及确实闲置字段；runInteractive 通过 app.AESConfigLoad 填 aesKey/aesIV；cmdCustomKeyIv 用 app.AESConfigSave 与 AESConfigPath，保留警告/成功输出。
- [ ] **步骤 5：检查。** 原 gofmt 与 CLI 测试预期全绿；只删除确认无引用的旧 helper 以修编译问题。

## 任务 5：CLI Apollo 常规命令改用 App

[共享历史替换代码与命令](2026-08-25-full-ui-migration.md#task-5-refactor-the-cli-apollo-commands-initimportgetsetunsetcompareexport-to-use-internalapp)。文件：cli.go，保留 CLI 测试。

- [ ] **步骤 1：init。** GenerateAndStoreKey 换为 app.InitKey，成功输出仍为 `snapshot key created and stored in macOS Keychain`。
- [ ] **步骤 2：import。** 保留 flags、读取、警告、空解析检查；解析密钥/目录后 app.Import，打印数量/名称/路径。原说明称 app 不检查存在性，沿用当时 CLI 覆盖语义；这是历史，不是当前覆盖授权。
- [ ] **步骤 3：get。** 保留 flags/参数，路径加载部分改 app.GetValue；处理错误/不存在，打印值并返回 0。
- [ ] **步骤 4：set。** 保留 flags/参数/secret 计算，改 app.SetValue，保留成功输出。
- [ ] **步骤 5：unset。** 改 app.DeleteValue，false 报不存在，保留成功输出。
- [ ] **步骤 6：compare。** 只把快照加载改 app.Compare；参数、目录/密钥、val helper、JSON、彩色行及无差异分支不变。
- [ ] **步骤 7：export。** 保留 flags/参数/--copy pbcopy；改 app.Export 后写入 strings.Builder。
- [ ] **步骤 8：检查。** gofmt、CLI 测试预期全绿；若 app 错误措辞改变，原计划允许相应调整错误断言，但成功输出不得变。

## 任务 6：CLI Reveal 与 Edit 改用 App

[共享历史替换代码与命令](2026-08-25-full-ui-migration.md#task-6-refactor-the-cli-reveal-and-edit-commands-to-use-internalapp)。文件：cli.go，现有测试。

- [ ] **步骤 1：reveal。** 保留 flags、参数、目录/snapKey；调用 app.Reveal(name, targets, 显式 key/IV)，保持 JSON、单 target 彩色输出、多 target KEY=value 顺序。覆盖/env/快照优先级不变，使用已经解析的 snapKey。
- [ ] **步骤 2：edit。** app.EditLoad 后创建 0600 临时明文文件，defer 删除；按 --editor、EDITOR、vi 选编辑器，sh -c 执行并继承 stdin/out/err；读取内容，ParseKV 打印警告，app.EditApply，输出更新数。源码保留所有失败处理，不是当前明文操作许可。
- [ ] **步骤 3：检查。** gofmt/CLI 测试预期通过；重点保留假编辑器和多 key reveal JSON 测试。aesx 无引用后才删除 import。
- [ ] **步骤 4：全测。** 原 go test ./... 预期所有包 PASS。

## 任务 7：新增 UI Handler

[共享历史实现、测试与命令](2026-08-25-full-ui-migration.md#task-7-add-the-new-ui-handler-endpoints)。文件：ui.go/ui_test.go。

- [ ] **步骤 1：失败测试。** 添加 reveal/edit 缺确认 400、edit 重加密且摘要无值、gen-key/no-store/尺寸、AES 往返、临时 aes.json GET/PUT/DELETE、已有密钥 init 冲突、init 错方法。补 encoding/json/path/filepath imports；完整九项测试及断言保留。
- [ ] **步骤 2：确认失败。** 原九项选择器预期路由未注册 404。
- [ ] **步骤 3：路由。** 导入 app，注册 init、aes/gen-key、aes/transform、aes/config。
- [ ] **步骤 4：snapshotView 分发。** reveal 只 POST；edit 的 POST 加载、PUT 保存；其余方法返回 405。
- [ ] **步骤 5：init/reveal/edit。** 原源码保留 request 类型和完整 handler：init 解码 force，已有 key 且不 force 则 409，否则 InitKey/201；reveal 要 confirm 和非空 targets，校验名、取 key、app.Reveal、不返回部分结果；editLoad 要 confirm、取 key、app.EditLoad，返回 text；editApply 拒绝空 text，取 key、app.EditApply，把无条目映射 empty_import，成功返回 name/total。保留 invalid_json、method_not_allowed、key_exists、key_init_failed、confirm_required、invalid_key、snapshot_key_unavailable、decrypt_failed、snapshot_edit_failed 等历史错误映射及 loadError。
- [ ] **步骤 6：AES。** gen-key 只 POST，调用 app.GenKey，参数错误 400；transform 只 POST，解码 op/key/iv/text、拒绝缺 key/IV 或未知 op，调用 app 加解密，失败 aes_op_failed，成功 result。config GET 返回 key/iv/path；PUT 要非空并保存；DELETE 清除并 204/no-store；保留 JSON/IO/方法错误处理。此 config 路由是被替代的历史提案。
- [ ] **步骤 7：handler 全测。** 原 UI 包测试预期新旧全部 PASS。
- [ ] **步骤 8：格式/vet。** 原命令预期 clean。

## 任务 8：前端侧栏、AES、设置、Reveal、整份编辑

[共享完整 HTML/CSS/JS、测试及冒烟脚本](2026-08-25-full-ui-migration.md#task-8-extend-the-frontend--rail-sections-aes-tools-settings-reveal-bulk-edit)。文件：static/index.html、app.css、app.js。

- [ ] **步骤 1：骨架失败测试。** TestRootServesFullWorkspace 检查根路径 200 与 view-aes、view-settings、reveal-dialog、edit-dialog；原定向测试预期新元素缺失而失败。
- [ ] **步骤 2：替换 HTML。** 共享源码保留整份文档，不以译文代替。包括 zh-CN/viewport/title、侧栏标识/导入/最近工作/快照/工具/本地身份、顶栏、快照搜索/表格/对比/下一步、AES key/IV/输入/结果/预填/生成/保存/复制、设置密钥状态/初始化/清除配置，以及导入、条目、删除、对比、导出含复制、reveal、整份 edit、未来安全摘要占位对话框。保留所有 DOM id、中文文案、可选 AppID、警告、hidden 状态及脚本引用作为历史来源。
- [ ] **步骤 3：CSS。** 追加导航活动/悬停、view hidden、工具 panel/fields/actions/textarea、状态、reveal box 样式。原 snippet 末尾误含 `</style>`：原注释明确应省略，因为是在 CSS 文件追加而非 style 标签内；代码与勘误同时保留。
- [ ] **步骤 4：JavaScript。** 保持原行为，拟作以下十二项改动；所有函数体在共享来源中原样保留。

1. state 增加 `view:'snapshots'`。
2. 不新增 API helper：reveal/edit 返回 JSON，复制导出用 Response.text() 读取明文。
3. renderRecent 后增加 switchView：切换 snapshots/aes/settings 的 hidden 与 active，更新面包屑，进入 AES/设置时载入配置/状态。
4. AES：缓存配置用于预填；读取 key/IV/text，校验空输入，调用 transform 显示 result；生成 16/12 填表；保存配置并更新缓存；错误内联；结果通过 clipboard 复制并反馈成功/失败。
5. 设置：请求 snapshots 判定密钥状态/显示初始化按钮；读取 AES 配置、路径及清除按钮；init 使用 force:false，更新状态；clear 删除配置、清缓存、刷新；错误内联。
6. Reveal：记录 item，清空/隐藏旧值，显示确认按钮；确认后调用带 targets/confirm 的端点，显示值或空值提示，隐藏确认；错误显示在 dialog。
7. 导出复制：无快照时报错；confirm POST，读取 text，写 clipboard，关闭 dialog 并记录最近工作。
8. 整份编辑：打开时清空文本、隐藏保存并要求确认加载；load POST confirm 后填文本，置 editLoaded；save 仅在加载后 PUT，关闭、记录、刷新。错误保留在 dialog。
9. renderTable 的敏感行添加 reveal 按钮，保留掩码及长度；点击 stopPropagation，打开 reveal；非敏感值仍显示文本。
10. wire 连接 bulk-edit/aes-tools data-action、两项导航、六项 AES 控件、初始化/清除、reveal/edit submit/edit load，以及导出复制。原所有事件绑定块完整保留。
11. init 在 wire 后继续默认 switchView('snapshots')。
12. 追加 `.reveal-btn` 与 hover 样式，保留原 CSS 数值。

- [ ] **步骤 5：静态/全部 UI 测试。** 原命令依次执行两个 workspace 测试与 UI 包全测，预期 PASS。
- [ ] **步骤 6：服务/API 冒烟检查。** 原脚本构建 /tmp 二进制、临时快照目录、后台服务，解析日志 URL，curl 根路径、aes/gen-key、init，然后 kill 与删除二进制。原预期为根路径 200、gen-key 返回 key/iv、init 首次 201 或 Keychain 已有钥匙 409。这会接触真实 HOME/Keychain，且输出秘密；“开发机可接受”仅是历史判断，绝非现行授权或隔离证据，不执行。

## 任务 9：README 与最终验证

[共享完整 README 样例与原检查](2026-08-25-full-ui-migration.md#task-9-update-readme-and-run-full-verification)。文件：README。

- [ ] **步骤 1：README。** 替换“本地 Web UI”节：保留三个启动命令、仅本机绑定、快照 CRUD/导入/对比/明文编辑/导出/AES/初始化全部覆盖、所有明文出口确认/no-store/不持久化的旧声明。原中文片段用四反引号外层完整保留，不作为现行保证。开头从快照浏览扩展为全部快照/AES；“其他”节 ui 命令项不变。
- [ ] **步骤 2：格式检查。** 原 `gofmt -l internal/app internal/cli internal/ui` 预期无输出。
- [ ] **步骤 3：全测。** 原 go test ./... 预期所有包 PASS。
- [ ] **步骤 4：vet。** 原 go vet ./... 预期 0。
- [ ] **步骤 5：人工安全检查。** 原计划拟用带醒目合成敏感值的临时快照，经浏览器或 curl 检查以下七项；这是未证实的计划预期，不是重放授权。

1. GET snapshots/prod 无敏感明文/密文。
2. GET compare 无新旧敏感明文。
3. Reveal 只在确认后显示，输入不预填。
4. 全量 edit 只在确认后加载，保存重新加密，刷新新值可见且敏感项仍掩码。
5. AES 加解密往返、生成填表、保存 aes.json 后刷新保留。
6. 无 Keychain key 时显示/可用初始化，有 key 时隐藏。
7. 所有明文响应 no-store，URL 以 `http://127.0.0.1:` 开头。

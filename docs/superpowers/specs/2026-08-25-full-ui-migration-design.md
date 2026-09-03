# Full UI Migration Design

## Goal

把 `vaulty-keeper` 的全部功能迁移到本地 Web UI，使其成为唯一的主要手动入口；CLI 保留全部子命令作为脚本与自动化接口，现有行为与输出格式完全不变。

## Scope

本次迁移覆盖：

- **apollo**：init、import、list/get、set、unset、compare、reveal、edit、export（含剪贴板复制）。
- **aes**：encrypt、decrypt、gen-key、key/iv 管理（`~/.vaulty/aes.json`）。
- **密钥管理**：快照密钥初始化引导（UI 内完成）。
- **CLI**：全部保留，仅重构内部实现以共享应用层，输出不变。

不包含（用户明确暂缓）：AI 调用方的明文安全边界、外部 AI 对接、MCP server、AES 之外的加解密、多用户/远程托管。

## 产品形态

单页应用，沿用 Claude 浅色视觉系统。侧栏在原「快照工作区」之外增加两个分区：

- **快照工作区**（现有）：快照列表、浏览、搜索、单条目增删改、导入、对比、导出。
- **AES 工具**：encrypt / decrypt 表单、gen-key 一键生成、key/iv 从 `aes.json` 预填或手动输入。
- **设置**：快照密钥状态与初始化引导、`aes.json` 管理（查看/保存/清除）。

明文出口（reveal、edit 全量编辑、export、aes 解密结果）统一规则：**显式二次确认后显示**，提供关闭按钮，绝不写入 localStorage/sessionStorage/cookies，响应 `Cache-Control: no-store`。

## Architecture

新增 `internal/app` 包作为唯一领域逻辑层，CLI 与 UI 都调用它：

```
internal/app/
  snapshot.go    apollo 领域操作
  aes.go         aes 加解密 + aes.json 读写
  key.go         快照密钥解析 / 可用性判断
```

- `internal/cli` 改为薄适配：flag 解析 → `app` 层 → 格式化输出（JSON/彩色/掩码逻辑留在 CLI）。所有命令名、flag、输出格式不变。
- `internal/ui` 的 handler 同样调用 `app` 层，只做 HTTP 适配（参数解析、错误信封、no-store）。
- `internal/cli/interactive.go` 中的 `aes.json` 读写（`aesConfigPath`/`loadAESConfig`/`saveAESConfig`）迁入 `internal/app`，CLI 与 UI 共用。

### 下沉到 app 层的操作

- apollo：`Init`、`Import`、`GetValue`、`SetValue`、`DeleteValue`、`Compare`、`Reveal`、`EditLoad`、`EditApply`、`Export`
- aes：`Encrypt`、`Decrypt`、`GenKey`、`AESConfigLoad`、`AESConfigSave`、`AESConfigClear`
- key：`SnapshotKey`、`KeyAvailable`

## API Contract

所有 `/api/*` 响应 `Cache-Control: no-store`。现有端点不变；新增：

```text
POST /api/init                          {force:bool}                   → 201 生成并存储快照密钥
POST /api/snapshots/{name}/reveal       {targets:[...], confirm:true}  → 200 {values:{key:"明文"}}
POST /api/snapshots/{name}/edit         {confirm:true}                 → 200 {text:"KEY = value\n..."}
PUT  /api/snapshots/{name}/edit         {text:"..."}                   → 200 全量重加密保存
POST /api/aes/gen-key                   {bytes, iv_bytes}              → 200 {key, iv}
POST /api/aes/transform                 {op:"encrypt"|"decrypt", key, iv, text} → 200 {result}
GET  /api/aes/config                    → 200 {key, iv, path}
PUT  /api/aes/config                    {key, iv}                      → 200 保存到 aes.json
DELETE /api/aes/config                  → 204 清除 aes.json
```

### 端点行为

- `POST /api/init`：调用 `app.Init(force)`。`force:false` 且密钥已存在 → `409 key_exists`；`force:true` 时即使已有密钥也重新生成；成功 201。前端只在密钥不可用时显示引导按钮（不传 force）。
- `POST /api/snapshots/{name}/reveal`：`confirm` 非 true → `400 confirm_required`。校验快照名与每个 target（`ValidateKey`）。服务端从快照内解析 AES 配置（`imile.fs.aes.secret-key` / `imile.fs.aes.iv`，与 CLI `reveal` 默认行为一致；UI 不提供手动 key/iv 覆盖，需要手动加解密时走 AES 工具区），逐 target 解密；任一失败返回结构化错误且不返回部分明文。
- `POST /api/snapshots/{name}/edit`：`confirm` 非 true → `400 confirm_required`。返回排序后的 `KEY = value\n` 全量明文文本。
- `PUT /api/snapshots/{name}/edit`：解析文本（`ParseKV`），无条目 → `400 empty_import`；全量重加密（`NewSnapshot` + `Set` + `Save`），语义与 CLI `edit` 一致。返回新快照的 safe 摘要。
- `POST /api/aes/gen-key`：`bytes ∈ {16,24,32}`、`iv_bytes ∈ {12,16}`，否则 `400 invalid_aes_params`。返回可打印 key/iv。
- `POST /api/aes/transform`：`op` 非 encrypt/decrypt → `400 invalid_aes_params`；key/iv 缺失 → `400`；调用 `aesx.Encrypt/Decrypt`，错误 → `400 decrypt_failed`（encrypt 侧错误码 `aes_op_failed`）。
- `GET /api/aes/config`：返回 `aes.json` 当前内容与路径；文件不存在 → `{key:"",iv:""}` 而非错误。
- `PUT /api/aes/config`：key/iv 非空，写文件（0600）；成功 200。
- `DELETE /api/aes/config`：删除文件（不存在也返回 204）。

### 错误信封（沿用）

```json
{ "error": { "code": "confirm_required", "message": "..." } }
```

新增错误码：`confirm_required`、`decrypt_failed`、`aes_config_io`、`key_init_failed`、`invalid_aes_params`、`aes_op_failed`、`key_exists`。

## Security Rules

- 明文出口（reveal、edit load、export、aes transform 解密）响应均为 `Cache-Control: no-store`。
- 浏览器端不把明文写入 localStorage/sessionStorage/cookies；明文只在用户主动确认后的当前视图短暂存在。
- `GET /api/aes/config` 返回的 key/iv 属敏感明文，仅用于表单预填，不做其他持久化。
- `reveal` 任一 target 解密失败则不返回任何明文。
- 所有写操作失败不改变现有快照；不引入新依赖。

## Primary Flows

### 密钥初始化

1. UI 检测快照密钥不可用 → 快照区显示引导卡片：「运行 `vaulty-keeper apollo init` 或点击生成」。
2. 点击生成 → `POST /api/init` → 成功后刷新快照列表。

### 明文编辑（替代 CLI `edit`）

1. 快照上下文点「明文编辑」→ 确认对话框。
2. `POST .../edit {confirm:true}` 返回全量明文 → 多行文本框。
3. 编辑后保存 → `PUT .../edit {text}` → 服务端全量重加密 → 刷新视图。

### Reveal

1. 条目上点「解密显示」→ 确认对话框（说明将显示明文）。
2. `POST .../reveal {targets, confirm:true}` → 显示明文 + 关闭按钮。

### AES 工具

1. encrypt/decrypt 表单，key/iv 手动输入或从 `aes.json` 预填。
2. gen-key 生成后自动填入表单，可「保存为默认」。
3. 结果可复制（`navigator.clipboard`，loopback 为 secure context）。

## Error Handling

- 所有失败保持当前选中快照不变，错误内联显示在活动画布/对话框。
- 与现有 UI 规则一致：无 console.log 输出可能包含用户数据的明文。

## Testing

- `internal/app`：reveal 解密往返、edit load/apply 往返（含敏感/非敏感混合）、aes.json 读写/清除、gen-key 参数校验、init 幂等。
- `internal/ui`：新增端点——confirm 缺失返回 400、明文响应 no-store、reveal 正常返回、aes transform/gen-key/config 读写。
- `internal/cli`：现有测试保持绿，作为「重构无回归」的验证。
- `internal/aesx` / `internal/apollo`：不动。
- 前端手动验证清单（任务末尾）：reveal 确认后才出明文、edit 往返一致、export 确认后才下载、aes 加解密往返、明文不出现在任何响应之外。

## Non-Goals and Follow-Up Work

- AI 调用方明文安全边界（用户明确暂缓）：CLI 保留现状，不做 agent 检测或掩码强制。
- 不引入新依赖、不引入前端框架。
- TTY 交互菜单保留作为无浏览器/远程场景的降级入口，不做改动。
- completion/version 保持 CLI 专属。
- 后续可选项：外部 AI 安全摘要（safe-diff）、`safe-json` 导出、MCP server。

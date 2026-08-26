# Env+AppID Unique Key, Snapshot Delete, and Import Dialog Polish — Design

## Goal

以「环境 + AppID」作为快照唯一键（AppID 必填），支持在 UI 与 CLI 中删除快照，快照列表同时展示环境与 AppID，并优化导入对话框的预览滚动。

## Scope

- 存储：新快照一律存 `{env}__{appid}.json`；旧 `{env}.json`（无 AppID）读取兼容。
- CLI：各命令支持 `--appid`；`import` / `rm` 必填；新增 `rm` 删除命令（需确认）。
- UI：侧栏列表展示环境 + AppID；列表项删除入口（确认对话框）；导入时 AppID 必填、冲突按 env+appid 判定；预览区可滚动。
- 不做：快照迁移脚本、按 AppID 的批量操作、多 AppID 快照对比自动选择（对比需显式传 from/to appid）。

## Storage & Naming

`internal/apollo`：

- 新增 `SnapshotRef{ Name, AppID string }`。
- `ListSnapshots(dir) ([]SnapshotRef, error)`：扫描 `*.json`，读取 meta 得到 Name 与 AppID（不靠文件名解析）。
- 新增 `ValidateAppID(appID) error`：非空，字符集 `^[A-Za-z0-9][A-Za-z0-9_.-]*$`。
- 新增 `FileName(name, appID) string`：appID 非空 → `{name}__{appID}.json`；appID 空 → `{name}.json`。
- 新增 `SnapPath(dir, name, appID) string` = `filepath.Join(dir, FileName(...))`。
- 冲突判定 = 目标文件已存在（`{env}__{appid}.json` 或 `{env}.json` 天然覆盖了「env 含 `__` 撞名」的边缘情况）。
- 现有 `snapPath(dir, name)`（CLI 层）改为接收 appID；`ValidateSnapshotName` 不变。

## CLI

- 各 apollo 命令加 `--appid`（get / list / set / unset / compare / reveal / edit / export / import / rm）。访问类命令不指定时读 `{env}.json`（兼容旧快照）。
- `import`：`--app-id` 改为必填，参与文件命名；缺省报错。
- 新增 `apollo rm <env> --appid X`：`--appid` 必填（本版删除只针对有 AppID 的快照）。
  - TTY 下交互确认 `删除 env (appid) ？[y/N]`；非 TTY 必须带 `--yes` 才执行。
  - 成功输出 `removed env (appid)`，退出码 0；不存在 → 报错退出码 1。
- usage / completion 更新。

## UI API

`appid` 一律走 query 参数（URL encoded），路径结构不变，兼容旧快照。

- `GET /api/snapshots`：返回项已含 `app_id`，保持不变。
- `GET /api/snapshots/{env}?appid=X`：读取 `{env}__{X}.json`；不传 appid 读 `{env}.json`。
- `POST /api/snapshots`：`{env, app_id(必填), text}`；app_id 缺失/非法 → `400 invalid_app_id`；文件已存在 → `409 snapshot_exists`（消息含 env 与 appid）。
- `DELETE /api/snapshots/{env}?appid=X`：删除文件 → `204`；不存在 → `404 snapshot_not_found`。
- 条目增删改、export、reveal、edit 各端点均接受可选 `?appid=`。
- `GET /api/compare?from=&to=&from_appid=&to_appid=`：appid 可选，缺省读 `{env}.json`。

## UI Frontend

- 侧栏列表项两行：第一行 `环境` + 条目计数，第二行小字 `AppID`（读取自 `app_id` 字段）。
- 列表项 hover 显示删除按钮（不触发选中）；点删除 → 确认对话框（显示 `环境 / AppID`）→ `DELETE` → 刷新；若删的是当前选中，自动切到第一个剩余快照或空状态。
- `state.active` 由纯 env 改为 `{ env, appid }`；所有 API 调用带上 `?appid=`。
- 导入对话框：「快照名称」label 改为「环境」；「应用 ID」必填（空则前端提示）；预览区 `.preview` 加 `max-height: 220px; overflow-y: auto`。
- 冲突错误显示 `环境 (appid) 已存在`（服务端消息）。

## Security

- AppID 参与文件路径，必须经 `ValidateAppID` 校验（防路径穿越）。
- 删除确认只在 UI 确认对话框之后发起；明文出口规则不变（no-store、confirm）。

## Testing

- `internal/apollo`：`ValidateAppID`（合法/空/非法字符/路径穿越）、`FileName` 两种形态、`ListSnapshots` 返回 env+appid、`SnapPath`。
- `internal/app`：`Remove`（存在/不存在）、`Import` 带 appid 命名与冲突、读取旧 `{env}.json` 兼容。
- `internal/cli`：`--appid` 解析与寻址、`rm` 的确认/`--yes`/不存在路径。
- `internal/ui`：`DELETE` 端点（204/404/缺 appid 400）、`POST` 缺 appid 400、带 appid 的视图与 items 端点。
- 前端手动清单：列表双行展示、删除确认与刷新、导入必填与冲突提示、预览滚动。

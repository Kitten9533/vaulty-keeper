'use strict';

const state = { snapshots: [], active: null, activeAppID: '', snapshot: null, compare: null, view: 'snapshots', lastCompareTo: '', collapsedEnv: {} };
const MASK = '••••••••••••';
const TOKEN = new URLSearchParams(location.search).get('t') || '';
const $ = (id) => document.getElementById(id);
let editing = null;

// ---- i18n ----

const I18N = {
  en: {
    'app.title': 'vaulty-keeper · Config Workbench',
    checking: 'Checking…',
    'nav.import': 'Import snapshot',
    'rail.snapshots': 'Snapshots',
    'rail.tools': 'Tools',
    'nav.aes': 'AES encrypt/decrypt',
    'nav.db': 'Database tunnels',
    'nav.settings': 'Settings',
    'foot.local': 'Local workspace',
    'foot.local-sub': 'This device only',
    'hero.snapshots.title': 'Check config status.',
    'hero.snapshots.sub': 'Snapshots are encrypted locally; sensitive values are masked by default.',
    'hero.snapshots.import': 'Import snapshot',
    'plaintext-banner': 'Plaintext endpoints (export / decrypt / plaintext edit / AES key list) are disabled by default; restart the UI with <code>--allow-plaintext</code> to enable them. Do not send this URL\'s token to AI or scripts.',
    'context.loading': 'Loading…',
    'context.encrypted': 'Encrypted',
    'sec.config': 'Config',
    'search.ph': 'Search key or visible value…',
    'search.aria': 'Search config',
    'next.title': 'Next steps',
    'next.compare': 'Compare environments',
    'next.compare-sub': 'Check added, removed and changed.',
    'next.multi': 'Compare across environments',
    'next.multi-sub': 'Same key side by side across environments.',
    'next.item': 'View or edit an item',
    'next.item-sub': 'Search by key; sensitive values are hidden by default.',
    'next.edit': 'Plaintext-edit all',
    'next.edit-sub': 'Edit in plaintext after confirmation and re-encrypt.',
    'next.export': 'Export config',
    'next.export-sub': 'Requires a second confirmation before generating plaintext.',
    'next.note': '<b>Local-first.</b> Plaintext is only shown briefly after confirmation.',
    'aes.title': 'AES encrypt/decrypt.',
    'aes.sub': 'Java CryptoUtil compatible (AES/GCM/NoPadding). Encrypt/decrypt with a manual key/iv.',
    'aes.key': 'AES key (16/24/32 bytes UTF-8)',
    'aes.iv': 'IV (UTF-8 bytes)',
    'aes.input': 'Input',
    'aes.input-ph': 'Plaintext or base64 ciphertext…',
    'aes.encrypt': 'Encrypt',
    'aes.decrypt': 'Decrypt',
    'aes.output': 'Result',
    'aes.copy': 'Copy result',
    'db.title': 'Database tunnels.',
    'db.sub': 'Registered connection URLs are encrypted at rest (dedicated DB key); tunnels are provided by <code>vaulty-keeper serve</code> on the host. Register, test and generate client links here.',
    'db.init-btn': 'Initialize database key',
    'db.add-card': 'New connection',
    'db.name': 'Name',
    'db.name-ph': 'e.g. mysql-orders',
    'db.port': 'Tunnel port (optional, auto-assigned when empty)',
    'db.url': 'Database URL (with credentials; encrypted in the browser before sending)',
    'db.test-btn': 'Test connection',
    'db.add-btn': 'Register connection',
    'db.list-card': 'Connections',
    'db.regen-all': 'Regenerate all',
    'db.regen-all-title': 'Regenerate the tunnel token of every connection; old links stop working immediately',
    'db.th.name': 'Name',
    'db.th.type': 'Type',
    'db.th.state': 'State',
    'db.th.port': 'Port',
    'db.th.actions': 'Actions',
    'settings.title': 'Settings.',
    'settings.sub': 'Local keys and AES config management.',
    'settings.snap-key': 'Snapshot key',
    'settings.init-snap': 'Generate snapshot key',
    'settings.sensitive-key': 'Sensitive-value key',
    'settings.init-sensitive': 'Generate sensitive-value key',
    'dialog.import.title': 'Import snapshot',
    'dialog.import.env': 'Environment',
    'dialog.import.env-ph': 'e.g. prod',
    'dialog.import.appid': 'App ID (required)',
    'dialog.import.appid-ph': 'e.g. merdi-portal',
    'dialog.import.text': 'Paste key=value config',
    'dialog.import.preview': 'Preview',
    'dialog.import.confirm': 'Import',
    'dialog.entry.title': 'Edit item',
    'dialog.entry.warning': 'This is a sensitive value and cannot be shown. Entering a new value replaces it; saving an empty value keeps it unchanged.',
    'dialog.entry.value': 'Value',
    'dialog.delete.title': 'Delete item',
    'dialog.delete.confirm': 'Delete <code id="delete-key"></code>? This cannot be undone.',
    'dialog.snap-delete.title': 'Delete snapshot',
    'dialog.snap-delete.confirm': 'Delete snapshot <code id="snap-delete-name"></code> / <code id="snap-delete-appid"></code>? This cannot be undone.',
    'dialog.compare.title': 'Compare environments',
    'dialog.compare.sub': 'Compare <code id="compare-from"></code> with:',
    'dialog.compare.target': 'Target environment',
    'dialog.compare.start': 'Compare',
    'dialog.compare-result.title': 'Config diff',
    'dialog.compare-result.copy': 'Copy diff',
    'dialog.compare-result.filter-ph': 'Filter diff keys…',
    'dialog.multi.title': 'Compare across environments',
    'dialog.multi.sub': 'Select 2+ snapshots; compare each key side by side across environments.',
    'dialog.multi-result.title': 'Cross-environment compare',
    'dialog.multi-result.report': 'Diff report',
    'dialog.multi-result.copy-tsv': 'Copy as table (Tab-separated)',
    'dialog.multi-result.copy-csv': 'Copy CSV (comma-separated)',
    'dialog.multi-report.title': 'Diff report',
    'dialog.multi-report.copy': 'Copy report',
    'dialog.key-compare.title': 'Key comparison',
    'dialog.export.title': 'Export config',
    'dialog.export.sub': 'This generates a plaintext key=value file of <code id="export-name"></code> and downloads it in the browser.',
    'dialog.export.warning': '⚠ The plaintext includes sensitive values; view on this machine only, do not forward.',
    'dialog.export.copy': 'Copy to clipboard',
    'dialog.export.confirm': 'Export',
    'dialog.reveal.title': 'Reveal',
    'dialog.reveal.warning': '⚠ This shows the plaintext of <code id="reveal-key"></code>; view on this machine only.',
    'dialog.reveal.aes-key': 'AES key',
    'dialog.reveal.iv': 'IV',
    'dialog.reveal.key-ph': 'leave empty to use the system sensitive key',
    'dialog.reveal.hint': 'If the value is not encrypted with the system sensitive key (e.g. external AES ciphertext), fill in key/iv manually and retry.',
    'dialog.reveal.show': 'Reveal',
    'dialog.edit.title': 'Plaintext edit',
    'dialog.edit.warning': '⚠ This edits every entry of <code id="edit-name"></code> in plaintext; it re-encrypts on save.',
    'dialog.edit.text': 'Config content',
    'dialog.edit.save': 'Save and re-encrypt',
    'dialog.summary.title': 'Safe AI summary',
    'dialog.summary.text': 'The summary is generated from visible values and local metadata only; no raw secrets included. This is a placeholder in the current version.',
    'dialog.db-connect.title': 'Connection info',
    'dialog.db-show.title': 'Real connection info',
    'dialog.db-show.sub': 'Only visible in your browser; do not forward.',
    'dialog.db-delete.title': 'Delete connection',
    'dialog.db-delete.confirm': 'Delete connection <code id="db-delete-name"></code>? The tunnel goes away with it; this cannot be undone.',
    'dialog.db-regen.title': 'Regenerate tunnel token',
    'dialog.db-regen.confirm': 'Regenerate',
    'dialog.db-error.title': 'Failed to register connection',
    'common.cancel': 'Cancel',
    'common.close': 'Close',
    'common.delete': 'Delete',
    'common.save': 'Save',
    'common.confirm': 'Confirm',
    'common.ok': 'OK',
    'common.copy': 'Copy',
    'rel.just-now': 'just now',
    'rel.min': '{n} min ago',
    'rel.hour': '{n} hours ago',
    'rel.day': '{n} days ago',
    'rail.empty': 'No snapshots',
    'rail.expand': 'Click to expand',
    'rail.collapse': 'Click to collapse',
    'rail.items': '{n} items',
    'rail.updated': 'Updated {t}',
    'rail.delete': 'Delete',
    'rail.delete-title': 'Delete snapshot',
    'view.aes': 'AES encrypt/decrypt',
    'view.db': 'Database tunnels',
    'view.settings': 'Settings',
    'breadcrumb.workbench': 'Config Workbench',
    'api.fail': 'Request failed ({status})',
    'api.auth-expired': 'Access token is invalid or expired — the UI regenerates it on every start. Reopen the new URL printed by the running `vaulty-keeper ui`.',
    'aes.error.key-iv': 'Please fill in key and iv.',
    'aes.error.input': 'Please fill in the input.',
    'aes.copied': 'Copied to clipboard.',
    'aes.copy-failed': 'Copy failed.',
    'db.count': '{n} connections',
    'db.empty': 'No connections yet. Fill in the name and URL on the left to register one; the vaulty-keeper serve on the host automatically opens a local tunnel for it.',
    'db.broken': 'Key mismatch',
    'db.broken-title': 'Cannot decrypt with the current key',
    'db.state.on': 'On',
    'db.state.off': 'Off',
    'db.test': 'Test conn',
    'db.test-title': 'Test the real registered connection directly with its decrypted URL (not the tunnel)',
    'db.connect': 'Connect info',
    'db.regen': 'Regenerate',
    'db.regen-title': 'Regenerate the tunnel token; old links stop working immediately',
    'db.tunnel-on': 'Turn on tunnel',
    'db.tunnel-on-title': 'Turn on the tunnel; serve picks it up within ~2s',
    'db.tunnel-off': 'Turn off tunnel',
    'db.tunnel-off-title': 'Turn off the tunnel; the port stops listening',
    'db.show-url': 'View URL',
    'db.rm': 'Delete',
    'db.testing': 'Testing…',
    'db.pubkey-fail': 'Cannot fetch the database URL encryption public key',
    'db.init-confirm': 'Initialize the database key? (Only needed if never initialized; do not force-overwrite an existing one or existing connections become undecryptable)',
    'db.init-title': 'Initialize database key',
    'db.init-ok': 'Initialize',
    'db.init-overwrite': 'Key already exists. Force-overwrite? This makes every registered connection undecryptable; they must be re-registered.',
    'db.init-overwrite-title': 'Force-overwrite key',
    'db.init-overwrite-ok': 'Overwrite',
    'db.add-required': 'Name and database URL are required',
    'db.test-url-empty': 'Fill in a database URL first',
    'db.test-ok': '✅ Connected ({type}',
    'db.test-fail': '❌ {err}',
    'db.regen-done-note': 'A new token was generated; old links stopped working immediately',
    'db.connect.raw-label': 'Raw tunnel link (AI / other tools can convert from it)',
    'db.connect.footer': 'The token is the bridge token, not the real database password.',
    'db.regen-confirm-one': 'Regenerate the tunnel token of <code>{name}</code>? Old links stop working immediately; this cannot be undone (the global bridge token is unaffected).',
    'db.regen-confirm-all': 'Regenerate the tunnel token of every connection? All old links stop working immediately; this cannot be undone (the global bridge token is unaffected).',
    'db.regen-done': 'Regenerated the token of {n} connection(s); old links are invalid',
    'db.regen-none': 'No connections to regenerate',
    'db.off-confirm': 'Turn off the tunnel of {name}? The port stops listening and existing connections drop (can be turned back on anytime).',
    'db.off-title': 'Turn off tunnel',
    'db.off-ok': 'Turn off',
    'settings.snap-ok': 'Snapshot key available (Keychain or env var).',
    'settings.snap-missing': 'Snapshot key unavailable. Generate one below.',
    'settings.sensitive-ok': 'Sensitive-value key available (Keychain or env var). Sensitive values use it to encrypt and decrypt.',
    'settings.sensitive-missing': 'Sensitive-value key unavailable. Generate one below.',
    'settings.snap-created': 'Snapshot key generated.',
    'settings.sensitive-created': 'Sensitive-value key generated.',
    'snap.no-selection': 'No snapshot selected',
    'snap.empty-hint': 'No snapshots yet. Import one via "Import snapshot" on the left.',
    'snap.context': '{total} items · {sensitive} sensitive · updated {time}',
    'snap.hero-title': 'Check the config status of {name}.',
    'snap.hero-title-simple': 'Check config status.',
    'table.no-match': 'No matching items',
    'table.empty': 'No config yet. Import one via "Import snapshot" on the left.',
    'table.chars': '{n} chars',
    'table.reveal': 'Reveal',
    'table.compare-key': 'Compare this key',
    'table.compare-key-title': 'View this key across all snapshots',
    'common.no-token': 'No access token: open the full URL printed at startup (with ?t=...) so imports/edits/exports/decrypts work.',
    'import.dupe': 'Snapshot already exists: {name} ({appid}), {total} items. The import will fail; use a different environment or app ID.',
    'import.error.paste': 'Paste the config first.',
    'import.error.env': 'Fill in the environment.',
    'import.error.appid': 'Fill in the app ID.',
    'import.error.dupe': 'Snapshot {name} ({appid}) already exists and cannot be imported again.',
    'import.tag-sensitive': 'Sensitive',
    'entry.title': 'Edit item',
    'entry.title-sensitive': 'Replace sensitive value',
    'entry.ph': 'Enter a new value to replace…',
    'compare.no-other': 'No other snapshots to compare.',
    'compare.error.target': 'Select a target environment.',
    'compare.added': '+{n} added',
    'compare.removed': '-{n} removed',
    'compare.changed': '~{n} changed',
    'compare.no-changes': 'No changes',
    'compare.identical': 'Both environments are identical.',
    'compare.sensitive': 'Sensitive · {n} chars',
    'compare.fp': ' · fingerprint {fp}',
    'compare.copied': 'Copied',
    'compare.copy-failed': 'Copy failed',
    'multi.error': 'Select at least 2 snapshots.',
    'multi.header-title': '{n} items · {s} sensitive',
    'multi.sensitive': 'Sensitive·{n}chars{fp}',
    'key-compare.title': 'Key comparison: {key}',
    'key-compare.title-appid': 'Key comparison: {key} (same appid {appid})',
    'key-compare.loading': 'Loading…',
    'key-compare.empty': 'No snapshot contains this key.',
    'report.title': 'Cross-environment diff report',
    'report.refs': 'Compared environments: {refs}',
    'report.stats': 'Stats: {total} keys, {diff} differ, {same} identical; {sensitiveDiff} sensitive differences',
    'report.all-same': 'All environments are identical.',
    'report.diff-fields': 'Differing keys:',
    'export.no-selection': 'No snapshot selected.',
    'reveal.empty': '(empty)',
    'reveal.show': 'Reveal',
  },
  zh: {
    'app.title': 'vaulty-keeper · 配置工作台',
    checking: '检测中…',
    'nav.import': '导入配置快照',
    'rail.snapshots': '快照',
    'rail.tools': '工具',
    'nav.aes': 'AES 加解密',
    'nav.db': '数据库隧道',
    'nav.settings': '设置',
    'foot.local': '本地工作区',
    'foot.local-sub': '仅此设备可访问',
    'hero.snapshots.title': '检查配置状态。',
    'hero.snapshots.sub': '快照在本地加密，敏感值默认遮罩。',
    'hero.snapshots.import': '导入快照',
    'plaintext-banner': '明文接口（导出/解密/明文编辑/AES 密钥列表）默认禁用；需要时用 <code>--allow-plaintext</code> 重启 UI。当前 URL 的令牌不要发给 AI/脚本。',
    'context.loading': '加载中…',
    'context.encrypted': '已加密',
    'sec.config': '配置',
    'search.ph': '搜索 key 或可见值…',
    'search.aria': '搜索配置',
    'next.title': '下一步',
    'next.compare': '比较环境',
    'next.compare-sub': '检查新增、删除与变更。',
    'next.multi': '横向对比多环境',
    'next.multi-sub': '多环境同 key 并排对比。',
    'next.item': '查看或修改单项',
    'next.item-sub': '按 key 搜索；敏感内容默认隐藏。',
    'next.edit': '明文编辑全部',
    'next.edit-sub': '确认后以明文编辑并重新加密。',
    'next.export': '导出配置',
    'next.export-sub': '生成明文前需要再次确认。',
    'next.note': '<b>本地优先。</b> 明文只在确认后短暂显示。',
    'aes.title': 'AES 加解密工具。',
    'aes.sub': 'Java CryptoUtil 兼容（AES/GCM/NoPadding）。手动输入 key/iv 加解密。',
    'aes.key': 'AES key（16/24/32 字节 UTF-8）',
    'aes.iv': 'IV（UTF-8 字节）',
    'aes.input': '输入',
    'aes.input-ph': '明文或 base64 密文…',
    'aes.encrypt': '加密',
    'aes.decrypt': '解密',
    'aes.output': '结果',
    'aes.copy': '复制结果',
    'db.title': '数据库隧道。',
    'db.sub': '注册的连接 URL 加密存储（独立 DB 密钥），隧道由 host 的 <code>vaulty-keeper serve</code> 提供；这里可注册、测试、生成客户端链接',
    'db.init-btn': '初始化数据库密钥',
    'db.add-card': '新增连接',
    'db.name': '名称',
    'db.name-ph': '例如 mysql-orders',
    'db.port': '隧道端口（可选，留空自动分配）',
    'db.url': '数据库 URL（含账号密码，浏览器内加密后传输）',
    'db.test-btn': '测试连接',
    'db.add-btn': '注册连接',
    'db.list-card': '连接',
    'db.regen-all': '全部重新生成',
    'db.regen-all-title': '所有连接的隧道 token 重新生成，旧链接立即失效',
    'db.th.name': '名称',
    'db.th.type': '类型',
    'db.th.state': '状态',
    'db.th.port': '端口',
    'db.th.actions': '操作',
    'settings.title': '设置。',
    'settings.sub': '本地密钥与 AES 配置管理。',
    'settings.snap-key': '快照密钥',
    'settings.init-snap': '生成快照密钥',
    'settings.sensitive-key': '敏感值密钥',
    'settings.init-sensitive': '生成敏感值密钥',
    'dialog.import.title': '导入配置快照',
    'dialog.import.env': '环境',
    'dialog.import.env-ph': '例如 prod',
    'dialog.import.appid': '应用 ID（必填）',
    'dialog.import.appid-ph': '例如 merdi-portal',
    'dialog.import.text': '粘贴 key=value 配置',
    'dialog.import.preview': '预览',
    'dialog.import.confirm': '确认导入',
    'dialog.entry.title': '编辑配置项',
    'dialog.entry.warning': '当前为敏感值，无法显示。输入新值将替换它；留空并保存不修改。',
    'dialog.entry.value': '值',
    'dialog.delete.title': '删除配置项',
    'dialog.delete.confirm': '确定要删除 <code id="delete-key"></code> 吗？此操作不可撤销。',
    'dialog.snap-delete.title': '删除快照',
    'dialog.snap-delete.confirm': '确定删除 <code id="snap-delete-name"></code> / <code id="snap-delete-appid"></code> 快照吗？此操作不可撤销。',
    'dialog.compare.title': '比较环境',
    'dialog.compare.sub': '将 <code id="compare-from"></code> 与以下环境对比：',
    'dialog.compare.target': '目标环境',
    'dialog.compare.start': '开始对比',
    'dialog.compare-result.title': '配置对比',
    'dialog.compare-result.copy': '复制差异',
    'dialog.compare-result.filter-ph': '过滤差异 key…',
    'dialog.multi.title': '横向对比多环境',
    'dialog.multi.sub': '选择 2 个以上快照，按 key 并排对比各环境取值。',
    'dialog.multi-result.title': '横向对比',
    'dialog.multi-result.report': '差异报告',
    'dialog.multi-result.copy-tsv': '复制为表格（Tab 分隔）',
    'dialog.multi-result.copy-csv': '复制 CSV（逗号分隔）',
    'dialog.multi-report.title': '差异报告',
    'dialog.multi-report.copy': '复制报告',
    'dialog.key-compare.title': '单 key 对比',
    'dialog.export.title': '导出配置',
    'dialog.export.sub': '将生成 <code id="export-name"></code> 的明文 key=value 文件，并在本机浏览器中下载。',
    'dialog.export.warning': '⚠ 明文包含敏感值，请仅在本机查看，不要转发。',
    'dialog.export.copy': '复制到剪贴板',
    'dialog.export.confirm': '确认导出',
    'dialog.reveal.title': '显示',
    'dialog.reveal.warning': '⚠ 将显示 <code id="reveal-key"></code> 的明文值，请仅在本机查看。',
    'dialog.reveal.aes-key': 'AES key',
    'dialog.reveal.iv': 'IV',
    'dialog.reveal.key-ph': '留空用系统敏感密钥',
    'dialog.reveal.hint': '值不是用系统敏感密钥加密时（如外部 AES 密文），手动填写 key/iv 后重试。',
    'dialog.reveal.show': '显示',
    'dialog.edit.title': '明文编辑',
    'dialog.edit.warning': '⚠ 将以明文编辑 <code id="edit-name"></code> 的全部条目，保存后重新加密。',
    'dialog.edit.text': '配置内容',
    'dialog.edit.save': '保存并重新加密',
    'dialog.summary.title': '安全 AI 摘要',
    'dialog.summary.text': '安全摘要仅基于可见值与本机元数据生成，不包含原始密钥。此能力在当前版本为占位，将在后续版本提供。',
    'dialog.db-connect.title': '连接信息',
    'dialog.db-show.title': '真实连接信息',
    'dialog.db-show.sub': '仅你的浏览器可见，请勿转发给他人。',
    'dialog.db-delete.title': '删除连接',
    'dialog.db-delete.confirm': '确定删除连接 <code id="db-delete-name"></code> 吗？隧道将随之消失，此操作不可撤销。',
    'dialog.db-regen.title': '重新生成隧道 token',
    'dialog.db-regen.confirm': '重新生成',
    'dialog.db-error.title': '注册连接失败',
    'common.cancel': '取消',
    'common.close': '关闭',
    'common.delete': '删除',
    'common.save': '保存',
    'common.confirm': '确认',
    'common.ok': '确定',
    'common.copy': '复制',
    'rel.just-now': '刚刚',
    'rel.min': '{n} 分钟前',
    'rel.hour': '{n} 小时前',
    'rel.day': '{n} 天前',
    'rail.empty': '暂无快照',
    'rail.expand': '点击展开',
    'rail.collapse': '点击折叠',
    'rail.items': '{n} 项',
    'rail.updated': '更新于 {t}',
    'rail.delete': '删除',
    'rail.delete-title': '删除快照',
    'view.aes': 'AES 加解密',
    'view.db': '数据库隧道',
    'view.settings': '设置',
    'breadcrumb.workbench': '配置工作台',
    'api.fail': '请求失败 ({status})',
    'api.auth-expired': '访问令牌无效或已过期——UI 每次启动都会重新生成。请用正在运行的 `vaulty-keeper ui` 打印的新 URL 重新打开。',
    'aes.error.key-iv': '请填写 key 和 iv。',
    'aes.error.input': '请填写输入内容。',
    'aes.copied': '已复制到剪贴板。',
    'aes.copy-failed': '复制失败。',
    'db.count': '{n} 条',
    'db.empty': '暂无连接，在左侧填写名称与数据库 URL 并注册；注册后 host 上的 vaulty-keeper serve 会自动为它开启一条本地隧道',
    'db.broken': '密钥不匹配',
    'db.broken-title': '无法用当前密钥解密',
    'db.state.on': '开',
    'db.state.off': '关',
    'db.test': '测试连接',
    'db.test-title': '用注册的真实 URL 直连测试（不走隧道）',
    'db.connect': '连接信息',
    'db.regen': '重新生成',
    'db.regen-title': '重新生成隧道 token，旧链接立即失效',
    'db.tunnel-on': '开启隧道',
    'db.tunnel-on-title': '开启隧道，serve 约 2 秒内生效',
    'db.tunnel-off': '关闭隧道',
    'db.tunnel-off-title': '关闭隧道，端口停止监听',
    'db.show-url': '查看URL',
    'db.rm': '删除',
    'db.testing': '测试中…',
    'db.pubkey-fail': '无法获取数据库 URL 加密公钥',
    'db.init-confirm': '初始化数据库密钥？（仅当此前从未初始化时才需要；已存在时不要用强制覆盖，否则已有连接将无法解密）',
    'db.init-title': '初始化数据库密钥',
    'db.init-ok': '初始化',
    'db.init-overwrite': '密钥已存在，确认强制覆盖？这会使所有已注册连接无法解密，必须重新注册',
    'db.init-overwrite-title': '强制覆盖密钥',
    'db.init-overwrite-ok': '覆盖',
    'db.add-required': '名称与数据库 URL 必填',
    'db.test-url-empty': '请先填写数据库 URL',
    'db.test-ok': '✅ 连接成功（{type}',
    'db.test-fail': '❌ {err}',
    'db.regen-done-note': '已重新生成新 token，旧链接已立即失效',
    'db.connect.raw-label': '原始隧道链接（AI / 其他工具可据此自行转换）',
    'db.connect.footer': 'token 是 bridge token，不是真实数据库密码。',
    'db.regen-confirm-one': '确定重新生成 <code>{name}</code> 的隧道 token 吗？旧链接将立即失效，此操作不可撤销（全局 bridge token 不受影响）。',
    'db.regen-confirm-all': '确定重新生成所有连接的隧道 token 吗？所有旧链接将立即失效，此操作不可撤销（全局 bridge token 不受影响）。',
    'db.regen-done': '已重新生成 {n} 个连接的 token，旧链接已失效',
    'db.regen-none': '没有可重新生成 token 的连接',
    'db.off-confirm': '确定关闭 {name} 的隧道吗？端口将停止监听，现有连接会断开（可随时重新开启）。',
    'db.off-title': '关闭隧道',
    'db.off-ok': '关闭',
    'settings.snap-ok': '快照密钥可用（Keychain 或环境变量）。',
    'settings.snap-missing': '快照密钥不可用。点击下方按钮生成本机密钥。',
    'settings.sensitive-ok': '敏感值密钥可用（Keychain 或环境变量）。敏感值用它加密，显示时用它解密。',
    'settings.sensitive-missing': '敏感值密钥不可用。点击下方按钮生成本机密钥。',
    'settings.snap-created': '快照密钥已生成。',
    'settings.sensitive-created': '敏感值密钥已生成。',
    'snap.no-selection': '未选择快照',
    'snap.empty-hint': '暂无快照，点击左侧「导入配置快照」开始。',
    'snap.context': '{total} 项配置 · {sensitive} 个敏感项 · {time}更新',
    'snap.hero-title': '检查 {name} 的配置状态。',
    'snap.hero-title-simple': '检查配置状态。',
    'table.no-match': '没有匹配的配置项',
    'table.empty': '暂无配置，点击左侧「导入配置快照」开始。',
    'table.chars': '{n} 字符',
    'table.reveal': '显示',
    'table.compare-key': '对比此 key',
    'table.compare-key-title': '查看此 key 在所有快照中的取值',
    'common.no-token': '未携带访问令牌：请用启动时打印的完整 URL（含 ?t=...）打开，否则导入/修改/导出/解密等操作不可用。',
    'import.dupe': '已存在快照：{name} ({appid})，共 {total} 项配置。导入会失败，请换用其他环境或应用 ID。',
    'import.error.paste': '请先粘贴配置内容。',
    'import.error.env': '请填写环境。',
    'import.error.appid': '请填写应用 ID。',
    'import.error.dupe': '快照 {name} ({appid}) 已存在，无法重复导入。',
    'import.tag-sensitive': '敏感',
    'entry.title': '编辑配置项',
    'entry.title-sensitive': '替换敏感值',
    'entry.ph': '输入新值以替换…',
    'compare.no-other': '没有可对比的其他快照。',
    'compare.error.target': '请选择目标环境。',
    'compare.added': '+{n} 新增',
    'compare.removed': '-{n} 删除',
    'compare.changed': '~{n} 变更',
    'compare.no-changes': '0 处变更',
    'compare.identical': '两个环境配置一致。',
    'compare.sensitive': '敏感值 · {n} 字符',
    'compare.fp': ' · 指纹 {fp}',
    'compare.copied': '已复制',
    'compare.copy-failed': '复制失败',
    'multi.error': '请至少选择 2 个快照。',
    'multi.header-title': '{n} 项 · {s} 个敏感',
    'multi.sensitive': '敏感值·{n}字符{fp}',
    'key-compare.title': '单 key 对比：{key}',
    'key-compare.title-appid': '单 key 对比：{key}（同 appid {appid}）',
    'key-compare.loading': '加载中…',
    'key-compare.empty': '没有快照包含此 key。',
    'report.title': '横向对比报告',
    'report.refs': '对比环境：{refs}',
    'report.stats': '统计：共 {total} 个 key，{diff} 个存在差异，{same} 个全部一致；敏感值差异 {sensitiveDiff} 个',
    'report.all-same': '所有环境配置完全一致。',
    'report.diff-fields': '差异字段：',
    'export.no-selection': '当前没有已选快照。',
    'reveal.empty': '(空)',
    'reveal.show': '显示',
  }
};

const LANG_KEY = 'vk-lang';
let LANG = 'en';
try { LANG = localStorage.getItem(LANG_KEY) || 'en'; } catch (_) { /* storage unavailable */ }
if (LANG !== 'en' && LANG !== 'zh') LANG = 'en';

// t resolves a key in the current language, with optional {var} substitution.
function t(key, vars) {
  const dict = I18N[LANG] || I18N.en;
  let s = dict[key] != null ? dict[key] : (I18N.en[key] != null ? I18N.en[key] : key);
  if (vars) {
    for (const k of Object.keys(vars)) {
      s = s.split('{' + k + '}').join(String(vars[k]));
    }
  }
  return s;
}

// applyI18n fills every data-i18n* element from the dictionary.
function applyI18n() {
  document.documentElement.lang = LANG === 'zh' ? 'zh-CN' : 'en';
  document.title = t('app.title');
  document.querySelectorAll('[data-i18n]').forEach((el) => {
    // Never overwrite containers with element children (labels wrapping
    // inputs etc.) — those must put data-i18n on an inner span instead.
    if ([...el.childNodes].some((n) => n.nodeType === 1)) return;
    const v = t(el.dataset.i18n);
    if (v != null) el.textContent = v;
  });
  document.querySelectorAll('[data-i18n-html]').forEach((el) => {
    const v = t(el.dataset.i18nHtml);
    if (v != null) el.innerHTML = v;
  });
  document.querySelectorAll('[data-i18n-ph]').forEach((el) => {
    const v = t(el.dataset.i18nPh);
    if (v != null) el.setAttribute('placeholder', v);
  });
  document.querySelectorAll('[data-i18n-aria]').forEach((el) => {
    const v = t(el.dataset.i18nAria);
    if (v != null) el.setAttribute('aria-label', v);
  });
  document.querySelectorAll('[data-i18n-title]').forEach((el) => {
    const v = t(el.dataset.i18nTitle);
    if (v != null) el.setAttribute('title', v);
  });
  document.querySelectorAll('#lang-toggle [data-lang]').forEach((b) => {
    b.classList.toggle('active', b.dataset.lang === LANG);
  });
}

// setLang switches the language, persists it, and re-renders every surface.
// The preference is stored in localStorage (this browser) and pushed to the
// shared ~/.vaulty/prefs.json so CLI runs pick it up too.
function setLang(lang) {
  if (lang !== 'en' && lang !== 'zh') lang = 'en';
  LANG = lang;
  try { localStorage.setItem(LANG_KEY, lang); } catch (_) { /* storage unavailable */ }
  try {
    api('/api/prefs', jsonOptions('PUT', { lang })).catch(() => {});
  } catch (_) { /* file write is best-effort */ }
  document.querySelectorAll('dialog[open]').forEach((d) => d.close());
  applyI18n();
  renderRail();
  if (state.view === 'snapshots') { renderContext(); renderTable(); }
  if (state.view === 'db') loadDB();
  if (state.view === 'settings') loadSettings();
}

// ---- api ----

async function api(path, options = {}) {
  const method = options.method || 'GET';
  if (TOKEN && method !== 'GET') {
    options = { ...options, headers: { ...(options.headers || {}), 'X-Auth-Token': TOKEN } };
  }
  const res = await fetch(path, options);
  if (res.status === 204) return null;
  const type = res.headers.get('content-type') || '';
  if (!res.ok) {
    if (res.status === 401) throw new Error(t('api.auth-expired'));
    let message = t('api.fail', { status: res.status });
    try {
      if (type.includes('application/json')) {
        const body = await res.json();
        if (body && body.error && body.error.message) message = body.error.message;
      } else {
        const text = await res.text();
        if (text) message = text;
      }
    } catch (_) { /* keep default message */ }
    throw new Error(message);
  }
  if (type.includes('application/json')) return res.json();
  return res;
}

function jsonOptions(method, body) {
  return { method, headers: { 'Content-Type': 'application/json' }, body: JSON.stringify(body) };
}

function escapeHtml(s) {
  return String(s).replace(/[&<>"']/g, (c) => ({ '&': '&amp;', '<': '&lt;', '>': '&gt;', '"': '&quot;', "'": '&#39;' }[c]));
}

function relTime(iso) {
  const dt = new Date(iso);
  if (isNaN(dt.getTime())) return '';
  const diff = Date.now() - dt.getTime();
  const min = Math.floor(diff / 60000);
  if (min < 1) return t('rel.just-now');
  if (min < 60) return t('rel.min', { n: min });
  const hours = Math.floor(min / 60);
  if (hours < 24) return t('rel.hour', { n: hours });
  const days = Math.floor(hours / 24);
  if (days < 7) return t('rel.day', { n: days });
  return dt.toLocaleDateString(LANG === 'zh' ? 'zh-CN' : 'en-US');
}

// ---- render: rail ----

function renderRail() {
  const list = $('snapshot-list');
  list.textContent = '';
  if (!state.snapshots.length) {
    const d = document.createElement('div');
    d.className = 'item';
    d.textContent = t('rail.empty');
    list.appendChild(d);
    return;
  }
  const groups = new Map();
  for (const s of state.snapshots) {
    if (!groups.has(s.name)) groups.set(s.name, []);
    groups.get(s.name).push(s);
  }
  for (const [env, refs] of groups) {
    const g = document.createElement('div');
    g.className = 'rail-group' + (state.collapsedEnv[env] ? ' collapsed' : '');
    const gTitle = document.createElement('div');
    gTitle.className = 'group-title';
    gTitle.title = state.collapsedEnv[env] ? t('rail.expand') : t('rail.collapse');
    const arrow = document.createElement('span');
    arrow.className = 'arrow';
    arrow.textContent = state.collapsedEnv[env] ? '▸' : '▾';
    const envName = document.createElement('span');
    envName.className = 'env-name';
    envName.textContent = env;
    const gCount = document.createElement('span');
    gCount.className = 'g-count';
    gCount.textContent = `${refs.length}`;
    gTitle.appendChild(arrow);
    gTitle.appendChild(envName);
    gTitle.appendChild(gCount);
    gTitle.addEventListener('click', () => {
      state.collapsedEnv[env] = !state.collapsedEnv[env];
      renderRail();
    });
    g.appendChild(gTitle);
    for (const s of refs) {
      const d = document.createElement('div');
      d.className = 'item' + (s.name === state.active && (s.app_id || '') === (state.activeAppID || '') ? ' active' : '');
      const top = document.createElement('div');
      top.className = 'rail-row';
      const name = document.createElement('span');
      name.textContent = s.app_id ? s.app_id : s.name;
      const count = document.createElement('span');
      count.className = 'count';
      count.textContent = t('rail.items', { n: s.total });
      top.appendChild(name);
      top.appendChild(count);
      d.appendChild(top);
      const sub = document.createElement('div');
      sub.className = 'rail-sub';
      sub.textContent = relTime(s.captured_at) ? t('rail.updated', { t: relTime(s.captured_at) }) : '';
      d.appendChild(sub);
      const del = document.createElement('button');
      del.type = 'button';
      del.className = 'rail-del';
      del.textContent = t('rail.delete');
      del.title = t('rail.delete-title');
      del.addEventListener('click', (e) => {
        e.stopPropagation();
        openDeleteSnapshot(s);
      });
      d.appendChild(del);
      d.addEventListener('click', () => selectSnapshot(s.name, s.app_id));
      g.appendChild(d);
    }
    list.appendChild(g);
  }
}

function refLabel(name, appID) {
  return appID ? `${name} (${appID})` : name;
}

// ---- view switching ----

function switchView(name) {
  state.view = name;
  if (name !== 'snapshots') {
    state.active = null;
    state.activeAppID = '';
    state.snapshot = null;
    renderRail();
    renderContext();
  }
  for (const v of ['snapshots', 'aes', 'db', 'settings']) {
    const el = $(`view-${v}`);
    if (el) el.hidden = v !== name;
    const nav = $(`nav-${v}`);
    if (nav) nav.classList.toggle('active', v === name);
  }
  const titles = { aes: t('view.aes'), db: t('view.db'), settings: t('view.settings') };
  $('breadcrumb').innerHTML = name === 'snapshots'
    ? `<b>${t('breadcrumb.workbench')}</b>`
    : `<b>${titles[name] || name}</b>`;
  if (name === 'db') loadDB();
  if (name === 'settings') loadSettings();
}

// ---- AES tools ----

async function runAES(op) {
  const key = $('aes-key').value.trim();
  const iv = $('aes-iv').value.trim();
  const text = $('aes-input').value;
  if (!key || !iv) { showAESError(t('aes.error.key-iv')); return; }
  if (!text) { showAESError(t('aes.error.input')); return; }
  try {
    const data = await api('/api/aes/transform', jsonOptions('POST', { op, key, iv, text }));
    $('aes-output').value = data.result;
    showAESError('');
  } catch (err) { showAESError(err.message); }
}

function showAESError(msg) {
  $('aes-error').textContent = msg;
  $('aes-error').hidden = !msg;
}

function copyAESOutput() {
  const text = $('aes-output').value;
  if (!text) return;
  copyText(text).then(() => showAESError(t('aes.copied'))).catch(() => showAESError(t('aes.copy-failed')));
}

// ---- database tunnels ----

let dbConns = [];

function showDBError(msg) {
  const el = $('db-error');
  el.textContent = msg;
  el.hidden = !msg;
}

async function loadDB() {
  const initBtn = $('db-init-btn');
  try {
    const ks = await api('/api/db/key');
    initBtn.hidden = ks.available;
  } catch (_) {
    initBtn.hidden = false;
  }
  try {
    const res = await api('/api/db/list');
    dbConns = res.connections || [];
    renderDBTable();
  } catch (e) {
    showDBError(e.message);
  }
}

function renderDBTable() {
  $('db-count').textContent = dbConns.length ? t('db.count', { n: dbConns.length }) : '';
  const body = $('db-body');
  body.innerHTML = '';
  const empty = $('db-empty');
  if (!dbConns.length) {
    empty.hidden = false;
    empty.textContent = t('db.empty');
  } else {
    empty.hidden = true;
  }
  for (const c of dbConns) {
    const tr = document.createElement('tr');
    tr.dataset.name = c.name;
    const typeCell = c.broken
      ? `<td><span class="warn-badge" title="${escapeHtml(t('db.broken-title'))}">${escapeHtml(t('db.broken'))}</span></td>`
      : `<td>${escapeHtml(c.type)}</td>`;
    const stateCell = c.broken
      ? `<td></td>`
      : c.disabled
        ? `<td><span class="db-state off">${escapeHtml(t('db.state.off'))}</span></td>`
        : `<td><span class="db-state on">${escapeHtml(t('db.state.on'))}</span></td>`;
    const actions = c.broken
      ? `<td class="row-actions"><button class="row-del" type="button" data-db-action="rm" data-name="${escapeHtml(c.name)}">${escapeHtml(t('db.rm'))}</button></td>`
      : `<td class="row-actions">
          <button class="ghost" type="button" data-db-action="test" data-name="${escapeHtml(c.name)}" title="${escapeHtml(t('db.test-title'))}">${escapeHtml(t('db.test'))}</button>
          <button class="ghost" type="button" data-db-action="connect" data-name="${escapeHtml(c.name)}">${escapeHtml(t('db.connect'))}</button>
          <button class="ghost" type="button" data-db-action="regen" data-name="${escapeHtml(c.name)}" title="${escapeHtml(t('db.regen-title'))}">${escapeHtml(t('db.regen'))}</button>
          ${c.disabled
            ? `<button class="ghost" type="button" data-db-action="on" data-name="${escapeHtml(c.name)}" title="${escapeHtml(t('db.tunnel-on-title'))}">${escapeHtml(t('db.tunnel-on'))}</button>`
            : `<button class="ghost" type="button" data-db-action="off" data-name="${escapeHtml(c.name)}" title="${escapeHtml(t('db.tunnel-off-title'))}">${escapeHtml(t('db.tunnel-off'))}</button>`}
          <button class="ghost" type="button" data-db-action="show" data-name="${escapeHtml(c.name)}" hidden>${escapeHtml(t('db.show-url'))}</button>
          <button class="row-del" type="button" data-db-action="rm" data-name="${escapeHtml(c.name)}">${escapeHtml(t('db.rm'))}</button>
        </td>`;
    tr.innerHTML = `<td><b>${escapeHtml(c.name)}</b></td>${typeCell}${stateCell}<td>:${c.port}</td>${actions}`;
    body.appendChild(tr);
  }
  // only offer "View URL" when plaintext endpoints are enabled
  api('/api/config').then((cfg) => {
    if (cfg.allow_plaintext) {
      body.querySelectorAll('[data-db-action="show"]').forEach((b) => (b.hidden = false));
    }
  }).catch(() => {});
}

// URL encryption: the browser derives a fresh AES-GCM key from the UI's
// per-process ECDH public key, so the database URL never crosses the wire as
// plaintext. The matching private key lives only in the ui process memory.
let dbPubKey = '';

async function fetchDBPubKey() {
  if (dbPubKey) return dbPubKey;
  const res = await api('/api/db/pubkey');
  if (!res || !res.pub) throw new Error(t('db.pubkey-fail'));
  dbPubKey = res.pub;
  return dbPubKey;
}

function b64ToBuf(b64) {
  const bin = atob(b64);
  const u = new Uint8Array(bin.length);
  for (let i = 0; i < bin.length; i++) u[i] = bin.charCodeAt(i);
  return u.buffer;
}

function bufToB64(buf) {
  const u = new Uint8Array(buf);
  let bin = '';
  for (let i = 0; i < u.length; i++) bin += String.fromCharCode(u[i]);
  return btoa(bin);
}

async function encryptDBURL(url) {
  const pubB64 = await fetchDBPubKey();
  const serverPub = await crypto.subtle.importKey('raw', b64ToBuf(pubB64), { name: 'ECDH', namedCurve: 'P-256' }, false, []);
  const eph = await crypto.subtle.generateKey({ name: 'ECDH', namedCurve: 'P-256' }, true, ['deriveKey']);
  const aesKey = await crypto.subtle.deriveKey({ name: 'ECDH', public: serverPub }, eph.privateKey, { name: 'AES-GCM', length: 256 }, false, ['encrypt']);
  const iv = crypto.getRandomValues(new Uint8Array(12));
  const ct = await crypto.subtle.encrypt({ name: 'AES-GCM', iv }, aesKey, new TextEncoder().encode(url));
  const ephRaw = await crypto.subtle.exportKey('raw', eph.publicKey);
  return { eph: bufToB64(ephRaw), iv: bufToB64(iv), ct: bufToB64(ct) };
}

function showDBFormResult(msg, cls) {
  const el = $('db-form-result');
  el.textContent = msg || '';
  el.className = 'db-form-result ' + (cls || '');
  el.hidden = !msg;
}

function setDBFormBusy(busy) {
  $('db-add-btn').disabled = busy;
  $('db-test-btn').disabled = busy;
}

async function dbInit() {
  const ok = await uiConfirm(
    t('db.init-confirm'),
    { title: t('db.init-title'), okText: t('db.init-ok') });
  if (!ok) return;
  try {
    await api('/api/db/init', jsonOptions('POST', { force: false }));
    showDBError('');
    loadDB();
  } catch (e) {
    if (e.message.includes('already exists')) {
      const overwrite = await uiConfirm(
        t('db.init-overwrite'),
        { title: t('db.init-overwrite-title'), okText: t('db.init-overwrite-ok'), danger: true });
      if (overwrite) {
        try {
          await api('/api/db/init', jsonOptions('POST', { force: true }));
          loadDB();
        } catch (e2) {
          showDBError(e2.message);
        }
      }
    } else {
      showDBError(e.message);
    }
  }
}

function showDBAddError(msg) {
  openDialog('db-error-dialog');
  const el = $('db-error-msg');
  el.textContent = msg || '';
  el.hidden = !msg;
}

async function dbAdd() {
  const name = $('db-name').value.trim();
  const url = $('db-url').value.trim();
  const port = parseInt($('db-port').value || '0', 10) || 0;
  if (!name || !url) {
    showDBAddError(t('db.add-required'));
    return;
  }
  setDBFormBusy(true);
  try {
    const url_enc = await encryptDBURL(url);
    await api('/api/db/connections', jsonOptions('POST', { name, url_enc, port }));
    $('db-name').value = '';
    $('db-url').value = '';
    $('db-port').value = '';
    showDBError('');
    showDBFormResult('', '');
    loadDB();
  } catch (e) {
    showDBAddError(e.message);
  } finally {
    setDBFormBusy(false);
  }
}

async function dbTestURL() {
  const url = $('db-url').value.trim();
  if (!url) {
    showDBFormResult(t('db.test-url-empty'), 'fail');
    return;
  }
  setDBFormBusy(true);
  try {
    const url_enc = await encryptDBURL(url);
    const res = await api('/api/db/test-url', jsonOptions('POST', { url_enc }));
    if (res.ok) {
      let msg = t('db.test-ok', { type: res.type });
      if (res.user) msg += `，user=${res.user}`;
      if (res.db) msg += `，db=${res.db}`;
      msg += '）';
      showDBFormResult(msg, 'ok');
    } else {
      showDBFormResult(t('db.test-fail', { err: res.error }), 'fail');
    }
  } catch (e) {
    showDBFormResult(t('db.test-fail', { err: e.message }), 'fail');
  } finally {
    setDBFormBusy(false);
  }
}

function findDBRow(name) {
  return [...$('db-body').rows].find((r) => r.dataset.name === name);
}

function removeDBResultRow(row) {
  const next = row.nextElementSibling;
  if (next && next.classList.contains('db-result-row')) next.remove();
}

function insertDBResultRow(row, ok, text) {
  removeDBResultRow(row);
  const tr = document.createElement('tr');
  tr.className = 'db-result-row';
  const td = document.createElement('td');
  td.colSpan = 5;
  td.className = ok ? 'db-result ok' : 'db-result fail';
  td.textContent = text;
  tr.appendChild(td);
  row.after(tr);
  setTimeout(() => { if (tr.parentNode) tr.remove(); }, 10000);
}

async function dbTest(name) {
  const row = findDBRow(name);
  const btn = row && row.querySelector('[data-db-action="test"]');
  if (btn) {
    btn.disabled = true;
    btn.textContent = t('db.testing');
  }
  let ok = false;
  let text;
  try {
    const res = await api('/api/db/test', jsonOptions('POST', { name }));
    ok = res.ok;
    let parts = [`${res.name} (${res.type}) :${res.port}`];
    if (res.user) parts.push(`user=${res.user}`);
    if (res.db) parts.push(`db=${res.db}`);
    text = ok ? '✅ ' + parts.join(' ') : '❌ ' + parts.join(' ') + ' — ' + res.error;
  } catch (e) {
    text = '❌ ' + e.message;
  }
  if (row) insertDBResultRow(row, ok, text);
  if (btn) {
    btn.disabled = false;
    btn.textContent = t('db.test');
  }
}

function showDBConnectDialog(res, regenerated) {
  $('db-connect-head').innerHTML =
    `<b>${escapeHtml(res.name)}</b><span class="tag">${escapeHtml(res.type)}</span><span class="port">:${res.port}</span>`;
  const note = $('db-connect-note');
  if (regenerated) {
    note.textContent = t('db.regen-done-note');
    note.hidden = false;
  } else if (res.note) {
    note.textContent = res.note;
    note.hidden = false;
  } else {
    note.hidden = true;
  }
  let html = '';
  if (res.raw) {
    html += `<div class="db-line"><div class="db-line-label">${escapeHtml(t('db.connect.raw-label'))}</div>` +
      `<pre><code>${escapeHtml(res.raw)}</code></pre><button type="button" class="copy-btn" data-copy>${escapeHtml(t('common.copy'))}</button></div>`;
  }
  for (const cl of res.clients) {
    html += `<div class="db-line"><div class="db-line-label">${escapeHtml(cl.label)}</div>` +
      `<pre><code>${escapeHtml(cl.line)}</code></pre><button type="button" class="copy-btn" data-copy>${escapeHtml(t('common.copy'))}</button></div>`;
  }
  html += `<div class="db-connect-footer">${escapeHtml(t('db.connect.footer'))}</div>`;
  $('db-connect-body').innerHTML = html;
  openDialog('db-connect-dialog');
}

async function dbConnectInfo(name) {
  try {
    const res = await api(`/api/db/connect?name=${encodeURIComponent(name)}`);
    showDBConnectDialog(res);
  } catch (e) {
    showDBError(e.message);
  }
}

async function dbShow(name) {
  try {
    const res = await api('/api/db/show', jsonOptions('POST', { name }));
    $('db-show-url').textContent = res.url;
    $('db-show-copy-btn').textContent = t('common.copy');
    openDialog('db-show-dialog');
  } catch (e) {
    showDBError(e.message);
  }
}

let deletingDBConn = null;

function dbRemove(name) {
  deletingDBConn = name;
  $('db-delete-name').textContent = name;
  openDialog('db-delete-dialog');
}

async function confirmDBDelete() {
  if (!deletingDBConn) return;
  const name = deletingDBConn;
  try {
    await api(`/api/db/connections/${encodeURIComponent(name)}`, { method: 'DELETE' });
    closeDialog('db-delete-dialog');
    deletingDBConn = null;
    loadDB();
  } catch (err) {
    dialogError('db-delete-dialog', err.message);
  }
}

let regenTarget = null; // { all: true } or { name }

function dbRegen(name) {
  regenTarget = { name };
  $('db-regen-desc').innerHTML =
    t('db.regen-confirm-one', { name: escapeHtml(name) });
  openDialog('db-regen-dialog');
}

function dbRegenAll() {
  regenTarget = { all: true };
  $('db-regen-desc').textContent = t('db.regen-confirm-all');
  openDialog('db-regen-dialog');
}

async function confirmDBRegen() {
  if (!regenTarget) return;
  try {
    if (regenTarget.all) {
      const res = await api('/api/db/regen', jsonOptions('POST', { all: true }));
      closeDialog('db-regen-dialog');
      regenTarget = null;
      const n = (res.regenerated || []).length;
      showDBError(n ? t('db.regen-done', { n }) : t('db.regen-none'));
      loadDB();
    } else {
      const res = await api('/api/db/regen', jsonOptions('POST', { name: regenTarget.name }));
      closeDialog('db-regen-dialog');
      regenTarget = null;
      showDBConnectDialog(res, true);
    }
  } catch (err) {
    dialogError('db-regen-dialog', err.message);
  }
}

async function dbSetTunnel(name, enabled) {
  if (!enabled) {
    const ok = await uiConfirm(
      t('db.off-confirm', { name }),
      { title: t('db.off-title'), okText: t('db.off-ok'), danger: true });
    if (!ok) return;
  }
  try {
    await api('/api/db/tunnel', jsonOptions('POST', { name, enabled }));
    loadDB();
  } catch (e) {
    showDBError(e.message);
  }
}

// ---- settings ----

async function loadSettings() {
  const status = $('key-status');
  const initBtn = $('init-key-btn');
  try {
    const ks = await api('/api/key');
    if (ks.available) {
      status.textContent = t('settings.snap-ok');
      status.className = 'status-line ok';
      initBtn.hidden = true;
    } else {
      throw new Error('unavailable');
    }
  } catch (_) {
    status.textContent = t('settings.snap-missing');
    status.className = 'status-line warn';
    initBtn.hidden = false;
  }
  const sStatus = $('sensitive-key-status');
  const sInit = $('init-sensitive-btn');
  try {
    const sk = await api('/api/sensitive/key');
    if (sk.available) {
      sStatus.textContent = t('settings.sensitive-ok');
      sStatus.className = 'status-line ok';
      sInit.hidden = true;
    } else {
      throw new Error('unavailable');
    }
  } catch (_) {
    sStatus.textContent = t('settings.sensitive-missing');
    sStatus.className = 'status-line warn';
    sInit.hidden = false;
  }
}

async function initKey() {
  try {
    await api('/api/init', jsonOptions('POST', { force: false }));
    $('init-key-btn').hidden = true;
    $('key-status').textContent = t('settings.snap-created');
    $('key-status').className = 'status-line ok';
  } catch (err) { showSettingsError(err.message); }
}

async function initSensitiveKey() {
  try {
    await api('/api/sensitive/init', jsonOptions('POST', { force: false }));
    $('init-sensitive-btn').hidden = true;
    $('sensitive-key-status').textContent = t('settings.sensitive-created');
    $('sensitive-key-status').className = 'status-line ok';
  } catch (err) { showSettingsError(err.message); }
}

function showSettingsError(msg) {
  $('settings-error').textContent = msg;
  $('settings-error').hidden = !msg;
}

// ---- render: context / hero ----

function renderContext() {
  const s = state.snapshots.find((x) => x.name === state.active && (x.app_id || '') === (state.activeAppID || ''));
  const secure = $('snapshot-context').querySelector('.secure');
  if (!s) {
    $('context-name').textContent = t('snap.no-selection');
    $('context-meta').textContent = t('snap.empty-hint');
    $('snapshot-context').querySelector('.env').textContent = '✦';
    secure.hidden = true;
    $('breadcrumb').innerHTML = `<b>${t('breadcrumb.workbench')}</b>`;
    $('hero-title').textContent = t('snap.hero-title-simple');
    return;
  }
  $('context-name').textContent = state.active + (state.activeAppID ? ' / ' + state.activeAppID : '');
  $('context-meta').textContent = t('snap.context', { total: s.total, sensitive: s.sensitive, time: relTime(s.captured_at) });
  $('snapshot-context').querySelector('.env').textContent = s.name.slice(0, 1);
  secure.hidden = false;
  $('breadcrumb').innerHTML = `<b>${t('breadcrumb.workbench')}</b><span class="sep">/</span>${escapeHtml(s.name)}${s.app_id ? `<span class="sep">/</span>${escapeHtml(s.app_id)}` : ''}`;
  $('hero-title').textContent = t('snap.hero-title', { name: s.name + (s.app_id ? ` (${s.app_id})` : '') });
}

// ---- render: table ----

function currentItems() {
  if (!state.snapshot) return [];
  const q = $('search-input').value.trim().toLowerCase();
  const items = [...state.snapshot.items];
  if (!q) return items;
  return items.filter((it) => {
    if (it.key.toLowerCase().includes(q)) return true;
    return !it.sensitive && it.value && it.value.toLowerCase().includes(q);
  });
}

function renderTable() {
  const body = $('config-body');
  body.textContent = '';
  const items = currentItems();
  $('config-count').textContent = state.snapshot ? `${items.length} / ${state.snapshot.items.length}` : '';
  if (!items.length) {
    const row = document.createElement('tr');
    const td = document.createElement('td');
    td.colSpan = 2;
    const span = document.createElement('span');
    span.className = 'empty';
    span.textContent = state.snapshot ? t('table.no-match') : t('table.empty');
    td.appendChild(span);
    row.appendChild(td);
    body.appendChild(row);
    return;
  }
  for (const it of items) {
    const tr = document.createElement('tr');
    const tdKey = document.createElement('td');
    tdKey.textContent = it.key;
    const tdVal = document.createElement('td');
    if (it.sensitive) {
      tdVal.className = 'masked';
      tdVal.textContent = MASK;
      const i = document.createElement('i');
      i.textContent = t('table.chars', { n: it.length });
      tdVal.appendChild(i);
      const btn = document.createElement('button');
      btn.type = 'button';
      btn.className = 'reveal-btn';
      btn.textContent = t('table.reveal');
      tdVal.appendChild(btn);
      btn.addEventListener('click', (e) => {
        e.stopPropagation();
        openReveal(it);
      });
    } else {
      tdVal.textContent = it.value || '';
    }
    const cmpBtn = document.createElement('button');
    cmpBtn.type = 'button';
    cmpBtn.className = 'reveal-btn';
    cmpBtn.textContent = t('table.compare-key');
    cmpBtn.title = t('table.compare-key-title');
    tdVal.appendChild(cmpBtn);
    cmpBtn.addEventListener('click', (e) => {
      e.stopPropagation();
      openKeyCompare(it.key);
    });
    tr.appendChild(tdKey);
    tr.appendChild(tdVal);
    tr.addEventListener('click', () => openEntry(it));
    body.appendChild(tr);
  }
}

// ---- snapshot loading ----

function showError(msg) {
  $('error-region').textContent = msg;
  $('error-region').hidden = false;
}

function clearError() {
  $('error-region').textContent = '';
  $('error-region').hidden = true;
}

async function loadSnapshot(name, appID) {
  const requested = name;
  const requestedAppID = appID || '';
  state.active = name;
  state.activeAppID = requestedAppID;
  state.compare = null;
  state.snapshot = null;
  $('compare-result-dialog').close();
  $('search-input').value = '';
  clearError();
  renderRail();
  renderContext();
  const q = state.activeAppID ? `?appid=${encodeURIComponent(state.activeAppID)}` : '';
  try {
    const data = await api(`/api/snapshots/${encodeURIComponent(name)}${q}`);
    if (state.active !== requested || state.activeAppID !== requestedAppID) return;
    state.snapshot = { name: data.name, items: data.items };
  } catch (err) {
    if (state.active !== requested || state.activeAppID !== requestedAppID) return;
    showError(err.message);
  }
  renderTable();
}

function selectSnapshot(name, appID) {
  switchView('snapshots');
  loadSnapshot(name, appID);
}

// Reloads the active snapshot's data and the rail summaries without resetting
// UI state (search text, scroll position, compare panel).
async function reloadSnapshotData() {
  const reqName = state.active;
  if (!reqName) return;
  const q = state.activeAppID ? `?appid=${encodeURIComponent(state.activeAppID)}` : '';
  try {
    const data = await api(`/api/snapshots/${encodeURIComponent(reqName)}${q}`);
    state.snapshot = { name: data.name, items: data.items };
  } catch (err) {
    showError(err.message);
  }
  try {
    const list = await api('/api/snapshots');
    state.snapshots = list.snapshots;
  } catch (_) { /* keep current list */ }
  renderRail();
  renderContext();
  renderTable();
}

async function refreshSnapshots(selectRef) {
  try {
    const data = await api('/api/snapshots');
    state.snapshots = data.snapshots;
  } catch (err) {
    showError(err.message);
  }
  renderRail();
  const target = selectRef || (state.active ? { name: state.active, app_id: state.activeAppID } : state.snapshots[0]);
  if (target && target.name) await loadSnapshot(target.name, target.app_id);
  else {
    state.active = null;
    state.activeAppID = '';
    renderContext();
    renderTable();
  }
}

// ---- dialogs ----

function openDialog(id) {
  clearDialogError(id);
  $(id).showModal();
}

function closeDialog(id) {
  $(id).close();
}

function dialogError(id, msg) {
  const el = $(id).querySelector('.error');
  if (!el) return;
  el.textContent = msg;
  el.hidden = false;
}

function clearDialogError(id) {
  const el = $(id).querySelector('.error');
  if (!el) return;
  el.textContent = '';
  el.hidden = true;
}

// uiConfirm shows a modal confirmation dialog instead of window.confirm and
// resolves with true only when the user clicks the confirm button (ESC or
// cancel resolve with false).
function uiConfirm(message, opts = {}) {
  return new Promise((resolve) => {
    const dlg = $('confirm-dialog');
    $('confirm-title').textContent = opts.title || t('common.confirm');
    $('confirm-msg').textContent = message;
    const okBtn = $('confirm-ok-btn');
    okBtn.textContent = opts.okText || t('common.ok');
    okBtn.className = opts.danger ? 'danger' : 'primary';
    let settled = false;
    const finish = (val) => {
      if (settled) return;
      settled = true;
      dlg.removeEventListener('close', onClose);
      okBtn.onclick = null;
      resolve(val);
    };
    const onClose = () => finish(false);
    dlg.addEventListener('close', onClose);
    okBtn.onclick = () => { finish(true); dlg.close(); };
    openDialog('confirm-dialog');
  });
}

// ---- import ----

function openImport() {
  $('import-name').value = '';
  $('import-appid').value = '';
  $('import-text').value = '';
  $('import-preview').hidden = true;
  $('import-preview').textContent = '';
  $('import-dupe').hidden = true;
  $('import-confirm-btn').hidden = true;
  $('import-preview-btn').hidden = false;
  openDialog('import-dialog');
  $('import-name').focus();
}

function checkImportDuplicate() {
  const env = $('import-name').value.trim();
  const appid = $('import-appid').value.trim();
  const el = $('import-dupe');
  if (!env || !appid) {
    el.hidden = true;
    return;
  }
  const dupe = state.snapshots.find((s) => s.name === env && s.app_id === appid);
  if (dupe) {
    el.textContent = t('import.dupe', { name: dupe.name, appid: dupe.app_id, total: dupe.total });
    el.hidden = false;
  } else {
    el.hidden = true;
  }
}

async function runImportPreview() {
  const text = $('import-text').value;
  if (!text.trim()) {
    dialogError('import-dialog', t('import.error.paste'));
    return;
  }
  try {
    const data = await api('/api/import/preview', jsonOptions('POST', { text }));
    renderImportPreview(data);
    $('import-confirm-btn').hidden = false;
    $('import-preview-btn').hidden = true;
  } catch (err) {
    dialogError('import-dialog', err.message);
  }
}

function renderImportPreview(data) {
  const box = $('import-preview');
  box.textContent = '';
  const warns = data.warnings || [];
  if (warns.length) {
    const list = document.createElement('div');
    list.className = 'warn-list';
    for (const w of warns) {
      const el = document.createElement('div');
      el.className = 'warn';
      const badge = document.createElement('span');
      badge.className = 'warn-badge';
      badge.textContent = `L${w.line}`;
      el.appendChild(badge);
      const msg = document.createElement('span');
      msg.textContent = w.message + (w.content ? `: "${w.content}"` : '');
      el.appendChild(msg);
      list.appendChild(el);
    }
    box.appendChild(list);
  }
  for (const it of data.items || []) {
    const row = document.createElement('div');
    row.className = 'row';
    const code = document.createElement('code');
    code.textContent = it.key;
    row.appendChild(code);
    if (it.sensitive) {
      const tag = document.createElement('span');
      tag.className = 'tag';
      tag.textContent = t('import.tag-sensitive');
      row.appendChild(tag);
    }
    box.appendChild(row);
  }
  box.hidden = false;
}

async function confirmImport() {
  const name = $('import-name').value.trim();
  const appId = $('import-appid').value.trim();
  const text = $('import-text').value;
  if (!name) {
    dialogError('import-dialog', t('import.error.env'));
    return;
  }
  if (!appId) {
    dialogError('import-dialog', t('import.error.appid'));
    return;
  }
  if (!text.trim()) {
    dialogError('import-dialog', t('import.error.paste'));
    return;
  }
  const dupe = state.snapshots.find((s) => s.name === name && s.app_id === appId);
  if (dupe) {
    dialogError('import-dialog', t('import.error.dupe', { name, appid: appId }));
    return;
  }
  try {
    const data = await api('/api/snapshots', jsonOptions('POST', { name, app_id: appId, text }));
    closeDialog('import-dialog');
    await refreshSnapshots({ name: data.name, app_id: data.app_id });
  } catch (err) {
    dialogError('import-dialog', err.message);
  }
}

// ---- entry edit / replace / delete ----

function openEntry(item) {
  editing = item;
  $('entry-key').textContent = item.key;
  $('entry-env').textContent = item.key.slice(0, 1);
  const input = $('entry-value');
  const warn = $('entry-warning');
  if (item.sensitive) {
    $('entry-title').textContent = t('entry.title-sensitive');
    input.type = 'password';
    input.value = '';
    input.placeholder = t('entry.ph');
    warn.hidden = false;
  } else {
    $('entry-title').textContent = t('entry.title');
    input.type = 'text';
    input.value = item.value || '';
    input.placeholder = '';
    warn.hidden = true;
  }
  openDialog('entry-dialog');
  input.focus();
}

async function saveEntry() {
  if (!editing) return;
  const key = editing.key;
  const secret = editing.sensitive;
  const value = $('entry-value').value;
  if (secret && !value.trim()) {
    closeDialog('entry-dialog');
    editing = null;
    return;
  }
  try {
    await api(`/api/snapshots/${encodeURIComponent(state.active)}/items/${encodeURIComponent(key)}${snapshotQuery()}`,
      jsonOptions('PUT', { value, secret }));
    closeDialog('entry-dialog');
    editing = null;
    await reloadSnapshotData();
  } catch (err) {
    dialogError('entry-dialog', err.message);
  }
}

function openDelete() {
  if (!editing) return;
  $('delete-key').textContent = editing.key;
  openDialog('delete-dialog');
}

async function confirmDelete() {
  if (!editing) return;
  const key = editing.key;
  try {
    await api(`/api/snapshots/${encodeURIComponent(state.active)}/items/${encodeURIComponent(key)}${snapshotQuery()}`,
      { method: 'DELETE' });
    closeDialog('delete-dialog');
    closeDialog('entry-dialog');
    editing = null;
    await reloadSnapshotData();
  } catch (err) {
    dialogError('delete-dialog', err.message);
  }
}

// ---- compare ----

function openCompare() {
  const select = $('compare-target');
  select.textContent = '';
  $('compare-from').textContent = refLabel(state.active, state.activeAppID) || '—';
  for (const s of state.snapshots) {
    if (s.name === state.active && (s.app_id || '') === (state.activeAppID || '')) continue;
    const opt = document.createElement('option');
    opt.value = `${s.name}\u0000${s.app_id || ''}`;
    opt.textContent = s.app_id ? `${s.name} (${s.app_id})` : s.name;
    select.appendChild(opt);
  }
  if (!select.options.length) {
    showError(t('compare.no-other'));
    return;
  }
  if (state.lastCompareTo) {
    const match = [...select.options].find((o) => o.value === state.lastCompareTo);
    if (match) match.selected = true;
  }
  openDialog('compare-dialog');
}

async function confirmCompare() {
  const from = state.active;
  const optVal = $('compare-target').value;
  const sepIdx = optVal.indexOf('\u0000');
  const to = sepIdx >= 0 ? optVal.slice(0, sepIdx) : optVal;
  const toApp = sepIdx >= 0 ? optVal.slice(sepIdx + 1) : '';
  const fromRef = `${from}\u0000${state.activeAppID || ''}`;
  const toRef = `${to}\u0000${toApp}`;
  if (!from || !to || fromRef === toRef) {
    dialogError('compare-dialog', t('compare.error.target'));
    return;
  }
  try {
    const params = new URLSearchParams({ from, to });
    if (state.activeAppID) params.set('from_appid', state.activeAppID);
    if (toApp) params.set('to_appid', toApp);
    const data = await api(`/api/compare?${params.toString()}`);
    state.compare = data;
    state.lastCompareTo = optVal;
    state.compareMeta = {
      fromLabel: refLabel(from, state.activeAppID),
      toLabel: refLabel(to, toApp),
      changes: data.changes || [],
    };
    $('compare-filter').value = '';
    renderCompareRefs(state.compareMeta.fromLabel, state.compareMeta.toLabel, state.compareMeta.changes);
    renderCompare(state.compareMeta.changes);
    closeDialog('compare-dialog');
    openDialog('compare-result-dialog');
  } catch (err) {
    dialogError('compare-dialog', err.message);
  }
}

function renderCompareRefs(fromLabel, toLabel, changes) {
  const bar = $('compare-refs');
  bar.textContent = '';
  const f = document.createElement('span');
  f.className = 'compare-ref from';
  f.textContent = fromLabel;
  const arrow = document.createElement('span');
  arrow.className = 'compare-arrow';
  arrow.textContent = '→';
  const el = document.createElement('span');
  el.className = 'compare-ref to';
  el.textContent = toLabel;
  bar.appendChild(f);
  bar.appendChild(arrow);
  bar.appendChild(el);
  const stat = document.createElement('span');
  stat.className = 'compare-stat';
  const counts = { added: 0, removed: 0, changed: 0 };
  for (const c of changes) counts[c.kind] = (counts[c.kind] || 0) + 1;
  const parts = [];
  if (counts.added) {
    const s = document.createElement('span');
    s.className = 'add';
    s.textContent = t('compare.added', { n: counts.added });
    parts.push(s);
  }
  if (counts.removed) {
    const s = document.createElement('span');
    s.className = 'del';
    s.textContent = t('compare.removed', { n: counts.removed });
    parts.push(s);
  }
  if (counts.changed) {
    const s = document.createElement('span');
    s.className = 'chg';
    s.textContent = t('compare.changed', { n: counts.changed });
    parts.push(s);
  }
  if (!parts.length) {
    const s = document.createElement('span');
    s.className = 'same';
    s.textContent = t('compare.no-changes');
    parts.push(s);
  }
  for (const p of parts) stat.appendChild(p);
  bar.appendChild(stat);
  bar.hidden = false;
}

function renderCompare(changes) {
  const body = $('compare-body');
  body.textContent = '';
  if (!changes.length) {
    const d = document.createElement('span');
    d.className = 'empty';
    d.textContent = t('compare.identical');
    body.appendChild(d);
    return;
  }
  const fmt = (v) => {
    if (!v || !v.present) return '';
    if (v.sensitive) {
      const fp = v.fingerprint ? t('compare.fp', { fp: v.fingerprint }) : '';
      return t('compare.sensitive', { n: v.length }) + fp;
    }
    return v.value == null ? '' : v.value;
  };
  const line = (sym, cls, text) => {
    const row = document.createElement('div');
    row.className = 'row ' + cls;
    const s = document.createElement('span');
    s.className = 'sym';
    s.textContent = sym;
    const code = document.createElement('code');
    code.textContent = text;
    row.appendChild(s);
    row.appendChild(code);
    return row;
  };
  for (const c of changes) {
    if (c.kind === 'added') {
      body.appendChild(line('+', 'add', `${c.key} = ${fmt(c.new)}`));
    } else if (c.kind === 'removed') {
      body.appendChild(line('-', 'del', `${c.key} = ${fmt(c.old)}`));
    } else {
      body.appendChild(line('-', 'del', `${c.key} = ${fmt(c.old)}`));
      body.appendChild(line('+', 'add', `${c.key} = ${fmt(c.new)}`));
    }
  }
}

function applyCompareFilter() {
  const q = $('compare-filter').value.trim().toLowerCase();
  const all = state.compareMeta ? state.compareMeta.changes : [];
  const filtered = q ? all.filter((c) => c.key.toLowerCase().includes(q)) : all;
  renderCompare(filtered);
}

function copyText(text) {
  return new Promise((resolve, reject) => {
    if (navigator.clipboard && navigator.clipboard.writeText) {
      navigator.clipboard.writeText(text).then(resolve).catch(() => {
        legacyCopy(text) ? resolve() : reject(new Error('copy failed'));
      });
      return;
    }
    legacyCopy(text) ? resolve() : reject(new Error('copy failed'));
  });
  function legacyCopy(t) {
    const ta = document.createElement('textarea');
    ta.value = t;
    ta.style.position = 'fixed';
    ta.style.opacity = '0';
    document.body.appendChild(ta);
    ta.select();
    let ok = false;
    try { ok = document.execCommand('copy'); } catch (_) { ok = false; }
    ta.remove();
    return ok;
  }
}

function copyCompareDiff() {
  const meta = state.compareMeta;
  if (!meta) return;
  const fmtLine = (v) => {
    if (!v || !v.present) return '—';
    if (v.sensitive) {
      const fp = v.fingerprint ? t('compare.fp', { fp: v.fingerprint }) : '';
      return t('compare.sensitive', { n: v.length }) + fp;
    }
    return v.value == null ? '' : v.value;
  };
  const lines = [`${meta.fromLabel} → ${meta.toLabel}`];
  for (const c of meta.changes) {
    if (c.kind === 'added') lines.push(`+ ${c.key} = ${fmtLine(c.new)}`);
    else if (c.kind === 'removed') lines.push(`- ${c.key} = ${fmtLine(c.old)}`);
    else {
      lines.push(`- ${c.key} = ${fmtLine(c.old)}`);
      lines.push(`+ ${c.key} = ${fmtLine(c.new)}`);
    }
  }
  copyText(lines.join('\n'))
    .then(() => {
      const btn = $('compare-copy-btn');
      btn.textContent = t('compare.copied');
      setTimeout(() => { btn.textContent = t('dialog.compare-result.copy'); }, 1500);
    })
    .catch(() => {
      const btn = $('compare-copy-btn');
      btn.textContent = t('compare.copy-failed');
      setTimeout(() => { btn.textContent = t('dialog.compare-result.copy'); }, 1500);
    });
}

// ---- multi-environment horizontal compare ----

let multiState = null;

function openMultiCompare() {
  const list = $('multi-ref-list');
  list.textContent = '';
  $('multi-compare-error').hidden = true;
  for (const s of state.snapshots) {
    const label = document.createElement('label');
    label.className = 'check-item';
    const cb = document.createElement('input');
    cb.type = 'checkbox';
    cb.value = `${s.name}\u0000${s.app_id || ''}`;
    if (s.name === state.active && (s.app_id || '') === (state.activeAppID || '')) cb.checked = true;
    const txt = document.createElement('span');
    txt.textContent = refLabel(s.name, s.app_id);
    label.appendChild(cb);
    label.appendChild(txt);
    list.appendChild(label);
  }
  openDialog('multi-compare-dialog');
}

async function confirmMultiCompare() {
  const refs = [];
  for (const cb of $('multi-ref-list').querySelectorAll('input[type=checkbox]:checked')) {
    const [name, appID] = cb.value.split('\u0000');
    refs.push({ name, app_id: appID });
  }
  if (refs.length < 2) {
    dialogError('multi-compare-dialog', t('multi.error'));
    return;
  }
  try {
    const data = await api('/api/compare/multi', jsonOptions('POST', { refs }));
    multiState = data;
    renderMultiTable(data);
    closeDialog('multi-compare-dialog');
    openDialog('multi-result-dialog');
  } catch (err) {
    dialogError('multi-compare-dialog', err.message);
  }
}

function multiValueText(v) {
  if (!v || !v.present) return { text: '—', cls: 'absent' };
  if (v.sensitive) {
    const fp = v.fingerprint ? t('compare.fp', { fp: v.fingerprint }) : '';
    return { text: t('compare.sensitive', { n: v.length }) + fp, cls: 'masked' };
  }
  return { text: v.value == null ? '' : v.value, cls: 'plain' };
}

function stripQuotes(s) {
  if (typeof s !== 'string') return s;
  if (s.length >= 2 && ((s[0] === '"' && s[s.length - 1] === '"') || (s[0] === "'" && s[s.length - 1] === "'"))) {
    return s.slice(1, -1);
  }
  return s;
}

function rowHasDiff(row) {
  const seen = new Set();
  let presentCount = 0;
  for (const v of row.values) {
    if (!v || !v.present) continue;
    presentCount++;
    const t = v.sensitive ? `s:${v.length}:${v.fingerprint || ''}` : `p:${stripQuotes(v.value)}`;
    seen.add(t);
  }
  return seen.size > 1 || (presentCount > 0 && presentCount < row.values.length);
}

function renderMultiTable(data) {
  const wrap = $('multi-table-wrap');
  wrap.textContent = '';
  const table = document.createElement('table');
  table.className = 'multi-table';
  const thead = document.createElement('thead');
  const hrow = document.createElement('tr');
  const thKey = document.createElement('th');
  thKey.textContent = 'KEY';
  hrow.appendChild(thKey);
  for (const ref of data.refs) {
    const th = document.createElement('th');
    th.textContent = refLabel(ref.name, ref.app_id);
    th.title = t('multi.header-title', { n: ref.total, s: ref.sensitive });
    hrow.appendChild(th);
  }
  thead.appendChild(hrow);
  table.appendChild(thead);
  const tbody = document.createElement('tbody');
  for (const row of data.rows) {
    const tr = document.createElement('tr');
    const tdKey = document.createElement('td');
    tdKey.className = 'k';
    tdKey.textContent = row.key;
    tr.appendChild(tdKey);
    let hasDiff = rowHasDiff(row);
    for (const v of row.values) {
      const td = document.createElement('td');
      const info = multiValueText(v);
      td.textContent = info.text;
      if (info.cls === 'masked') td.classList.add('m');
      if (info.cls === 'absent') td.classList.add('a');
      tr.appendChild(td);
    }
    if (hasDiff) tr.classList.add('diff-row');
    tbody.appendChild(tr);
  }
  table.appendChild(tbody);
  wrap.appendChild(table);
}

function copyMultiCompare() {
  if (!multiState) return;
  const lines = ['KEY\t' + multiState.refs.map((r) => refLabel(r.name, r.app_id)).join('\t')];
  for (const row of multiState.rows) {
    lines.push(row.key + '\t' + row.values.map((v) => {
      if (!v || !v.present) return '—';
      if (v.sensitive) return t('multi.sensitive', { n: v.length, fp: v.fingerprint ? `·${v.fingerprint}` : '' });
      return v.value == null ? '' : v.value;
    }).join('\t'));
  }
  copyText(lines.join('\n'))
    .then(() => {
      const btn = $('multi-copy-btn');
      btn.textContent = t('compare.copied');
      setTimeout(() => { btn.textContent = t('dialog.multi-result.copy-tsv'); }, 1500);
    })
    .catch(() => {
      const btn = $('multi-copy-btn');
      btn.textContent = t('compare.copy-failed');
      setTimeout(() => { btn.textContent = t('dialog.multi-result.copy-tsv'); }, 1500);
    });
}

function multiCellText(v) {
  if (!v || !v.present) return '—';
  if (v.sensitive) return t('multi.sensitive', { n: v.length, fp: v.fingerprint ? `·${v.fingerprint}` : '' });
  return v.value == null ? '' : v.value;
}

function showMultiReport() {
  if (!multiState) return;
  $('multi-report-body').textContent = buildMultiReportText(multiState);
  openDialog('multi-report-dialog');
}

function buildMultiReportText(data) {
  const refsLabel = data.refs.map((r) => refLabel(r.name, r.app_id)).join(' / ');
  const diffRows = data.rows.filter(rowHasDiff);
  const sensitiveDiff = diffRows.filter((r) => r.values.some((v) => v && v.sensitive)).length;
  const lines = [];
  lines.push(t('report.title'));
  lines.push(t('report.refs', { refs: refsLabel }));
  lines.push(t('report.stats', { total: data.rows.length, diff: diffRows.length, same: data.rows.length - diffRows.length, sensitiveDiff }));
  lines.push('');
  if (!diffRows.length) {
    lines.push(t('report.all-same'));
  } else {
    lines.push(t('report.diff-fields'));
    for (const row of diffRows) {
      const cells = data.refs.map((r, i) => `${refLabel(r.name, r.app_id)}: ${multiCellText(row.values[i])}`);
      lines.push(`${row.key} → ${cells.join(' | ')}`);
    }
  }
  return lines.join('\n');
}

function copyMultiReport() {
  if (!multiState) return;
  copyText(buildMultiReportText(multiState))
    .then(() => {
      const btn = $('multi-report-copy-btn');
      btn.textContent = t('compare.copied');
      setTimeout(() => { btn.textContent = t('dialog.multi-report.copy'); }, 1500);
    })
    .catch(() => {
      const btn = $('multi-report-copy-btn');
      btn.textContent = t('compare.copy-failed');
      setTimeout(() => { btn.textContent = t('dialog.multi-report.copy'); }, 1500);
    });
}

function csvEscape(v) {
  if (/[",\n\r]/.test(v)) return '"' + v.replace(/"/g, '""') + '"';
  return v;
}

function copyMultiCSV() {
  if (!multiState) return;
  const lines = [['KEY', ...multiState.refs.map((r) => refLabel(r.name, r.app_id))].map(csvEscape).join(',')];
  for (const row of multiState.rows) {
    const cells = [row.key, ...row.values.map(multiCellText)].map(csvEscape);
    lines.push(cells.join(','));
  }
  copyText(lines.join('\n'))
    .then(() => {
      const btn = $('multi-csv-btn');
      btn.textContent = t('compare.copied');
      setTimeout(() => { btn.textContent = t('dialog.multi-result.copy-csv'); }, 1500);
    })
    .catch(() => {
      const btn = $('multi-csv-btn');
      btn.textContent = t('compare.copy-failed');
      setTimeout(() => { btn.textContent = t('dialog.multi-result.copy-csv'); }, 1500);
    });
}

// ---- single key across snapshots ----

function openKeyCompare(key) {
  const appid = state.activeAppID || '';
  $('key-compare-title').textContent = appid
    ? t('key-compare.title-appid', { key, appid })
    : t('key-compare.title', { key });
  const body = $('key-compare-body');
  body.textContent = '';
  const loading = document.createElement('span');
  loading.className = 'empty';
  loading.textContent = t('key-compare.loading');
  body.appendChild(loading);
  openDialog('key-compare-dialog');
  loadKeyCompare(key);
}
async function loadKeyCompare(key) {
  const body = $('key-compare-body');
  try {
    const appid = state.activeAppID || '';
    const data = await api(`/api/compare/key?key=${encodeURIComponent(key)}&appid=${encodeURIComponent(appid)}`);
    body.textContent = '';
    if (!data.rows.length) {
      const d = document.createElement('span');
      d.className = 'empty';
      d.textContent = t('key-compare.empty');
      body.appendChild(d);
      return;
    }
    for (const row of data.rows) {
      const line = document.createElement('div');
      line.className = 'row';
      const ref = document.createElement('code');
      ref.className = 'key-ref';
      ref.textContent = refLabel(row.name, row.app_id);
      const val = document.createElement('code');
      const info = multiValueText(row.value);
      val.className = info.cls;
      val.textContent = info.text;
      line.appendChild(ref);
      line.appendChild(val);
      body.appendChild(line);
    }
  } catch (err) {
    body.textContent = '';
    const d = document.createElement('span');
    d.className = 'empty';
    d.textContent = err.message;
    body.appendChild(d);
  }
}

// ---- export ----

function openExport() {
  if (!state.active) {
    showError(t('export.no-selection'));
    return;
  }
  $('export-name').textContent = refLabel(state.active, state.activeAppID);
  openDialog('export-dialog');
}

async function confirmExport() {
  const name = state.active;
  if (!name) {
    dialogError('export-dialog', t('export.no-selection'));
    return;
  }
  try {
    const res = await api(`/api/snapshots/${encodeURIComponent(name)}/export${snapshotQuery()}`,
      jsonOptions('POST', { confirm: true }));
    const blob = await res.blob();
    const url = URL.createObjectURL(blob);
    const a = document.createElement('a');
    a.href = url;
    a.download = `${name}${state.activeAppID ? `__${state.activeAppID}` : ''}.json`;
    document.body.appendChild(a);
    a.click();
    a.remove();
    URL.revokeObjectURL(url);
    closeDialog('export-dialog');
  } catch (err) {
    dialogError('export-dialog', err.message);
  }
}

async function copyExport() {
  const name = state.active;
  if (!name) { dialogError('export-dialog', t('export.no-selection')); return; }
  try {
    const res = await api(`/api/snapshots/${encodeURIComponent(name)}/export${snapshotQuery()}`,
      jsonOptions('POST', { confirm: true }));
    const text = await res.text();
    await copyText(text);
    closeDialog('export-dialog');
  } catch (err) {
    dialogError('export-dialog', err.message);
  }
}

// ---- summary (placeholder) ----

function openSummary() {
  openDialog('summary-dialog');
}

// ---- snapshot delete ----

function snapshotQuery() {
  return state.activeAppID ? `?appid=${encodeURIComponent(state.activeAppID)}` : '';
}

let deletingSnapshot = null;

function openDeleteSnapshot(s) {
  deletingSnapshot = s;
  $('snap-delete-name').textContent = s.name;
  $('snap-delete-appid').textContent = s.app_id || '—';
  openDialog('snap-delete-dialog');
}

async function confirmDeleteSnapshot() {
  if (!deletingSnapshot) return;
  const { name, app_id } = deletingSnapshot;
  const q = app_id ? `?appid=${encodeURIComponent(app_id)}` : '';
  try {
    await api(`/api/snapshots/${encodeURIComponent(name)}${q}`, { method: 'DELETE' });
    closeDialog('snap-delete-dialog');
    deletingSnapshot = null;
    if (state.active === name && (state.activeAppID || '') === (app_id || '')) {
      state.active = null;
      state.activeAppID = '';
    }
    await refreshSnapshots();
  } catch (err) {
    dialogError('snap-delete-dialog', err.message);
  }
}

// ---- reveal ----

let revealItem = null;

function openReveal(item) {
  revealItem = item;
  $('reveal-key').textContent = item.key;
  $('reveal-value').textContent = '';
  $('reveal-value').hidden = true;
  $('reveal-error').hidden = true;
  $('reveal-advanced').hidden = true;
  $('reveal-key-input').value = '';
  $('reveal-iv-input').value = '';
  $('reveal-confirm-btn').hidden = false;
  $('reveal-confirm-btn').textContent = t('reveal.show');
  openDialog('reveal-dialog');
}

async function confirmReveal() {
  if (!revealItem) return;
  const key = revealItem.key;
  const aesKey = $('reveal-key-input').value.trim();
  const aesIV = $('reveal-iv-input').value.trim();
  try {
    const data = await api(`/api/snapshots/${encodeURIComponent(state.active)}/reveal${snapshotQuery()}`,
      jsonOptions('POST', { targets: [key], confirm: true, key: aesKey, iv: aesIV }));
    $('reveal-value').textContent = data.values[key] != null ? data.values[key] : t('reveal.empty');
    $('reveal-value').hidden = false;
    $('reveal-confirm-btn').hidden = true;
  } catch (err) {
    $('reveal-advanced').hidden = false;
    dialogError('reveal-dialog', err.message);
  }
}

// ---- bulk edit ----

let editLoaded = false;

function openBulkEdit() {
  if (!state.active) { showError(t('export.no-selection')); return; }
  $('edit-name').textContent = refLabel(state.active, state.activeAppID);
  $('edit-text').value = '';
  $('edit-save-btn').hidden = true;
  editLoaded = false;
  openDialog('edit-dialog');
  loadBulkEdit();
}

async function loadBulkEdit() {
  if (!state.active) return;
  try {
    const data = await api(`/api/snapshots/${encodeURIComponent(state.active)}/edit${snapshotQuery()}`,
      jsonOptions('POST', { confirm: true }));
    $('edit-text').value = data.text;
    $('edit-save-btn').hidden = false;
    editLoaded = true;
  } catch (err) {
    dialogError('edit-dialog', err.message);
  }
}

async function saveBulkEdit() {
  if (!state.active || !editLoaded) return;
  const text = $('edit-text').value;
  try {
    await api(`/api/snapshots/${encodeURIComponent(state.active)}/edit${snapshotQuery()}`,
      jsonOptions('PUT', { text }));
    closeDialog('edit-dialog');
    await reloadSnapshotData();
  } catch (err) {
    dialogError('edit-dialog', err.message);
  }
}

// ---- wiring ----

function wire() {
  document.addEventListener('click', (e) => {
    const close = e.target.closest('[data-close]');
    if (close) {
      close.closest('dialog').close();
      return;
    }
    const act = e.target.closest('[data-action]');
    if (!act) return;
    switch (act.dataset.action) {
      case 'import': openImport(); break;
      case 'compare': openCompare(); break;
      case 'export': openExport(); break;
      case 'focus-search': switchView('snapshots'); $('search-input').focus(); break;
      case 'summary': openSummary(); break;
      case 'bulk-edit': openBulkEdit(); break;
      case 'multi-compare': openMultiCompare(); break;
    }
  });

  $('nav-aes').addEventListener('click', () => switchView('aes'));
  $('nav-db').addEventListener('click', () => switchView('db'));
  $('nav-settings').addEventListener('click', () => switchView('settings'));

  $('lang-toggle').addEventListener('click', (e) => {
    const btn = e.target.closest('[data-lang]');
    if (btn) setLang(btn.dataset.lang);
  });

  $('db-init-btn').addEventListener('click', dbInit);
  $('db-add-btn').addEventListener('click', dbAdd);
  $('db-test-btn').addEventListener('click', dbTestURL);
  $('db-regen-all-btn').addEventListener('click', dbRegenAll);
  $('db-body').addEventListener('click', (e) => {
    const btn = e.target.closest('[data-db-action]');
    if (!btn) return;
    const name = btn.getAttribute('data-name');
    switch (btn.getAttribute('data-db-action')) {
      case 'test': dbTest(name); break;
      case 'connect': dbConnectInfo(name); break;
      case 'regen': dbRegen(name); break;
      case 'show': dbShow(name); break;
      case 'on': dbSetTunnel(name, true); break;
      case 'off': dbSetTunnel(name, false); break;
      case 'rm': dbRemove(name); break;
    }
  });
  $('db-connect-dialog').addEventListener('click', (e) => {
    const btn = e.target.closest('[data-copy]');
    if (!btn) return;
    const code = btn.closest('.db-line').querySelector('code');
    if (!code) return;
    copyText(code.textContent)
      .then(() => { btn.textContent = t('compare.copied'); setTimeout(() => { btn.textContent = t('common.copy'); }, 1200); })
      .catch(() => {});
  });
  $('db-show-copy-btn').addEventListener('click', () => {
    const text = $('db-show-url').textContent;
    if (!text) return;
    copyText(text)
      .then(() => { $('db-show-copy-btn').textContent = t('compare.copied'); setTimeout(() => { $('db-show-copy-btn').textContent = t('common.copy'); }, 1200); })
      .catch(() => {});
  });

  $('aes-encrypt-btn').addEventListener('click', () => runAES('encrypt'));
  $('aes-decrypt-btn').addEventListener('click', () => runAES('decrypt'));
  $('aes-copy').addEventListener('click', copyAESOutput);

  $('init-key-btn').addEventListener('click', initKey);
  $('init-sensitive-btn').addEventListener('click', initSensitiveKey);

  $('reveal-confirm-btn').addEventListener('click', confirmReveal);
  $('edit-form').addEventListener('submit', (e) => { e.preventDefault(); saveBulkEdit(); });

  $('snap-delete-confirm-btn').addEventListener('click', confirmDeleteSnapshot);
  $('db-delete-confirm-btn').addEventListener('click', confirmDBDelete);
  $('db-regen-confirm-btn').addEventListener('click', confirmDBRegen);

  $('export-copy-btn').addEventListener('click', copyExport);

  $('search-input').addEventListener('input', renderTable);

  $('import-form').addEventListener('submit', (e) => {
    e.preventDefault();
    if ($('import-confirm-btn').hidden) runImportPreview();
    else confirmImport();
  });
  $('import-preview-btn').addEventListener('click', runImportPreview);
  $('import-name').addEventListener('input', checkImportDuplicate);
  $('import-appid').addEventListener('input', checkImportDuplicate);

  $('entry-form').addEventListener('submit', (e) => {
    e.preventDefault();
    saveEntry();
  });
  $('entry-delete-btn').addEventListener('click', openDelete);
  $('delete-confirm-btn').addEventListener('click', confirmDelete);
  $('compare-confirm-btn').addEventListener('click', confirmCompare);
  $('compare-filter').addEventListener('input', applyCompareFilter);
  $('compare-copy-btn').addEventListener('click', copyCompareDiff);
  $('multi-compare-confirm-btn').addEventListener('click', confirmMultiCompare);
  $('multi-copy-btn').addEventListener('click', copyMultiCompare);
  $('multi-report-btn').addEventListener('click', showMultiReport);
  $('multi-report-copy-btn').addEventListener('click', copyMultiReport);
  $('multi-csv-btn').addEventListener('click', copyMultiCSV);
  $('export-confirm-btn').addEventListener('click', confirmExport);
}

async function init() {
  wire();
  applyI18n();
  // First visit in this browser: adopt the shared CLI/UI language (the
  // ~/.vaulty/prefs.json set by `vaulty-keeper lang` or another browser).
  try {
    if (!localStorage.getItem(LANG_KEY)) {
      const p = await api('/api/prefs');
      if (p && (p.lang === 'en' || p.lang === 'zh') && p.lang !== LANG) {
        LANG = p.lang;
        try { localStorage.setItem(LANG_KEY, LANG); } catch (_) {}
        applyI18n();
      }
    }
  } catch (_) { /* prefs read is best-effort */ }
  switchView('snapshots');
  if (!TOKEN) {
    showError(t('common.no-token'));
  }
  try {
    const cfg = await api('/api/config');
    if (cfg && !cfg.allow_plaintext) {
      $('plaintext-banner').hidden = false;
    }
  } catch (_) { /* banner is cosmetic */ }
  try {
    const data = await api('/api/snapshots');
    state.snapshots = data.snapshots;
  } catch (err) {
    showError(err.message);
  }
  renderRail();
  const first = state.snapshots[0];
  if (first) await loadSnapshot(first.name, first.app_id);
  else {
    state.active = null;
    state.activeAppID = '';
    renderContext();
    renderTable();
  }
}

document.addEventListener('DOMContentLoaded', init);

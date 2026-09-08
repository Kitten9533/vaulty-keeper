# vaulty-keeper 安全策略

> 中文 | [English](SECURITY.md)

vaulty-keeper 是一个个人 AI 工具箱：加密 Apollo 快照存储、AES-256-GCM 工具和本地数据库隧道，附带仅回环的 Web UI。统一安全参考是[安全模型](docs/security-model.zh-CN.md)（[English](docs/security-model.md)）——信任层级、明文生命周期、token 生命周期、容器限制、协议限制和验证状态。本文件只是入口和报告策略。

## 受支持版本

支持最新的已发布 tag。安全修复应用到当前源码树并以新 tag 发布；不对旧版本做 LTS 或 backport 承诺。

## 报告漏洞

**不要**为安全问题开公开 issue。请私下报告：

- 在 GitHub 上打开[私有安全通告](https://github.com/Kitten9533/vaulty-keeper/security/advisories/new)，或
- 通过邮件联系仓库所有者，说明描述、受影响版本，以及（如可能）最小复现。

报告中**不要**包含真实秘密、真实 DSN 或密钥材料。你会收到确认，并会被询问公开披露时机。

## 安全边界速览

| 领域 | 保证 |
|---|---|
| 静态加密 | 已存储的快照值和已注册数据库 URL/隧道 token 以 AES-256-GCM 加密（0600）。不覆盖：明文 AES key/iv JSON、导入源、导出/下载、编辑器临时文件、shell 历史、终端输出、剪贴板、进程内存。 |
| 同用户进程 | 系统密钥存储和 0600 文件**不能**阻止同用户进程（包括 AI shell）读取密钥。不要把密钥、DSN 或密文放到不可信 agent 可达的位置。 |
| 明文出口 | `reveal`/`export`/`edit`/`aes decrypt`/`db show` 需要 stdin TTY；TTY 是防误操作门禁，不是真人证明。 |
| Web UI | 仅回环；非 GET 操作需要 UI token；显式明文路由还需要 `--allow-plaintext`。 |
| DB 隧道 | token 门控访问；PG/MySQL/Redis 接受全局或专属 token，MongoDB 只接受专属 token。token 不加密客户端到代理的传输。 |

以上是摘要。做信任决策前请阅读[安全模型](docs/security-model.zh-CN.md)，并且不要拿文档表述当作测试或生产证据。

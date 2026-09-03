# vaulty-keeper 数据库隧道代理 · 图解

> 用图说话：Docker 里是什么、凭据存在哪、隧道怎么工作、安全边界在哪。
> 配合 `scripts/dbtest.sh` 一起看，跑一遍再对照图，就全通了。

---

## 图 1 · 总览：一图看懂全链路

```
┌─────────────────────────────────────────────────────────────────────┐
│ Docker 容器（AI agent 隔离域：摸不到密钥/密文/真实凭据）                 │
│                                                                     │
│   你的客户端：psql / mysql / redis-cli / DBeaver / Redis Insight     │
│      │  只带 TOKEN，不知道真实账号密码                                 │
└──────┼──────────────────────────────────────────────────────────────┘
       │ TCP（容器内用 host 访问时，host 部分写 host.docker.internal）
       ▼
┌─────────────────────────────────────────────────────────────────────┐
│ Host：vaulty-keeper serve --addr 0.0.0.0:8972   （一个进程，两层服务）      │
│                                                                     │
│  ① HTTP 掩码桥 :8972            ② DB 隧道（每连接一个 TCP 端口）       │
│    /api/* 全部要 token              pgdb       :15432 (postgres)     │
│    remote list/get/compare          mysqltest  :15435 (mysql·sha2)   │
│    remote dblist → 连接清单          mysqlnative:15436 (mysql·native)│
│                                     cache      :15434 (redis)       │
│        │                                  │                          │
│        │  token 校验                       │  token 校验（协议字段）   │
│        ▼                                  ▼                          │
│    读 db.json（DB Key 解密）         读 db.json → 握手注入真实凭据      │
│        │                                  │                          │
│        │    ┌─────────────────────────────┘                          │
│        │    ▼                                                       │
│        │  DB Key（系统密钥库 / env VAULTY_KEEPER_DB_KEY 兜底）              │
└───────┼──────────────────────────────────────────────────────────────┘
        │ TCP（真实凭据只在 host 进程内存里出现，绝不出 host）
        ▼
┌─────────────────────────────────────────────────────────────────────┐
│ 真实数据库（可以是 Docker 容器 / 内网机器 / 云 RDS，对隧道一视同仁）    │
│   PostgreSQL :59918    MySQL :59919    Redis :59920                  │
└─────────────────────────────────────────────────────────────────────┘
```

一句话：**AI 只认 token 和隧道端口；真实凭据只活在 host 的 serve 进程里；两者之间由隧道在握手阶段完成"换凭据"。**

---

## 图 2 · 现在 Docker 里是什么（测试环境快照）

```
跑着的容器（只是"测试用的数据库"，serve 把它们当普通远端 DB，不特殊处理）
┌───────────┬──────────────────┬────────────────┬──────────────────────┐
│ 容器       │ 镜像              │ 宿主端口(动态)   │ 真实凭据              │
├───────────┼──────────────────┼────────────────┼──────────────────────┤
│ aipg      │ postgres:17.6    │ 127.0.0.1:59918│ app / pgpass / appdb │
│ aimysql8  │ mysql:8.4        │ 127.0.0.1:59919│ sha2user+sha2pass    │
│           │                  │                │ nativeuser+nativepass│
│           │                  │                │ 库: shop             │
│ airedis   │ redis:7          │ 127.0.0.1:59920│ :redispass / 0       │
└───────────┴──────────────────┴────────────────┴──────────────────────┘

这些 URL 被 vaulty-keeper 加密后注册成"连接"（存 db.json）：
  pgdb      ← postgres://app:pgpass@127.0.0.1:59918/appdb
  mysqltest ← mysql://sha2user:sha2pass@127.0.0.1:59919/shop
  mysqlnative← mysql://nativeuser:nativepass@127.0.0.1:59919/shop
  cache     ← redis://:redispass@127.0.0.1:59920/0

serve 为每个连接开一个隧道端口，AI 侧拿到的"地址"：
  PostgreSQL:  jdbc:postgresql://127.0.0.1:15432/appdb?user=<TOKEN>
  MySQL(sha2): 127.0.0.1:15435  user=<TOKEN> 密码任意
  MySQL(native):127.0.0.1:15436 user=<TOKEN> 密码任意
  Redis:       127.0.0.1:15434  AUTH <TOKEN>
```

注意：**隧道端口（15432/15435/15436/15434）是固定的**（注册时 `--port` 指定或自动分配并写入 db.json）；**容器宿主端口（59918/59919/59920）是动态的**，每次重跑 `scripts/dbtest.sh` 会变。AI 只需要隧道端口，不需要知道容器端口。

---

## 图 3 · 真实账号密码存在哪（存储链路）

```
① 注册连接：echo 'postgres://app:pgpass@...' | vaulty-keeper db add pgdb
             │  URL 从 stdin 读，不进命令行参数 → 不进 shell history / ps
             ▼
真实 URL（含账号密码）
   │  AES-256-GCM 加密（密钥 = DB Key，独立于快照/敏感值密钥）
   ▼
db.json（磁盘密文，权限 0600，无任何明文）◄── 快照密钥/敏感值密钥泄露也解不开
   │
   │  serve 启动时：Keychain(或 env VAULTY_KEEPER_DB_KEY) 取 DB Key → 解密 URL
   ▼
进程内存中的 URL（只存在于 host 的 serve 进程）
   │  每个连接：token 校验 → 用真实凭据连真实库 → 握手注入 → 纯字节转发
   ▼
真实数据库
```

安全分层的意义：
| 层 | 防什么 |
|---|---|
| db.json 只有密文 + 0600 | 磁盘/备份/误发文件都不泄露明文 |
| DB Key 独立于其他密钥 | 快照密钥泄露 ≠ 数据库凭据泄露 |
| DB Key 在系统密钥库 | 防其他用户 / 其他机器 / 意外明文 |
| 真实凭据只进 host 内存 | 日志、回包、客户端、AI 环境全部接触不到 |
| token 门控 | 防"无 token 的第三方"（局域网暴露 0.0.0.0 时兜底）。token 为**连接专属**（128 位随机，`db add` 生成、随 URL 一并加密落盘），`db regen <name>|--all` 可轮换、旧 token 立即失效；未升级的旧连接回退全局 bridge token（掩码桥同款） |

---

## 图 4 · 三库认证注入：客户端只发 token，隧道换真凭据

```
PostgreSQL :15432           MySQL :15435/15436          Redis :15434
┌──────────────┐           ┌──────────────────┐        ┌──────────────┐
│ psql/DBeaver │           │ mysql/DBeaver    │        │ redis-cli /  │
│ user=<TOKEN> │           │ username=<TOKEN> │        │ Redis Insight│
│ 密码留空      │           │ 密码任意          │        │ AUTH <TOKEN> │
└──────┬───────┘           └────────┬─────────┘        └──────┬───────┘
       │ 假 server 直接放行          │ 假 server 校验          │ 校验首命令
       │ (AuthenticationOk,         │ username=token         │ 为 AUTH token
       │  trust 风格)               │ 然后回 OK               │ 然后回 +OK
       ▼                           ▼                        ▼
┌──────────────────────────────────────────────────────────────────────┐
│  serve 用 db.json 里解密的 URL 去连真实库（真实账号密码只在这步出现）    │
│                                                                      │
│  PG   : 真实 user/password 完成 SCRAM-SHA-256 / md5 / cleartext       │
│  MySQL: 认证应答替换（mysql_native_password / caching_sha2 + RSA）     │
│  Redis: 代发 AUTH <真实密码> (+ SELECT 库号)                           │
└──────────────────────────────────────────────────────────────────────┘
       │ 两端都认证通过 → splice（纯字节转发，不再解析协议）
       ▼
真实数据库
```

关键：**客户端到隧道的"假 server"** 和 **隧道到真实库的"真客户端"** 是两个独立握手，
中间的桥就是 `internal/dbproxy` 里每个协议的 handler。

---

## 图 5 · 安全边界：AI 能看到 vs 看不到

```
AI / 容器内能看到                                    AI 永远看不到
──────────────────────────────────────────          ─────────────────────────
✔ 连接名 / 类型 / 隧道端口（db list）        ✘ 真实 URL（地址 / 账号 / 密码）
✔ 自己的隧道 token（连接专属；旧连接回退 bridge token）   ✘ db.json 密文内容
✔ 查询结果（本来就是要给 AI 的数据）          ✘ DB Key 及任何密钥
✔ 审计日志的"成功 / 拒绝"行（无 SQL/凭据）    ✘ 明文出口（reveal/export 等 TTY-only）
✔ 隧道端口用原生客户端自由查询                ✘ serve 之外的任何明文中间态

防御链条（层层兜底，防"故意对抗的 AI"）：
  ① 容器隔离：AI 进 Docker，摸不到 ~/.vaulty、系统密钥库、真实凭据
  ② token 门控：无 token 的第三方连隧道端口即被断
  ③ 凭据不出 host：真实账号密码只在 serve 进程内存
  ④ 审计：每次成功/拒绝都记（时间、来源 IP、连接名）
  ⑤ 防误标/明文门禁：明文命令只在用户本人 TTY 可用
```

---

## 图 6 · 一次完整查询的时序（以 DBeaver 查 PG 为例）

```
DBeaver                     serve(host)                   真实 PG
   │ jdbc 连 127.0.0.1:15432  │                              │
   │ user=<TOKEN>             │                              │
   ├─────────────────────────▶│                              │
   │                          │ 校验 user==token?            │
   │                          │ 回 AuthenticationOk          │
   │◀─────────────────────────┤  (trust 风格直接放行)          │
   │                          │ 连 127.0.0.1:59918            │
   │                          │ 用 app/pgpass 走 SCRAM        │
   │                          ├─────────────────────────────▶│
   │                          │◀──────── AuthenticationOk ───┤
   │                          │ splice：两边变纯字节通道        │
   │ SELECT * FROM t ────────▶│─────────────────────────────▶│
   │◀──────── 数据行 ─────────│◀──────────── 数据行 ──────────┤
   │   （DBeaver 全程不知道    │                              │
   │    真实账号是 app）        │                              │
```

---

*图对应代码：`internal/dbproxy/tunnel.go`（隧道框架/审计）、`store.go`（db.json 加密存储）、
`postgres.go` / `mysql.go` / `redis.go`（三种协议认证注入）、`internal/cli/db.go`（命令）、
`scripts/dbtest.sh`（Docker 测试环境）。*

---

## 第二部分 · 核心流程走查（纵向图）

> 这种图每走一步给一句注解，适合"从头到尾跟一遍"。

### 流程 1 · 真实凭据的一生（注册 → 存储 → 运行 → 消亡）

```
① 注册：echo 'postgres://app:pgpass@...' | vaulty-keeper db add pgdb
   │ URL 走 stdin，不进命令行 → 不进 ps / shell history
   ▼
② 加密落盘：AES-256-GCM 加密（密钥 = DB Key）◄── DB Key 独立于快照/敏感值密钥
   ▼
③ db.json（0600，只含 url_cipher + nonce）◄── 磁盘无明文；误发/备份/rsync 都不泄
   │
   ▼
④ serve 启动：Keychain(或 env) 取 DB Key → 解密到进程内存
   │ 真实 URL 从此只在 host 内存里活
   ▼
⑤ 每来一个连接：token 校验 → 用真实凭据连真实库 → 握手注入 → 纯字节转发
   │ 客户端 / 日志 / 回包永远见不到 URL
   ▼
⑥ 用完即弃：进程退出、连接断开 → 内存里的 URL 随之消亡
```

### 流程 2 · 一次隧道查询的完整生命周期（以 Redis 为例）

```
客户端 redis-cli -a $TOKEN -p 15434
   │ ① 发 AUTH <token>
   ▼
serve（cache 隧道）
   │ ② token 比对？
   │    不对 → -ERR authentication required（拒绝 + 审计日志）
   ▼ 对
   │ ③ 用 db.json 解密的 URL 连真实 redis（127.0.0.1:59920）
   │ ④ 代发 AUTH <真实密码> ◄── 真实密码只在 host 内部这条链路出现
   ▼
真实 Redis
   │ ⑤ 回 +OK
   ▼
serve
   │ ⑥ 把 +OK 回给客户端 ◄── 客户端以为是自己 token 通过的，不知道真实密码
   ▼
客户端
   │ ⑦ 之后的命令纯字节转发（splice），隧道不再解析协议
   ▼
客户端 ⇄ 真实 Redis（PING / GET / SET ... 结果直通）
```

### 流程 3 · AI 想拿真实密码，有几条路（每条都堵死）

```
路1 让隧道把密码发回来？
   serve 只回 +OK / -ERR / 查询结果 ◄── 认证交换在 host 侧完成，客户端全程看不到
   结果：堵死

路2 查 SQL 拿密码明文？
   SELECT ...password... ◄── 没有任何 SQL 返回密码明文
   结果：堵死（哈希要暴力破解；受限账号连哈希都读不到）

路3 读 db.json？
   容器里没有这个文件 ◄── 未挂载；host 上是 0600 密文
   结果：堵死

路4 读密钥 / 进程内存？
   容器里没有密钥 ◄── 不同 VM（Docker 隔离）；同主机同账号场景不设防（信任边界）
   结果：Docker 下堵死；同主机靠"AI 不主动读"约定
```

### 流程 4 · 密钥层级（谁保护谁）

```
系统密钥库（macOS Keychain / Windows 凭据管理器 / Linux Secret Service）
   │
   ├─ apollo 快照密钥 ────────── 加密 → Apollo 快照的「非敏感值」
   ├─ sensitive 敏感值密钥 ───── 加密 → 快照里的「敏感值」
   └─ db 数据库密钥(VAULTY_KEEPER_DB_KEY) ─ 加密 → db.json 里的「真实数据库 URL」
        │
        ▼ 单独泄露的影响
   apollo 密钥泄露   → 能解非敏感快照值，但解不开敏感值、解不开 db.json
   sensitive 密钥泄露 → 能解敏感值，但解不开 db.json
   db 密钥泄露       → 能解数据库 URL（最值钱，所以独立）
```

### 流程 5 · serve 启动时序

```
vaulty-keeper serve --addr 0.0.0.0:8972
   │ ① 生成 128 位随机全局 bridge token → 写 ~/.vaulty/bridge-token（0600）+ 打印
   │    （掩码桥 /api 用；每条连接的隧道 token 由 db add 生成、db regen 轮换）
   ▼
   │ ② 读 db.json（DB Key 解密）→ 每个连接开一个 TCP 隧道
   │    pgdb :15432 / mysqltest :15435 / mysqlnative :15436 / cache :15434
   ▼
   │ ③ 起 HTTP 掩码桥 :8972（/api/* 全部要 token + 失败限速）
   ▼
就绪：4 隧道 + 1 桥，等待连接
   │ 每次连接 → 审计日志（authenticated / invalid token，无 SQL/凭据）
   ▼
Ctrl-C / 退出 → 隧道与桥关闭，内存中的真实 URL 消亡
```

### 流程 6 · Apollo + Docker：AI 在容器里读 / 对比配置

```
Host 侧（先准备好，AI 看不到这些）：
  vaulty-keeper apollo init / sensitive init    ← 快照密钥 + 敏感值密钥进 Keychain
  vaulty-keeper apollo import prod.txt --app-id xx
       │ 明文只在"你手动导入"这一刻出现，之后全部加密落盘
       ▼
  ~/.vaulty/apollo/prod__xx.json（0600 密文）◄── 磁盘无明文，容器未挂载
       ▼
  vaulty-keeper serve --addr 0.0.0.0:8970        ← host 持有密钥，永远只回掩码
       │
       ▼
容器侧（AI 视角，每次操作都经桥）：
  vaulty-keeper remote list prod --appid xx
       │ ① AI 问 serve："prod 有哪些 key？"（带 token）
       ▼
  serve
       │ ② 用 Keychain 密钥解密快照 → 每个值算「掩码 + 长度 + 指纹」
       ▼
  AI 拿到：APP_NAME   = *** (5 chars)  [51650fd5fb747230]
           DB_PASSWORD = *** (11 chars) [afd76e19e7393961]
       │ 看不到明文，但长度 + 指纹够用
       ▼
对比判断（AI 的核心工作）：
  vaulty-keeper remote compare prod test --appid xx --appid-to xx
       ▼
  ~ DB_PASSWORD: *** (11 chars) [afd76e19] -> *** (11 chars) [b5a112e5]
       │ 指纹不同 = 内容不同（即使长度一样）
       ▼
  AI 结论：DB_PASSWORD 两环境不一致、LOG_LEVEL 也不一致 → 汇报人类
       │
       ▼
边界：remote 是只读的 ◄── 桥没有 set/unset/import 端点；
      容器里的 AI 永远只能"掩码读 + 对比判断"，改配置留在 host 命令行/UI
```

---

## 附 · Docker 在项目里的两个角色

项目里的 Docker 文件是**两种独立用途**，别混在一起：

### 角色 A：测试数据库（`scripts/dbtest.sh`）
起 PG/MySQL/Redis **数据库容器**给隧道当靶子。只服务于本地验证，不参与任何 vaulty-keeper 逻辑；serve 把它们当普通远端 DB 连。

### 角色 B：隔离 AI agent（`Dockerfile` + `docker-compose.yml` + `docker/agent-entrypoint.sh`）
把 **AI 本身**关进容器，让 AI 摸不到 host 的密钥/密文——这是防"故意对抗的 AI"的唯一可靠办法。

```
Dockerfile（两阶段）
  阶段1 golang → go build 出 vaulty-keeper 二进制
  阶段2 node（agent CLI 是 npm 包）+ git + 非 root 用户 agent

docker-compose.yml（隔离要点）
  cap_drop: ALL             容器无内核特权
  no-new-privileges         无法提权
  volumes: 只挂项目目录      不挂 ~/.vaulty / Keychain / ~/.ssh / docker.sock
  env: 只有 BRIDGE_ADDR/TOKEN（没有密钥）
  extra_hosts: host.docker.internal:host-gateway   Linux 兼容（Docker Desktop 无影响）
  volumes: agent-home:/home/agent                   持久化 CLI/历史（重建不丢）

agent-entrypoint.sh
  按 VAULTY_KEEPER_INSTALL_AGENTS 自动 npm 装 codex/claude/opencode，再开 shell
```

容器里同时具备两种能力（都经 host 的 serve）：
- **Apollo 掩码读**：`vaulty-keeper remote list|get|compare` → 只有 `*** (n chars)` + 指纹
- **DB 隧道**：`db list` 拿隧道端口 → psql/mysql/redis-cli 连 `host.docker.internal:端口`，隧道 token 当用户名/AUTH（连接专属，`db connect` 打印；旧连接回退 bridge token）

**边界提醒**：Docker 是"防绝大多数 AI 主动拿密钥"的强隔离，但不是绝对隔离（daemon 是 root 服务，容器逃逸是真实攻击面）。极高威胁等级应升级到独立账号 / VM / 云沙箱（README「不用 Docker 的替代用法」）。

### 什么时候用哪个

| 场景 | 用什么 |
|---|---|
| 本地验证 DB 隧道 | `scripts/dbtest.sh`（需本机 Docker Desktop） |
| 防"会主动读密钥"的 AI | `docker compose up -d`（host 先 `serve --addr 0.0.0.0:8970` + 导 token） |
| 只防"守规矩"的 AI | 不用 Docker，host 直接 serve |
| 真隔离但不用 Docker | 独立 macOS 账号 / VM（README「不用 Docker 的替代用法」） |
| 极高威胁 / 合规审计 | 独立账号 / VM / 云沙箱（Docker 隔离之上再加一层） |
| 生产 / 云 | 不用 Docker：隧道纯 TCP，DB 可以是云 RDS，AI 放任意隔离域 |

### 两个角色怎么串起来

```
host: vaulty-keeper serve（掩码桥 + DB 隧道）  ← 裸跑，与 Docker 无关
Docker 里：[角色B agent容器: AI] --db list--> [隧道端口] --token--> 真实 DB
                                     （真实 DB = 角色A 的容器 / 内网 / 云 RDS 均可）
```

---

## 附 · AI 用隧道到底能拿到什么（实测）

关键原则：**隧道 = 把"注册账号的完整权限"交给 AI**。注册 URL 里那个账号的权限，就是 AI 在隧道里的权限上限。

| 想拿的东西 | 能不能拿到 | 说明 |
|---|---|---|
| 密码明文 | ❌ 拿不到 | 认证交换在 host 完成，客户端只收到"成功/失败"；SQL 也没有返回密码明文的途径；日志/回包已实测无密码 |
| 密码哈希 | ⚠️ 看账号权限 | 注册高权账号（如超级用户）→ 能读 `pg_authid`/`mysql.user` 拿哈希（还原仍需暴力破解）；注册受限只读账号 → `permission denied` ✅ |
| 真实账号名 | ⚠️ 必然可见 | `SELECT current_user` / `CURRENT_USER()` 返回会话属主——"给真实会话"的固有属性，无法同时隐藏。缓解：注册**专用只读账号**（如 `app_ro`），名字本身不敏感 |
| 真实地址 | ⚠️ 部分可见 | `inet_server_addr()`/`inet_server_port()` 会返回真实服务器地址（docker 场景是容器内网 IP）；hostname/映射端口不出现在配置与日志 |
| 写数据/删数据 | ⚠️ 看账号权限 | 只读账号 → 被拒；有写权限的账号 → 放行（代理不强制只读） |

实测（受限账号 `app_ro`，仅 SELECT）：
```
SELECT current_user            → app_ro            （账号名可见）
SELECT rolpassword FROM pg_authid → permission denied for table pg_authid   ✅
INSERT INTO t ...                → permission denied for table t             ✅
SELECT count(*) FROM t           → 2                （正常查询不受影响）
```

**结论**：密码（明文）在任何情况下都不出 host；哈希/写权限等能否拿到，完全由你注册的账号决定。所以安全使用的**前置条件**是：`vaulty-keeper db add` 时注册一个**专用只读、最小权限**的账号，而不是拿高权账号去注册。


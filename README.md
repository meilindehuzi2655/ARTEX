<div align="center">

# ARTEX 二开版

**AI 自主渗透测试 · 源码代码审计 · 小程序代码审计**

Go 1.26 + Next.js 16 + PostgreSQL · 单二进制内嵌前端

[本仓库](https://github.com/meilindehuzi2655/ARTEX) ·
[上游 RuoJi6/ARTEX](https://github.com/RuoJi6/ARTEX) ·
[AGPL-3.0](LICENSE)

</div>

---

## ✨ 二开亮点

> 保留上游的双图引擎、planner/worker 协作、三级审批与流量录制，把 ARTEX 从「黑盒渗透」扩展为
> 「渗透 + 源码审计 + 小程序审计」的一体化平台。

| 🧭 三种任务模式 | 🧪 本地靶场 | 🪟 Windows 友好 |
| :---: | :---: | :---: |
| **渗透测试 / 代码审计 / 小程序审计**<br>创建任务即选，planner 与 worker 自动切换审计立场、产出规约与资产关联方式 | **`lab_up` 一次性环境**<br>用 Docker 或 WSL2 在 `127.0.0.1` 起靶场，把静态发现升级为带流量证据的动态验证 | **Git Bash + WSL2**<br>Bash 工具可切 Git Bash，靶场支持 WSL2 内的 Docker Engine，不再受 PowerShell 限制 |

| 创建任务时选择模式（图中为「代码审计」） | Windows：Bash 工具切换 Git Bash |
| :---: | :---: |
| ![任务模式选择](screenshots/fork-task-modes.png) | ![Git Bash 设置](screenshots/fork-git-bash.png) |

| 本地靶场：两次 `lab_up`（第二次 `reuse=true`）返回同一环境 |
| :---: |
| ![本地靶场](screenshots/fork-local-lab.png) |

| 任务列表直接标注模式 |
| :---: |
| ![任务列表模式徽章](screenshots/fork-task-list.png) |

**还有：**

- `extract_archive`：解包 `.zip` / `.tar.gz`，带 zip-slip 防护、5 万条目 / 2GiB 上限；
- 内置 `code-audit` / `miniprogram-audit` 技能，自带 PHP / Java / Node / Python 靶场模板；
- 修复 Anthropic Base URL 以 `/v1` 结尾时拼成 `/v1/v1/messages`、`graph_overview` 缺失任务 scope 等问题。

### 上游 vs 本二开版

| 能力 | 上游 | 本二开版 |
| --- | :---: | :---: |
| 黑盒渗透（资产测绘 / 漏洞验证） | ✅ | ✅ |
| 源码代码审计模式 | — | ✅ |
| 小程序代码审计模式 | — | ✅ |
| 本地靶场（Docker / WSL2） | — | ✅ |
| `extract_archive` 解包工具 | — | ✅ |
| Windows Git Bash 配置 | — | ✅ |
| 审计技能与靶场模板 | — | ✅ |
| 双图引擎 / 三级审批 / 流量录制 | ✅ | ✅（保留） |

---

## 二开说明

本仓库是 [RuoJi6/ARTEX](https://github.com/RuoJi6/ARTEX) 的独立二开分支（上游源自
[Autumn-27/ARTEX](https://github.com/Autumn-27/ARTEX)）。二开改动的详细说明见
[二开版新增能力](#二开版新增能力) 与 [二开改动](#7-二开改动)。

当前分支版本标记：`0.4.5-lab`（构建时通过 `-X main.version=0.4.5-lab` 注入；未注入时显示 `dev`）。

> [!IMPORTANT]
> **本仓库不发布 Release、预编译包或 Docker 镜像。**
> 页面上的「一键更新」在代码里固定指向 `RuoJi6/ARTEX`（`selfupdate/github.go`），
> 在本二开版上使用会把程序替换成上游正式版；当前版本标记 `0.4.5-lab` 不是正式版本，
> 页面会直接禁用该按钮。除非你修改该常量并自行发布 Release，否则请忽略一键更新。
> `docker-compose.yml` 默认拉取的也是上游镜像 `ruoji6/artex`，
> 要运行本二开版的代码，请按「从源码构建」或自行构建镜像。

---

## 更多界面

> 以下为上游界面截图；二开版新增的模式选择器、模式徽章与 Git Bash 设置见上方「二开亮点」。

| 仪表盘 | 任务列表 |
| :---: | :---: |
| ![仪表盘](screenshots/dashboard.png) | ![任务](screenshots/tasks.png) |

| 探索链路 | 发现 |
| :---: | :---: |
| ![探索链路](screenshots/graph.png) | ![发现](screenshots/findings.png) |

---

## 目录

- [二开亮点](#-二开亮点)
- [二开说明](#二开说明)
- [更多界面](#更多界面)
- [二开版新增能力](#二开版新增能力)
- [功能总览](#功能总览)
- [快速开始](#快速开始)
- [配置](#配置)
- [使用指南](#使用指南)
- [系统架构](#系统架构)
- [开发与测试](#开发与测试)
- [升级与二开](#升级与二开)
- [安全与合规](#安全与合规)
- [许可与致谢](#许可与致谢)

---

## 二开版新增能力

### 1. 任务模式

创建任务时选择模式，模式只改变注入给 planner/worker 的指令与默认值，不改变引擎结构。

| 模式 | 适用场景 | 关键行为 |
| --- | --- | --- |
| `pentest`（默认） | 黑盒渗透测试 | 资产测绘、网络侦察、漏洞验证；资产覆盖度默认开启 |
| `code_audit` | 上传的源码包（Java/PHP/Python/JS 等） | 只读审计、不访问真实目标；按模块拆分意图；覆盖度默认关闭；可用本地靶场 |
| `miniprogram_audit` | 反编译后的小程序源码 | 按页面/分包/公共模块/网络层拆分；重点检查 AppSecret 硬编码、API 端点、登录态与支付逻辑 |

审计模式会：

- 跳过创建任务时的目标 seed，避免把 `xxx.zip` 误判成线上站点种进资产图；
- 在 planner/worker 系统提示中追加审计立场、产出规约与资产关联规约（`agent/modeblock.go`）；
- 默认关闭资产覆盖度（可在高级设置里手动打开）；
- 只给 worker 接线 `lab_*` 靶场工具，planner 不直接起环境。

### 2. 代码审计工作流

```text
上传源码包 → extract_archive 解包 → planner 按模块拆分意图
    → worker 只读审计（Read/grep）→ record_fact / report_finding
    → 需要时 lab_up 起本地环境 → 动态验证 → traffic 证据 → lab_down
    → reporter 汇总报告
```

审计任务的核心约束（写入系统提示与 `code-audit` 技能）：

- 不修改被审计源码，中间产物只写当前意图工作目录；
- 不访问代码里出现的线上地址，只把它们登记为资产；
- 允许访问本任务自建的 `127.0.0.1` / `localhost` 靶场；
- 漏洞证据必须包含 `文件路径:行号` + 关键代码片段 + 影响说明；
- 静态发现标注「静态发现，待验证」，动态验证成功的标注「动态验证（本地靶场）」。

### 3. 小程序代码审计

`miniprogram-audit` 技能面向**反编译后的产物**（`app.js` / `app.json` / `pages/` 等结构），
不负责 `wxapkg` 解包。重点覆盖：

- 后端 API 端点与域名提取，登记为资产；
- `AppSecret`、云开发密钥、硬编码 token 等敏感信息；
- 登录态、支付、用户信息相关的前端校验与越权风险；
- 分包与公共模块中的共享逻辑。

### 4. 本地靶场

审计任务可以把源码跑在一次性容器里做动态验证：

| 工具 | 作用 |
| --- | --- |
| `lab_status` | 查看当前任务的靶场与容器运行时是否可用 |
| `lab_up` | 启动靶场（compose 或 image 模式），返回 `lab_id` 与 `http://127.0.0.1:<port>` |
| `lab_down` | 释放引用；最后一个使用方释放时才真正销毁，`force=true` 立即销毁 |

运行方式：

- **Windows / macOS**：本机 Docker Desktop（`docker version` 能连上 daemon）；
- **Windows + WSL2**：WSL 发行版内运行 Docker Engine，平台会自动探测并通过
  `wsl -d <distro> -u root -- docker ...` 调用；
- **Linux**：本机 Docker Engine。

compose 模式会把以下变量注入 compose 进程：

```yaml
services:
  web:
    image: php:8.2-apache
    volumes:
      - ${LAB_SOURCE}:/var/www/html:ro
    ports:
      - "${LAB_BIND}:${LAB_PORT_1}:80"
```

- `${LAB_BIND}`：绑定地址（原生后端为 `127.0.0.1`，WSL 后端为 `0.0.0.0`）；
- `${LAB_PORT_1}..${LAB_PORT_N}`：自动分配的宿主端口；
- `${LAB_SOURCE}`：`source_path` 转换后的路径（WSL 下自动转成 `/mnt/<drive>/...`）。

其他行为：

- 默认资源限额：内存 2G、2 CPU、1024 pids；
- 多意图复用：`lab_up(key=应用名)` + 后续 `reuse=true` 拿到同一环境，按 holder 引用计数；
- 破坏性验证（删数据、改配置、上传文件、压测）必须单独起环境，不要复用共享靶场；
- 平台每 5 分钟 GC 一次：任务终态/删除或靶场超过 4 小时自动回收；
- 删除任务时会异步清理该任务的全部靶场容器、网络与数据卷。

`skills/code-audit/templates/` 自带 PHP/Java/Node/Python 四套 compose 模板，可直接复制修改。

### 5. Windows / Git Bash

Windows 默认的 Bash 工具使用 PowerShell，`grep`/`sed` 等 Unix 命令不可用。可在
**系统配置 → 运行环境 · Shell（Git Bash）** 中填写：

- `bash.exe` 的完整路径，例如 `C:\Program Files\Git\bin\bash.exe`；
- 或 Git Bash 根目录，平台会自动探测 `bin/bash.exe`、`usr/bin/bash.exe`。

保存后立即生效；路径非法会当场返回错误并保留原配置。留空则恢复平台默认。
该设置对所有 Agent 的 Bash 工具生效。

### 6. 新增工具

| 工具 | 绑定 | 说明 |
| --- | --- | --- |
| `extract_archive` | worker | 解压 `.zip` / `.tar` / `.tar.gz` / `.tgz` / `.tar.bz2`，带 zip-slip 防护、5 万条目与 2GiB 上限，跳过符号链接 |
| `lab_up` / `lab_down` / `lab_status` | worker（仅审计任务接线） | 本地靶场生命周期与状态查询 |

### 7. 二开改动

| 位置 | 改动 |
| --- | --- |
| `agent/modeblock.go` | 任务模式指令块（planner / worker / goals 三个视角） |
| `agent/lab.go`、`server/lab.go` | 靶场接口、Docker/WSL2 后端、引用计数、GC |
| `agent/tools_archive.go` | `extract_archive` 解包工具 |
| `server/shell.go` | Bash 工具 shell 配置的解析、校验与热应用 |
| `db/tasks.go`、`db/schema.sql` | `tasks.mode` 字段与模式归一 |
| `server/goals.go`、`server/task_llm.go` | 模式指令注入、审计任务跳过 seed、靶场接线 |
| `web/.../tasks/page.tsx` | 创建任务时的模式选择器与任务列表模式徽章 |
| `web/.../settings/page.tsx` | Git Bash shell 配置卡片 |
| `agent/provider.go` | 修复 Anthropic Base URL 以 `/v1` 结尾时拼成 `/v1/v1/messages` |
| `agent/tools.go` | `graph_overview` 补回任务 scope（含企业范围展开与关键词） |

---

## 功能总览

| 能力 | 说明 |
| --- | --- |
| 自主渗透 | 目标分解 → planner 派意图 → worker 执行真实工具 → 写回双图，事件驱动闭环 |
| 源码审计 | 上传压缩包，解包后按模块静态审计，漏洞带文件行号与代码证据 |
| 小程序审计 | 反编译产物审计，提取 API、密钥、登录与支付风险 |
| 本地靶场 | Docker/WSL2 一次性环境，动态验证静态发现，流量可绑定为证据 |
| 双图 | 全局资产图 + 每任务探索图，锚点关联，支持资产覆盖度与覆盖图 |
| 人在环路 | 主 Agent 对话、提示、暂停/恢复、中途纠偏（steer） |
| 三级审批 | 规则匹配 → LLM 兜底裁判 → 人工审批，支持超时策略 |
| 流量录制 | 内嵌 MITM 代理，HTTP 全量留痕，支持全文检索与证据快照 |
| 漏洞管理 | 上报、归并、统一报告、手动复测、IM/邮件推送、任务情报 |
| 扩展 | MCP、Skills、自定义工具、ScopeSentry 资产同步 |

---

## 快速开始

### 环境要求

| 依赖 | 版本 / 说明 |
| --- | --- |
| PostgreSQL | 16（唯一必需的外部服务） |
| LLM | Anthropic / OpenAI 兼容接口，可在 UI 配置 |
| Go | 1.26+（源码构建） |
| Node.js | 22.x（前端构建） |
| Docker | 可选，本地靶场需要 Docker Desktop 或 WSL2 内的 Docker Engine |

### 方式一：从源码构建（推荐）

```bash
git clone https://github.com/meilindehuzi2655/ARTEX.git
cd ARTEX

# 1) 数据库配置
cp config.example.json config.json
#    编辑 database.host/port/user/password/dbname；也可用 ARTEX_PG_DSN 覆盖

# 2) 构建前端并内嵌到二进制
cd web && npm ci && npm run build:static && cd ..
mkdir -p server/webui/dist
cp -r web/out/. server/webui/dist/

# 3) 编译
CGO_ENABLED=0 go build -tags embedui -trimpath \
  -ldflags "-s -w -X main.version=0.4.5-lab" \
  -o artex ./cmd/artex

# 4) 启动（守护脚本；Windows 用 start.bat）
./start.sh
```

> `./build.sh` 也做同样的事，并支持交叉编译：
> `ARTEX_OUTPUT=./artex ./build.sh`、`./build.sh --release`。
> 它依赖 `npm`、`rsync`、`zip`；Windows Git Bash 没有 `rsync` 时请用上面的手动命令。

首次启动后打开 **http://localhost:8787**，按页面提示在 `/setup` 设置管理员密码。

### 方式二：Docker Compose

`docker-compose.yml` 默认使用上游镜像 `ruoji6/artex`。要运行**本二开版**，先构建自己的镜像：

```bash
# 先完成「方式一」的第 2 步（web/out → server/webui/dist），再交叉编译 Linux 二进制
CGO_ENABLED=0 GOOS=linux GOARCH=amd64 \
  go build -tags embedui -o dist/amd64/artex ./cmd/artex

docker build -t <你的镜像名>/artex:0.4.5-lab .

cp .env.example .env
# 编辑 .env：ARTEX_IMAGE=<你的镜像名>/artex，ARTEX_TAG=0.4.5-lab
#            并设置 POSTGRES_PASSWORD
docker compose up -d
```

启动后访问 **http://localhost:8787**。`./data` 与 `./skills` 以绑定挂载持久化。

> 只想快速体验上游功能时，可以直接 `docker compose up -d` 使用 `ruoji6/artex` 镜像，
> 但那是上游代码，不包含本仓库的审计模式与靶场改动。

### 方式三：开发模式

```bash
./dev.sh
# 后端 :8787 / 流量代理 :8788 / 前端 http://localhost:5173
```

前后端分离热更新；前端 `/api` 反代到后端。

### 首次启动检查

```bash
curl http://127.0.0.1:8787/api/health
# {"ok":true,"service":"artex","version":"0.4.5-lab"}
```

- 首次进入 `/setup` 设置管理员密码；
- 在 **LLM 配置** 页添加并激活一个可用配置；
- 如需审计靶场，确认 `docker version` 或 `wsl -d <distro> -u root -- docker version` 可用。

---

## 配置

### config.json

只包含数据库与技能目录两项；其余运行时配置存 PostgreSQL，可在 UI 修改。

```json
{
  "database": {
    "host": "127.0.0.1",
    "port": 5432,
    "user": "artex",
    "password": "yourpass",
    "dbname": "artex",
    "sslmode": "disable"
  },
  "skill_dir": "./skills"
}
```

也可以用完整 DSN：

```json
{ "database": { "dsn": "postgres://artex:yourpass@127.0.0.1:5432/artex?sslmode=disable" } }
```

### 环境变量

| 变量 | 说明 |
| --- | --- |
| `ARTEX_CONFIG` | 指定 config.json 路径 |
| `ARTEX_PG_DSN` | PostgreSQL DSN，优先级高于 config.json |
| `ARTEX_SKILL_DIR` | 技能根目录，优先级高于 config.json |
| `ANTHROPIC_API_KEY` / `OPENAI_API_KEY` | LLM 密钥（也可在 UI 配置） |
| `ARTEX_LLM_PROVIDER` | `anthropic` / `openai` / `openai-responses` |
| `ARTEX_LLM_MODEL` | 模型名 |
| `ARTEX_LLM_BASE_URL` | 接口 Base URL |
| `ARTEX_LLM_PROXY` | LLM 请求出口代理 |

### 端口

| 端口 | 用途 |
| --- | --- |
| `8787` | Web UI + REST API + SSE |
| `8788` | 流量录制 MITM 代理（默认仅本机） |

启动参数：`./start.sh -addr :8787 -proxy 127.0.0.1:8788 -data ./data`。

### 数据目录

`data/` 由程序自动创建，包含：

- `jwt.key`：JWT 签名密钥（首次启动生成，**不要提交**）；
- `traffic/`：流量索引 SQLite、blob 库、CA 证书；
- `evidence/`：漏洞证据快照；
- `sessions/`、`transcripts/`：Agent 会话与转录；
- `drafts/`：上传的源码包等草稿文件；
- `labs-state.json`：靶场引用计数状态。

### LLM 配置

优先级：**数据库持久配置 > 环境变量**。在「LLM」页添加配置后激活即可；支持多个配置按顺序故障转移、
独立限流、用量记账。Anthropic 格式的 Base URL 现在兼容 `https://host`、`https://host/v1`、
`https://host/v1/messages` 三种写法。

### 访问控制与反向代理

- **HTTP Basic Auth**：系统配置页可开启独立访问验证，密码只存 bcrypt 哈希；
- **JWT**：登录后签发 7 天 token；
- **反向代理**：前端与 API/SSE 同源，无需配置 `NEXT_PUBLIC_SSE_BASE`；反代必须关闭缓冲
  （`proxy_buffering off`）并放宽读超时，否则 SSE 会一直转圈。

---

## 使用指南

### 新建任务

1. 在「任务」页点击 **新建任务**；
2. 选择任务模式（渗透测试 / 代码审计 / 小程序审计）；
3. 填写名称、描述、目标；
4. 审计任务上传源码压缩包（`.zip` / `.tar.gz`），上传后绝对路径会追加到描述末尾；
5. 可选：关联企业资产范围、来源任务、任务级拦截规则、LLM 配置链；
6. 创建后 planner 会先拆解目标，再按模式派发意图。

### 审计任务流程

1. **解包**：worker 调用 `extract_archive` 把压缩包解到工作目录，返回 `top_level` 供判断站点根；
2. **梳理**：按顶层目录/模块拆分审计面，planner 一条意图负责一块；
3. **审计**：worker 只读代码，发现即写回：
   - 代码事实 → `record_fact`（evidence 写 `文件:行号`）；
   - 漏洞 → `report_finding`（vulnclass + evidence + 影响说明）；
   - 端点/域名/appid → `insert_assets` 登记并归并到全局资产图；
4. **动态验证（可选）**：
   - `lab_status` 确认运行时可用；
   - `lab_up` 起环境（compose 推荐，模板在 `skills/code-audit/templates/`）；
   - 用 `curl` 等工具验证，`traffic_search(host=127.0.0.1)` 找到流量 id；
   - `report_finding` 带 `traffic_refs` 绑定证据，标注「动态验证（本地靶场）」；
   - `lab_down` 清理；
5. **收尾**：planner 判定目标是否全部覆盖，reporter 生成报告。

### 本地靶场注意事项

- 靶场只监听本机回环；WSL 后端通过 `0.0.0.0` + WSL localhost 转发访问；
- 首次 `docker compose up` 可能需要拉镜像/构建，超时上限 15 分钟；
- 同一 `key` 的只读验证可复用环境；破坏性验证请用独立 `lab_up`；
- 靶场状态持久化在 `data/labs-state.json`，重启后引用计数不丢；
- 平台 GC 与任务删除都会清理容器、网络和数据卷。

### 主要页面

| 页面 | 内容 |
| --- | --- |
| 仪表盘 | 活跃任务、漏洞统计、资产/流量计数、Token 用量 |
| 任务 | 任务列表、模式徽章、状态/分类筛选、批量操作、新建任务 |
| 任务详情 | 会话、总览、探索链路、播报板、发现、任务情报、复测、测试资产、资产覆盖图、拦截审批、报告 |
| 发现 | 跨任务漏洞汇总、按漏洞/原始上报/任务/资产视图、归并与统一报告 |
| 流量 | MITM 录制列表、内容检索、路径/状态码/长度筛选 |
| 资产 | 企业范围、资产树、覆盖图、情报关联 |
| 系统 | LLM、Agent、MCP、Skill、工具、通知推送、拦截规则、审批记录、日志、系统配置 |

---

## 系统架构

### 双图

- **资产图（全局共享）**：`root_domain / subdomain / ip / service / app / endpoint`，归属企业，
  跨任务复用，是资产真值库。
- **探索图（每任务独立）**：`goal / intent / fact / finding / hint / digest`，用
  `spawns / derived_from / yields / proves / covers` 连成血缘链。
- **锚点**：`exploration_anchors(node_id, asset_id)` 把探索节点挂到资产上，支撑覆盖度、
  资产覆盖图与跨任务继承。

```mermaid
flowchart LR
  subgraph EG["探索图（每任务）"]
    G[goal] -->|spawns| I[intent]
    I -->|yields| F[fact]
    F -->|derived_from| I2[intent]
    I2 -->|proves| FD[finding]
  end
  subgraph AG["资产图（全局）"]
    RD[root_domain] --> SD[subdomain] --> SV[service] --> EP[endpoint]
  end
  I -. anchor .-> SD
  F -. anchor .-> SV
  FD -. anchor .-> EP
```

### 四角色

| 角色 | 职责 |
| --- | --- |
| Goals | 创建任务时一次性把目标拆成 goal + 约束 + 初始范围；审计模式按审计范围拆分 |
| Planner | 事件驱动（图变防抖 800ms）读取态势、判目标、派意图；持有跨唤醒共享 todolist |
| Worker | 领一条意图，用真实工具执行，边发现边写回，做完即停，不自产新方向 |
| MainAgent | 人在环路：观察态势、加提示、加意图、纠偏、暂停/恢复 |

### 审批瀑布

```text
工具调用 → 规则匹配（intercept_rules）
        → 无命中 → LLM 兜底裁判
        → 判不准 → 人工审批（超时按配置 allow/deny）
```

审查输入刻意不包含任务描述、目标与历史，避免提示注入扩大授权；平台拦截消息带
「【ARTEX 平台管控·非目标防御】」前缀，防止 Agent 误判为目标 WAF 而绕过。

### 流量与证据

- 内嵌 MITM 代理（`8788`）记录 HTTP 流量，SQLite 索引 + blob 溢写 + FTS5 检索；
- `report_finding` 绑定流量时，会把内容写入独立证据库（内容寻址 + SHA-256），
  之后删除流量不影响证据；
- 删除任务/流量时跨 PG、SQLite、文件系统协同，带 staging 与崩溃恢复。

### 上下文治理

- planner 每轮维护冷热图，连续冷节点折叠为 `digest`，按需 `expand_digest` 还原；
- `graph_overview` 只给「最新一窗 + 计数兜底」，避免长任务上下文膨胀；
- 二开版在 coverage 块中补回 `scope`（含企业范围展开），让 Agent 始终看得到授权边界。

---

## 开发与测试

### 常用命令

```bash
# 后端
go vet ./...
go test ./...          # 需要可用的 PostgreSQL；建议使用独立库/独立 schema

# 指定测试库（示例）
export ARTEX_PG_DSN='postgres://artex:pass@127.0.0.1:5432/artex_test?sslmode=disable'
go test ./...

# 前端
cd web
npm ci
npm run dev            # 开发服务器
npm run build:static   # 静态导出
npm run check          # biome 检查
```

### 构建

```bash
# 当前平台，输出到 dist/artex-<os>-<arch>/artex
./build.sh

# 指定输出路径
ARTEX_OUTPUT=./artex ./build.sh

# 跨平台 Release 压缩包（需要 zip；可选 UPX）
./build.sh --release
ARTEX_TARGETS=linux/amd64,windows/amd64 ./build.sh --release
```

### 目录结构

```text
cmd/artex/         程序入口、启动流程、信号与退出码
agent/             goals / planner / worker / mainagent、工具装配、模式指令、靶场接口
server/            HTTP 路由、SSE、任务管理、审批、靶场后端、shell 配置
db/                PostgreSQL schema 与各 store（资产、探索、任务、归档、通知…）
guard/             工具调用前置守卫
intercept/         规则匹配、LLM 裁判、人工审批、脚本快照
traffic/           MITM 录制、SQLite 索引、blob、证据
evidence/          漏洞证据快照
llmpool/ llmrec/   LLM 故障转移池与用量/报文记账
mcphttp/           MCP Streamable HTTP / SSE 客户端
skills/            内置技能：api-recon、code-audit、miniprogram-audit、playwright-cli、scopesentry
web/               Next.js 前端（静态导出后嵌入二进制）
third_party/norma/ 本地 fork 的 Agent SDK（Windows shell 钩子）
```

---

## 升级与二开

### 本二开版如何升级

本仓库没有 Release，页面一键更新也不指向本仓库。升级方式：

```bash
git pull
# 重新构建前端与二进制（见「快速开始」），然后重启 ./start.sh
```

数据库迁移是幂等的：每次启动会重跑 `db/schema.sql`，无需手工执行 SQL。升级前建议备份
`data/` 与 PostgreSQL 数据库。

### 如果想让一键更新指向自己的仓库

1. 修改 `selfupdate/github.go` 中的 `const Repo = "RuoJi6/ARTEX"` 为你的仓库；
2. 按 `.github/workflows/release.yml` 的约定发布正式 Release（`vX.Y.Z` + 五平台 zip + `SHA256SUMS`）；
3. 页面「版本与更新」才会检查并安装你自己的版本。

> 发布前务必核对 `SHA256SUMS` 与 Release 资产，避免把半成品推给运行中的实例。

### Docker 镜像

`docker-compose.yml` 与 `.env.example` 默认指向 `ruoji6/artex`。要发布自己的镜像：

```bash
docker build -t <你的用户名>/artex:0.4.5-lab .
docker push <你的用户名>/artex:0.4.5-lab
# 在 .env 设置 ARTEX_IMAGE / ARTEX_TAG
```

### 开发约定

- 新增页面：`web/src/app/(main)/function/<name>/page.tsx` + `sidebar-items.ts` + `api.ts`/`types.ts`；
- 新增任务详情 Tab：`tasks/detail/_tabs/` + `detail/page.tsx` 的 TABS；
- 新增 Agent 角色：`agent/promptcatalog.go` + `db/db.go` 的 agents seed + tools 绑定；
- 新增工具：`agent/tools*.go` + `toolcatalog.go` + `db/schema.sql` 的 tools seed；
- 改 schema：`db/schema.sql` 必须幂等（`IF NOT EXISTS` / `DO $$`）；
- 新增拦截规则：`db/db.go` 的版本化 seed + settings 门控标志。

---

## 安全与合规

- 本工具仅用于**已获得明确授权的安全测试**、本地靶场与代码审计研究；使用者须自行确保
  测试范围、授权文件与当地法律法规要求。
- 审计任务默认不访问真实目标；本地靶场只监听 `127.0.0.1`，不要把它暴露到公网。
- 建议为平台开启 HTTP Basic Auth + HTTPS，并限制 `8787`/`8788` 的来源。
- 不要提交 `config.json`、`.env`、`jwt.key`、`data/` 或任何真实密钥；仓库的 `.gitignore`
  已排除这些路径。
- 平台内置三级审批与破坏性命令规则，但**不能替代授权与人工复核**。

---

## 许可与致谢

本项目沿用上游的 **GNU Affero General Public License v3.0（AGPL-3.0）**，完整条款见
[LICENSE](LICENSE)。衍生作品必须以 AGPL-3.0 开源；若修改后通过网络提供服务，也须向用户
公开对应源码。

致谢：

- [RuoJi6/ARTEX](https://github.com/RuoJi6/ARTEX) 与 [Autumn-27/ARTEX](https://github.com/Autumn-27/ARTEX)：原始项目与架构；
- [Autumn-27/norma](https://github.com/Autumn-27/norma)：Agent SDK；
- 以及上游 README 中列出的所有依赖与参考项目。

> 本项目按“现状（AS IS）”提供，不附带任何明示或默示担保。因使用本项目产生的一切后果由使用者自行承担。

---
name: code-audit
description: 对上传的源码压缩包做静态代码审计时调用该skill。
---

# Code Audit（源码静态代码审计）

对任务描述中给出的上传源码（已解包目录或压缩包）做系统性静态审计。覆盖
Java/JSP、PHP、Python、JS/Node/前端四类技术栈。

## 边界与禁止（硬规约）

| 事项 | 规则 |
|---|---|
| 修改被审计代码 | **禁止**。只读（Read/grep/awk），中间产物写本意图工作目录 |
| 访问目标线上环境 | **禁止**。代码中的后端地址只登记为资产，不发任何针对真实目标的验证请求 |
| 本地靶场 | **允许**（例外）：可用 `lab_up` 在 127.0.0.1 上搭建本任务的一次性环境做动态验证；只允许访问本任务自建的 127.0.0.1/localhost 端口。破坏性验证必须单独起环境，不复用共享靶场 |
| 网络侦察 | **禁止**。不端口扫描、不目录爆破、不子域枚举 |
| 上传文件到外部 | **禁止**。代码内容不出本机 |
| 修改既有资产数据 | 只允许 insert_assets（upsert 追加语义）与 report_finding/record_fact |

## 与资产图的关联工作流（硬规约）

1. **开工预检**：`list_assets` 检索代码中出现的域名/appid/后端 URL 是否已有资产。
   已有的直接复用其 technologies/auth/关联 findings 作为审计线索。
2. **发现即登记**：确认的后端 API 端点用 `insert_assets`（type=endpoint，url+method，
   params 记参数）；后端域名用 subdomain/root_domain。**已存在的资产会自动归并——不要
   因为可能已存在而跳过登记**，归并把本任务与资产的血缘连起来才是目的。
3. **漏洞挂资产**：`report_finding` 尽量带 asset_ids，让发现挂到对应资产上。
4. **增量补全**：确认的框架/组件版本补进资产 technologies（追加不覆盖）；代码中的
   可用凭据（数据库连接串、云 AK/SK）用 `record_intelligence`（kind=credential）登记。

## 完成定义

- [ ] 识别技术栈并记录（record_fact）
- [ ] 入口/路由层全部过目（HTTP 入口是攻击面的骨架）
- [ ] 危险功能面（上传/登录/支付/导出/命令执行点）逐一审查
- [ ] 硬编码密钥与敏感信息扫描完成
- [ ] 依赖版本 vs 已知漏洞比对完成（有 lock 文件时）
- [ ] 高危疑点尽量做本地动态验证（lab_status 可用时），验证结果挂流量证据
- [ ] 全部发现按产出规约落库（report_finding / record_fact / insert_assets）

## 执行路线图

### Phase 0：解包与识别栈

- 压缩包先 `extract_archive`（返回的 top_level 判断站点根目录）。
- 识别栈（找到即定调，可多栈并存）：

| 特征文件 | 栈 |
|---|---|
| pom.xml / build.gradle / *.jar / *.jsp | Java/JSP |
| composer.json / index.php / think 命名 | PHP |
| requirements.txt / manage.py / app.py / wsgi | Python |
| package.json / yarn.lock / next.config | JS/Node |
| 无后端特征（纯 HTML/JS） | 前端 SPA |

- `record_fact` 记录：栈、版本（从依赖文件读）、入口目录结构。

### Phase 1：入口与配置

- 找全部 HTTP 入口：Java 的 @RequestMapping/Servlet/doGet；PHP 的路由文件与单入口；
  Python 的 @app.route/ViewSet；Node 的 app.get/router。
- 读配置文件（application*.yml、.env、config.php、settings.py）：数据库连接、
  Redis/MQ、第三方 AK/SK、debug 开关。**发现硬编码密钥即报**。
- 找上传/下载点、导出/备份功能、定时任务（常是无鉴权重灾区）。

### Phase 2：四栈危险模式扫描

用 `grep -rn` 按下列清单逐项扫（pattern 按栈选）：

**通用（全栈）**
- SQL 拼接：`sql.*\+\|"\s*+\s*\$|f".*\{.*\}.*(SELECT|INSERT)`（各语言形态）
- 命令执行：`Runtime.exec\|ProcessBuilder\|system(\|shell_exec\|passthru\|exec(\|popen\|os.system\|subprocess.*(shell=True\|eval(\|child_process`
- SSRF：`requests.get(\|urlopen\|HttpClient.*(url.*\+|curl_init.*\$`
- 路径穿越：`File(.*req\.|open(.*req\.|\.\./\|realpath.*\.\.`
- 反序列化：`readObject\|unserialize\|pickle.loads\|yaml.load(\|JSON.parse.*eval`
- SSRF/XXE：`DocumentBuilderFactory\|SAXParser\|XMLParser`(看是否禁外部实体)
- 硬编码密钥：`(?i)(password|secret|apikey|api_key|access_key|token)\s*[:=]\s*['"][^'"]{8,}`
- 敏感信息：身份证/手机号/银行卡正则、`.sql` 备份文件、`dump`/`backup` 命名

**Java/JSP 专项**
- 表达式注入：OGNL `#`、SpEL、Struts2 `${}`、JSP EL 注入点
- fastjson/jackson 反序列化版本（对照已知 RCE 版本区间）
- shiro rememberMe 硬编码 key、log4j2 <2.17、Logback JNDI
- MyBatis `${}` vs `#{}`（前者拼接）

**PHP 专项**
- `include/require` 带变量（LFI/RFI）、`move_uploaded_file` 后缀校验方式
- `preg_replace /e`、`create_function`、`assert(`（代码执行）
- 弱比较 `==` 鉴权、`extract(`、`parse_str(` 变量覆盖
- thinkphp 版本对照已知 RCE（5.x 全版本、6.x <6.0.14）

**Python 专项**
- `render_template_string`（SSTI）、Jinja2 `autoescape=False`
- `pickle.loads`/`yaml.load`（非 safe_load）、`eval/exec`
- Django `DEBUG=True` 生产残留、`ALLOWED_HOSTS=['*']`
- ORM 的 `raw(` / `extra(` 拼接

**JS/Node/前端专项**
- `eval`/`new Function`/`setTimeout(string)`、DOM XSS 的 `innerHTML`/`v-html`/`dangerouslySetInnerHTML`
- `child_process.exec`（区别 exec/execFile）、原型污染 merge/deepClone
- express 中间件顺序（鉴权前挂路由）、CORS `origin:true` + credentials
- JWT none 算法/弱密钥、npm 依赖 `npm audit` 可用则跑
- 前端打包产物中的 sourcemap（.map 文件泄露源码）、API 端点与内网地址硬编码

### Phase 3：本地动态验证（可选但推荐）

把静态确认的高危疑点跑起来实测，产出带流量证据的「动态验证」结论。

**0) 开工先看可用性**：`lab_status`。`available=false`（无 Docker/WSL2）→ 跳过本阶段，保留静态结论，不要硬试。

**1) 选模板并复制到本意图工作目录**（不要改 skill 目录里的原版）：

```bash
cp -r "${SKILL_DIR}/templates/php-mysql" ./lab/
cp ./lab/docker-compose.yml ./lab/docker-compose.yml   # 按需改
```

| 模板 | 适用 | 默认入口/端口 |
|---|---|---|
| `php-mysql` | PHP + MySQL（含 init.sql 种子数据） | Apache 80 |
| `python-app` | Flask/FastAPI/Django（sqlite 或自带 DB） | 8000 |
| `node-mysql` | Express/Koa + MySQL | 3000 |
| `java-app` | Spring Boot 可执行 jar（WAR 见模板注释） | 8080 |

**2) 启动**：`lab_up`（compose 模式）

```
lab_up(mode="compose", compose_file="<本意图目录>/lab/docker-compose.yml",
       source_path="<解包后的源码目录>", env={"LAB_ENTRY":"app.py"})
```

工具会：分配随机宿主端口 → 注入 `${LAB_BIND}` `${LAB_PORT_1..N}` `${LAB_SOURCE}` → `docker compose up -d` → 返回 `{lab_id, urls:["http://127.0.0.1:端口"]}`。

**3) 验证**：用返回的 URL 发真实请求（curl 走平台记录代理，流量自动录制）：

```bash
curl -s -i "http://127.0.0.1:<端口>/app/user.php?id=1%20AND%20SLEEP(2)"
```

- 注入类：对比真假条件响应差异/时间差；命令执行：`id`/`whoami` 回显；
- 越权：无 Cookie 请求受保护接口；上传：传测试文件并访问；
- 失败也是结论：环境起不来/依赖缺失/应用要求外部服务 → 记 fact 说明原因，回退静态结论。

**4) 挂证据**：`traffic_search`（host=127.0.0.1）找到请求的 `traffic_id` → `report_finding` 带
`traffic_refs:[{traffic_id, role:"proof", note:"本地靶场复现"}]`，summary 标「动态验证（本地靶场）」。

**5) 收尾**：`lab_down(lab_id)`。任务结束/删除时平台也会兜底清理（5 分钟 GC + 4 小时 TTL）。

**多意图共用同一环境（复用）**：
- 一个任务里多条意图要打同一个应用时，不要各自重建：第一条意图 `lab_up(..., key="<应用名>")`，
  后续意图 `lab_up(reuse=true, key="<同一个应用名>")` —— 命中同任务同 key 的在运行靶场会直接返回
  同一个 `lab_id` 和 URL，不重建、不重新种子 DB。key 会自动归一化（小写、非法字符转 `-`），
  但仍建议统一用应用名小写，避免拼写差异导致静默不复用。
- **破坏性验证不得复用（硬规约）**：删数据、改配置、上传文件、压测/长耗时请求等会改变环境状态的
  操作，必须单独 `lab_up`（**不带 key**）起独立环境；只有只读验证（GET 注入、信息泄露、
  未授权访问、布尔/时间盲注等）才复用共享环境。共享环境被一个意图改脏后，其他意图的基线全部失真。
- `lab_down` 默认只释放**本意图**的引用（返回 `released:true`）；环境还有其他使用方时保持运行，
  最后一个使用方释放才真正销毁。需要立即强拆（如环境已损坏）用 `lab_down(lab_id, force=true)`。
- `lab_status` 会显示每个靶场的 `key` 与 `holders`（当前使用方数量），据此判断能否复用。
- 规划建议：按模块拆审计面时，让第一条涉及该应用的意图负责搭环境并在 fact 里记下
  `lab_id/key/URL`，后续意图直接复用，最后一条负责释放。

**模板调参要点**：源码路径用 `${LAB_SOURCE}`；端口只用 `${LAB_BIND}:${LAB_PORT_1}:容器端口`；
应用需要的环境变量（DB 连接、密钥）在 compose 里写默认值，必要时用 `lab_up.env` 覆盖；
源码只读挂载的应用若需要写目录（上传/缓存），在 compose 里加可写卷或 `tmpfs`。

**环境适配（常见坑）**：
- 应用硬编码了 `localhost` 数据库/外部服务 → 把源码**复制到 lab 目录**再改（原始上传代码保持不动），
  DB 主机改成 compose 服务名（如 `db`），账号/密码/库名与 compose 环境变量一致；
- 库名用 `lab_up.env={"LAB_DB_NAME":"<真实库名>"}` 传入，init.sql 按真实 schema 改；
- 应用需要 Redis/MQ 等 → 在 compose 里补服务，再 `lab_up`；
- **不要直接调用 Windows 的 `docker` 命令**（Docker Desktop 未运行时必然报 pipe 错误）：查状态用
  `lab_status`，看日志用 `lab_status` 返回的容器名再 `docker logs`（经 WSL 后端时用
  `wsl -d Ubuntu -u root -- docker logs <容器名>`），或直接重开一个靶场；
- `php:8.2-apache` 默认不含 mysqli/pdo_mysql——模板已用 Dockerfile 在构建期编译（首次 1-2 分钟，
  之后缓存）；自定义 PHP 镜像时同样在 Dockerfile 里 `docker-php-ext-install mysqli pdo_mysql`。

### Phase 4：鉴权与逻辑面

- 逐个入口标鉴权属性：匿名/登录/管理员。找「鉴权中间件漏挂」的路由。
- 水平越权：操作对象 id 直接来自请求且无属主校验。
- 垂直越权：普通用户可达管理接口。
- 支付/积分逻辑：金额可否客户端传入、负数、重放。

### Phase 5：汇总与写回

- 按产出规约统一落库；同一漏洞多处出现报一条 finding、evidence 列全部位置。
- 未确认的疑点（找不到完整链路）记 record_fact 而非 finding，标「待验证」。

## 产出规约

- **vulnclass 规范命名**：SQL注入 / 命令执行 / 反序列化 / SSRF / 路径穿越 / 文件上传 /
  XXE / SSTI / 硬编码密钥 / 敏感信息泄露 / 权限绕过 / 越权 / 组件已知漏洞 /
  原型污染 / XSS / 逻辑漏洞
- **evidence 格式**（report_finding）：
  1. 文件路径:行号（相对站点根）
  2. 关键代码片段（截取必要行，勿整文件）
  3. 影响说明 + 触发条件
  4. summary 注明「静态发现，待验证」（未经动态验证的都注明）；本地靶场复现成功的注明
     「动态验证（本地靶场）」并带 `traffic_refs`（role=proof）
- **record_fact**：代码结构事实、配置事实、无漏洞结论（如「XX 接口有参数化查询」）
  ——否定结论同样重要，证明这块审过了。
- **asset_ids**：finding 尽量关联到登记过的 endpoint/域名资产。

## 工具与输出约束

- 大文件（>100KB 的 vendor/依赖目录、min.js）不要 Read 整读，用 grep 定位再读片段。
- 依赖目录（vendor/、node_modules/、target/）默认跳过，只在其版本清单
  （composer.lock、package-lock.json、pom.xml）上做漏洞比对。
- grep 输出超长时写本意图目录的中间文件，finding 里只引用关键行。

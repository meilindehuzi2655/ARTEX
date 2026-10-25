---
name: miniprogram-audit
description: 对反编译后的小程序源码（wxapkg 解包产物）做静态审计时调用该skill。
---

# Miniprogram Audit（微信小程序代码审计）

对任务描述中给出的**反编译后小程序源码**做系统性静态审计。产物为用户上传的
反编译目录/压缩包（常见结构：app.js/app.json/pages/ 与 commonjs 包装的 js 模块）。
本 skill 不处理 wxapkg 解包——上传的已是反编译产物。

## 边界与禁止（硬规约）

| 事项 | 规则 |
|---|---|
| 修改被审计代码 | **禁止**。只读（Read/grep），中间产物写本意图工作目录 |
| 访问小程序后端 | **禁止**。发现的 API 域名/端点只登记为资产，不发请求 |
| 访问微信平台接口 | **禁止**。不动 api.weixin.qq.com，不做 session 换取 |
| 网络侦察 | **禁止** |

## 与资产图的关联工作流（硬规约）

1. **登记小程序本体**：`insert_assets` type=app，app_name 取 app.json 的项目名或
   pages 首页推断，bundle_id 填 appid（project.config.json 里有）。已有则自动归并。
2. **开工预检**：`list_assets` 查该 appid/名称及代码中出现的后端域名是否已有资产，
   复用已有情报。
3. **后端 API 全量登记**：代码中确认的每个 API 调用点 `insert_assets` type=endpoint
   （url=完整后端 URL，method，params 记参数结构）。这是本任务最有价值的资产产出
   ——后续渗透任务直接拿它当攻击面。
4. **AppSecret 类凭据**：`record_intelligence` kind=credential 登记（含出处文件:行号）。
5. **漏洞挂资产**：report_finding 带 asset_ids（endpoint 或 app 本体）。

## 完成定义

- [ ] app.json 完整解读（页面清单/分包/权限声明）并 record_fact
- [ ] 网络层全量梳理（每个 wx.request 调用点 → endpoint 资产）
- [ ] 专项清单（密钥/存储/WebView/登录态/支付）逐项过
- [ ] 全部发现按产出规约落库

## 反编译产物结构速览

| 文件/目录 | 内容 | 审计要点 |
|---|---|---|
| `app.json` | 页面注册/分包/tabBar/window/permission | 页面全集、requiredPrivateInfos（隐私接口） |
| `app.js` | 全局逻辑、onLaunch | 登录初始化、全局密钥、setStorageSync |
| `pages/` | 每页 4 件套（.js/.json/.wxml/.wxss） | 页面级逻辑漏洞、wxml 里的 rich-text/WebView |
| `project.config.json` | appid、编译设置 | appid、urlCheck 开关 |
| `common/` 或同级 `*.js` | 反编译还原的公共模块（commonjs 包装） | 网络封装、加密、配置常量 |
| `miniprogram_npm/` | 构建后的 npm 依赖 | 组件已知漏洞 |

反编译 JS 的特征：模块被包成 `__commonjs` 或 `define("pages/xx/xx.js", ...)` 形式，
变量名可能被压缩混淆——按 `wx.` API 调用与字符串常量（域名、密钥模式）定位，不追求
逐行读懂混淆代码。

## 执行路线图

### Phase 0：结构与全局配置

- 压缩包先 `extract_archive`。
- 读 `app.json`：pages 全集 + subpackages + permission/requiredPrivateInfos。
  record_fact 记「共 N 页面 M 分包，声明权限 [..]」。
- 读 `project.config.json`：appid（登记 app 资产）、`urlCheck:false`（说明开发期
  关了域名校验，本身就是信息点）。
- 读 `app.js`：onLaunch 里的 login 流程、全局常量（grep 密钥模式）。

### Phase 1：网络层梳理（核心产出）

- `grep -rn "wx.request\|wx.uploadFile\|wx.downloadFile\|WebSocket\|connectSocket"`。
- 对每个调用点抽出：URL（含域名拼接的 baseURL 常量）、method、参数结构、
  鉴权头（token 从哪来）。逐条 `insert_assets` endpoint。
- 找 baseURL 定义处（通常在 common 的 config 模块）：所有环境（dev/prod）的域名
  都登记——内网/测试环境域名是高价值情报。
- 合法域名校验绕过线索：`urlCheck:false`、wx.request 指向非 app.json 声明域名。

### Phase 2：专项检查清单

**密钥与敏感信息**
- AppSecret：grep `(?i)app[_-]?secret`（AppSecret 绝不应出现在小程序端——出现即严重）
- 其他硬编码：AK/SK、API key、加密密钥、第三方 SDK key
- appid 与后端接口的对应关系（记录，供伪造请求的后续任务用）

**本地存储**
- `setStorageSync` 存敏感数据（token/用户信息）明文——记 finding（低危）
- 存储键名清单——供后续「越权读缓存」分析

**WebView 与跳转**
- `web-view` 组件 / `wx.navigateTo` URL 是否含变量（任意 URL 跳转/钓鱼）
- WebView 加载的 H5 域名登记为资产
- `openEmbeddedMiniProgram`/`navigateToMiniProgram` 目标 appid 记录

**登录态与鉴权**
- 登录流程：code 换 session 的后端端点、token 存放位置、是否随请求携带
- 鉴权判断只在小程序端做（后端不校验）的迹象：接口无 token 也放行的注释/逻辑
- `wx.getUserProfile`/`getPhoneNumber` 的使用位置（隐私合规）

**支付与业务逻辑**
- `wx.requestPayment` 参数来源：金额/订单号是否客户端可控（后端校验痕迹在哪）
- 拼团/砍价/优惠券逻辑页面的关键判断（前端校验依赖）

**分享与伪造**
- `onShareAppMessage` 的 path 是否可被外部控制（诱导分享/钓鱼页）
- 客服消息/模板消息相关逻辑

**代码保护**
- `project.config.json` 的 `uglifyfilenames`/代码保护开关状态（未开则二次打包易）

### Phase 3：汇总与写回

- 同 API 多处调用合并为一条 endpoint 资产（params 并集）。
- 疑点（无法从静态代码确认后端行为的）记 record_fact 标「待验证」。

## 产出规约

- **vulnclass 规范命名**：硬编码密钥（AppSecret 类为 critical）/ 敏感信息泄露 /
  权限绕过 / 越权 / 逻辑漏洞 / XSS(rich-text) / 任意URL跳转 / 组件已知漏洞 /
  明文存储敏感数据
- **evidence 格式**（report_finding）：
  1. 文件路径:行号（相对小程序根）
  2. 关键代码片段（反编译代码保持原样，含模块名）
  3. 影响说明 + 触发条件
  4. summary 注明「静态发现，待验证」
- **record_fact**：结构事实（页面数/分包/权限）、网络层事实（API 清单及鉴权方式）、
  否定结论。
- **endpoint 资产**是本任务的硬产出：URL+method+params 完整，供后续任务继承。

## 工具与输出约束

- 反编译 JS 可能高度混淆：以 `wx.` API 与字符串常量为锚点定位，不逐行读混淆代码。
- miniprogram_npm/ 构建产物只做版本比对（package.json），不逐文件审计。
- wxml 用 grep 找 `web-view`/`rich-text`/`{{` 绑定即可，不必全文读。

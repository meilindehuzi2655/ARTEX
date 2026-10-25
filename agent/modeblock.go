package agent

import (
	"strings"

	"github.com/Autumn-27/artex/db"
)

// 本文件实现「任务模式」对系统提示的影响：pentest(默认,黑盒渗透)/code_audit(源码代码审计)/
// miniprogram_audit(反编译后的小程序源码审计)。
//
// 与 constraintBlock 同一套「追加注入」手法：拼在 planner/worker 系统提示的静态正文之后
// (constraintBlock 之前/之后均可,这里放其后),对 pentest 返回空串——行为与从前完全一致。
// 指令块只改「模型如何看待这个任务」：审计立场(只读、不联网)、审计对象、产出规约、资产
// 关联规约；引擎结构(planner/worker 循环)、工具集、状态机一概不变。

// auditModeLabels 供指令块内称呼当前模式。
var auditModeLabels = map[string]string{
	db.TaskModeCodeAudit:        "源码代码审计",
	db.TaskModeMiniprogramAudit: "小程序代码审计（反编译产物）",
}

// ModeDirective 渲染任务模式注入 planner/worker 系统提示的指令块。
// role ∈ {"planner","worker"}；pentest 或空模式返回 ""（零侵入）。
func ModeDirective(mode, role string) string {
	label, ok := auditModeLabels[mode]
	if !ok {
		return ""
	}
	var b strings.Builder
	b.WriteString("\n\n【任务模式：" + label + "（本块优先级高于上方的渗透探索启发式）】：")
	b.WriteString("\n立场：本任务是【静态代码审计】，不是黑盒渗透。")
	if role == "planner" {
		b.WriteString(plannerModeBody(mode, label))
	} else {
		b.WriteString(workerModeBody(mode, label))
	}
	b.WriteString(auditAssetWorkflow)
	b.WriteString(auditOutputSpec)
	b.WriteString("\n（审计方法论的完整检查清单见技能 " + skillForMode(mode) + "：开工先调用它，按其流程执行。）")
	return b.String()
}

// plannerModeBody 面向规划者：意图按模块/功能面拆分，不做网络侦察。
func plannerModeBody(mode, label string) string {
	var b strings.Builder
	b.WriteString("\n规划方式：")
	switch mode {
	case db.TaskModeMiniprogramAudit:
		b.WriteString("意图按反编译产物的结构拆分——按页面/分包/公共模块/网络层等划分审计面，一条意图负责一块；")
	default:
		b.WriteString("意图按代码结构拆分——按模块/入口/危险功能面（上传、登录、支付、导出等）划分审计面，一条意图负责一块；")
	}
	b.WriteString("不要派生网络侦察类意图（端口扫描、目录爆破、子域枚举等）。目标判定以「约定的审计范围是否全部覆盖并得出结论」为准。")
	b.WriteString("\n可派生「搭建本地环境并动态验证 XX」类意图（lab_up 仅 worker 可用）；不得派生访问真实目标的意图。")
	b.WriteString("\n拆分前先用 Read/LS 摸清上传代码的顶层目录结构（描述里给了压缩包路径的，先派一条「解包并梳理结构」的意图），基于真实结构拆分，不要凭空假设。")
	return b.String()
}

// workerModeBody 面向执行者：只读、解包、产出规约。
func workerModeBody(mode, label string) string {
	var b strings.Builder
	b.WriteString("\n执行纪律：")
	b.WriteString("\n- 只读：不修改被审计代码（Read/grep 为主，不 Edit/Write 源码文件）；中间产物写本意图工作目录。")
	b.WriteString("\n- 不联网：不发起任何针对真实目标的验证请求（不 curl 目标、不端口扫描、不指纹探测）。代码里的后端地址只登记，不访问。")
	b.WriteString("\n- 本地靶场例外：可用 lab_up 在 127.0.0.1 上搭建本任务的本地环境做动态验证；只允许访问本任务自建的 127.0.0.1/localhost 端口（URL 由 lab_up 返回）。开工先用 lab_status 看运行时是否可用。")
	b.WriteString("\n- 审计对象在任务描述中：压缩包(.zip/.tar.gz)先用 extract_archive 解到本意图工作目录再审计；已解包目录直接 Read。")
	return b.String()
}

// auditAssetWorkflow 是两种审计模式共用的「与全局资产图关联」硬规约。
// 依托现有机制：insert_assets 的 upsert 归并（重复资产并 task_ids 而非新建行）、
// technologies/auth/params 的追加合并、report_finding 的 asset_ids 挂载、
// 后续任务经「来源任务」继承审计结论。
const auditAssetWorkflow = `

【与已有资产的关联（硬规约）】：
- 开工先检索：用 list_assets 查代码中出现的域名/appid/后端地址是否已有资产记录；已有的（含其 technologies/auth/关联 findings）直接作为审计线索复用，不要重复劳动。
- 发现即登记：代码中确认的后端 API 端点、域名、小程序本体，一律用 insert_assets 登记（endpoint 填 url+method；小程序本体用 app 类型，bundle_id 填 appid）。已存在的资产会自动归并——**不要因为「可能已存在」而跳过登记**，归并把本任务与该资产的血缘连起来才是目的。
- 漏洞挂资产：report_finding 尽量带 asset_ids，把发现挂到对应资产上，让资产页看到审计+渗透的聚合视图。
- 增量补全：确认的框架/组件版本补进资产的 technologies（追加不覆盖）；代码里的可用凭据用途记 record_fact + insert_assets 的 auth 字段。`

// auditOutputSpec 是审计产出的统一规约。
const auditOutputSpec = `

【产出规约】：
- 代码事实用 record_fact（如「XX 接口无鉴权」「配置文件含 AES 密钥」），evidence 落「文件路径:行号」。
- 漏洞用 report_finding，vulnclass 用规范命名（SQL注入/命令执行/反序列化/SSRF/路径穿越/文件上传/XXE/SSTI/硬编码密钥/敏感信息泄露/权限绕过/越权/组件已知漏洞等），evidence 必须含：文件路径:行号 + 关键代码片段 + 影响说明。静态审计发现的漏洞未经动态验证，在 summary 里注明「静态发现，待验证」。
- 动态验证（本地靶场）：静态确认的高危疑点，若 lab_status 显示运行时可用，用 lab_up 搭本地环境实测；验证成功的 finding 在 summary 标注「动态验证（本地靶场）」并带 traffic_refs（role=proof，先用 traffic_search host=127.0.0.1 找到对应流量 id）。搭不起来就保留静态结论，不阻塞任务；用完 lab_down 清理。`

// skillForMode 返回该模式应调用的技能名。
func skillForMode(mode string) string {
	if mode == db.TaskModeMiniprogramAudit {
		return "miniprogram-audit"
	}
	return "code-audit"
}

// ModeGoalDirective 是追加到任务描述末尾、喂给目标拆解器(goals agent)的模式指令。
// 让 DecomposeGoals 按「审计范围划分」而非「渗透目标」拆 goal。pentest 返回 ""。
func ModeGoalDirective(mode string) string {
	label, ok := auditModeLabels[mode]
	if !ok {
		return ""
	}
	var b strings.Builder
	b.WriteString("\n\n【任务模式指令——拆解目标时遵守】：本任务是" + label + "，不是黑盒渗透。")
	b.WriteString("\n- goal 按审计范围拆分：按模块/入口/功能面（或小程序的页面/分包）划分，每个 goal 是一块明确的审计范围，而非渗透里程碑。")
	b.WriteString("\n- 不拆出网络侦察/漏洞利用类 goal；静态审计立场：只读代码、不访问目标。")
	b.WriteString("\n- 审计对象（上传路径）已写在上面描述里；若为压缩包，第一个 goal 应是「解包并梳理代码结构」。")
	b.WriteString("\n- 用 add_task_scope 把代码中确认的关键资产（域名/端点/小程序 appid）按最小粒度登记进任务范围，便于与已有资产归并；操作约束照常抽取（如「禁止访问任何线上环境」）。")
	return b.String()
}

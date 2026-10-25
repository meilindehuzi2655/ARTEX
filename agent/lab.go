package agent

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"

	actool "github.com/Autumn-27/norma/tool"
)

// 本地靶场（audit lab）：代码审计任务在 127.0.0.1 上搭建一次性本地环境，
// 用于把"静态发现"升级为"动态验证"。实现由 server 侧提供（Docker/WSL2），
// agent 包只定义接口与工具，避免耦合容器运行时。

// LabInfo 是一次靶场的可观测状态。
type LabInfo struct {
	LabID      string         `json:"lab_id"`
	Project    string         `json:"project"`
	Backend    string         `json:"backend,omitempty"` // 例如 "native" / "wsl:Ubuntu"
	State      string         `json:"state,omitempty"`   // running / stopped / ...
	URLs       []string       `json:"urls,omitempty"`    // http://127.0.0.1:<port> 形式
	Containers []LabContainer `json:"containers,omitempty"`
	Notes      string         `json:"notes,omitempty"`
	// Key 是 lab_up 传入的复用标识（多意图共用同一应用环境时用同一个 key）。
	Key string `json:"key,omitempty"`
	// Holders 是当前登记的环境使用方数量（引用计数；归零即无人使用）。
	Holders int `json:"holders,omitempty"`
}

// LabContainer 是靶场内单个容器的状态摘要。
type LabContainer struct {
	Name  string `json:"name"`
	State string `json:"state,omitempty"`
	Ports string `json:"ports,omitempty"`
}

// LabUpRequest 是启动一次靶场的输入。Mode=compose 用 compose_file；Mode=image
// 用 image(+command/container_port)。Reuse+Key 命中本任务同 key 的在运行靶场时
// 直接复用（不重建），并把 Holder 登记为使用方。
type LabUpRequest struct {
	TaskID      int64
	Mode        string
	ComposeFile string
	Image       string
	Command     string
	SourcePath  string
	ContainerPort int
	PortCount   int
	Env         map[string]string
	// Key 是复用标识：多个意图要打同一个应用时用相同的 key（如应用名）。
	Key string
	// Reuse 为 true 时按 (taskID, key) 查找在运行靶场并复用。
	Reuse bool
	// Holder 是使用方标识（一般为意图节点 id），lab_down 按它释放引用。
	Holder string
}

// LabRunner is the lab backend seen from the tool layer (implemented in server).
type LabRunner interface {
	Up(ctx context.Context, req LabUpRequest) (LabInfo, error)
	// Down 释放引用：force=false 时仅注销 holder，最后一个使用方才真正销毁；
	// force=true 立即销毁。record 缺失（旧靶场/状态丢失）时无条件销毁。
	Down(ctx context.Context, taskID int64, labID, holder string, force bool) error
	Status(ctx context.Context, taskID int64) (backend string, available bool, labs []LabInfo)
}

// SetLab wires the local-lab backend. nil = lab tools report unavailable.
func (t *ToolSet) SetLab(l LabRunner) { t.lab = l }

// labUp starts a disposable local environment for dynamic verification.
func (t *ToolSet) labUp() actool.CoreTool {
	return writeTool(
		"lab_up",
		"在 127.0.0.1 上搭建一次性本地靶场（Docker），用于把静态审计发现升级为动态验证。\n"+
			"两种模式：\n"+
			"1) compose（推荐）：传 compose_file（绝对路径），工具自动分配端口并通过环境变量注入："+
			"${LAB_BIND}（绑定地址，工具已按后端设好）、${LAB_PORT_1}..${LAB_PORT_N}（宿主端口）、${LAB_SOURCE}（源码路径，已按后端转换）。"+
			"compose 文件里端口必须写成 \"${LAB_BIND}:${LAB_PORT_1}:80\" 这种形式，不要硬编码端口。\n"+
			"2) image：传 image，可选 command/source_path（挂到 /app:ro）/container_port（把分配的宿主端口映射到该容器端口）。\n"+
			"多意图共用同一应用环境：第一次 lab_up 传 key（如应用名），后续意图传 reuse=true + 相同 key 即可拿到同一环境的 URL（不重建）；"+
			"用完各自 lab_down 释放，最后一个使用方释放时才真正销毁。key 会归一化（小写、非法字符转 '-'）。\n"+
			"**只读验证才复用**：删数据/改配置/上传文件/压测等会改变环境状态的验证必须单独 lab_up（不带 key），避免污染其他意图的基线。\n"+
			"资源默认限额（内存 2G / 2 CPU）。返回 {lab_id, urls:[http://127.0.0.1:端口], containers, key, holders}。\n"+
			"独自使用时用完 lab_down；任务结束时平台也会自动清理。",
		obj(map[string]any{
			"mode":           map[string]any{"type": "string", "enum": []string{"compose", "image"}, "description": "compose（默认）或 image"},
			"compose_file":   str("compose 模式必填：docker-compose.yml 的绝对路径"),
			"image":          str("image 模式必填：镜像名，如 php:8.2-apache"),
			"command":        str("image 模式可选：容器启动命令（sh -c 形式）"),
			"source_path":    str("源码目录绝对路径（可选）：compose 模式注入 ${LAB_SOURCE}；image 模式挂载到 /app:ro"),
			"container_port": intp("image 模式可选：要映射的容器端口（如 80/8000/3000）"),
			"port_count":     intp("要分配的宿主端口数量（默认 1）；compose 模式按 ${LAB_PORT_1..N} 注入"),
			"key":            str("复用标识（可选）：多个意图要打同一个应用时，用相同的 key（如应用名），配合 reuse"),
			"reuse": map[string]any{
				"type":        "boolean",
				"description": "true 时按 key 复用本任务已在运行的靶场（不重建）；找不到同 key 的运行靶场则新建",
			},
			"env": map[string]any{
				"type":        "object",
				"description": "额外环境变量（compose 模式注入 docker compose 进程环境，可在 compose 文件里用 ${KEY} 引用；image 模式作为 -e）",
				"additionalProperties": map[string]any{"type": "string"},
			},
		}),
		func(ctx context.Context, in json.RawMessage) (actool.Result, error) {
			if t.lab == nil {
				return actool.Errorf("lab_up 未启用：当前任务不是代码审计模式，或平台未检测到 Docker/WSL2。"), nil
			}
			if t.taskID <= 0 {
				return actool.Errorf("lab_up 需要任务上下文（当前不在任务内运行）"), nil
			}
			var a struct {
				Mode          string            `json:"mode"`
				ComposeFile   string            `json:"compose_file"`
				Image         string            `json:"image"`
				Command       string            `json:"command"`
				SourcePath    string            `json:"source_path"`
				ContainerPort int               `json:"container_port"`
				PortCount     int               `json:"port_count"`
				Key           string            `json:"key"`
				Reuse         bool              `json:"reuse"`
				Env           map[string]string `json:"env"`
			}
			if err := json.Unmarshal(in, &a); err != nil {
				return actool.Errorf("invalid input: " + err.Error()), nil
			}
			if a.Reuse && strings.TrimSpace(a.Key) == "" {
				return actool.Errorf("reuse=true 需要 key（与创建该靶场时相同的标识）"), nil
			}
			info, err := t.lab.Up(ctx, LabUpRequest{
				TaskID: t.taskID, Mode: a.Mode, ComposeFile: a.ComposeFile, Image: a.Image,
				Command: a.Command, SourcePath: a.SourcePath, ContainerPort: a.ContainerPort,
				PortCount: a.PortCount, Env: a.Env, Key: strings.TrimSpace(a.Key),
				Reuse: a.Reuse, Holder: t.labHolder(),
			})
			if err != nil {
				return actool.Errorf("lab_up 失败: " + err.Error()), nil
			}
			return jsonResult(info)
		},
	)
}

// labHolder 返回本 run 的使用方标识：worker 用其意图节点，其余退化为任务级。
func (t *ToolSet) labHolder() string {
	if t.ownerNode > 0 {
		return fmt.Sprintf("i%d", t.ownerNode)
	}
	return fmt.Sprintf("t%d", t.taskID)
}

// labDown stops and removes one lab.
func (t *ToolSet) labDown() actool.CoreTool {
	return writeTool(
		"lab_down",
		"释放一个本地靶场（lab_up 返回的 lab_id）。默认只注销本使用方的引用：该靶场还有其他使用方（holders>0）时保持运行，"+
			"最后一个使用方释放时才真正销毁（容器/网络/数据卷一并清理）。force=true 无视其他使用方立即销毁。",
		obj(map[string]any{
			"lab_id": str("lab_up 返回的 lab_id"),
			"force": map[string]any{
				"type":        "boolean",
				"description": "true=无视其他使用方立即销毁整个靶场（默认 false：引用计数，最后一个才拆）",
			},
		}, "lab_id"),
		func(ctx context.Context, in json.RawMessage) (actool.Result, error) {
			if t.lab == nil {
				return actool.Errorf("lab_down 未启用：平台未检测到 Docker/WSL2。"), nil
			}
			if t.taskID <= 0 {
				return actool.Errorf("lab_down 需要任务上下文"), nil
			}
			var a struct {
				LabID string `json:"lab_id"`
				Force bool   `json:"force"`
			}
			if err := json.Unmarshal(in, &a); err != nil {
				return actool.Errorf("invalid input: " + err.Error()), nil
			}
			if a.LabID == "" {
				return actool.Errorf("lab_id 不能为空"), nil
			}
			if err := t.lab.Down(ctx, t.taskID, a.LabID, t.labHolder(), a.Force); err != nil {
				return actool.Errorf("lab_down 失败: " + err.Error()), nil
			}
			return jsonResult(map[string]any{"lab_id": a.LabID, "released": true})
		},
	)
}

// labStatus lists labs of the current task and reports backend availability.
func (t *ToolSet) labStatus() actool.CoreTool {
	return readTool(
		"lab_status",
		"查看本任务当前的本地靶场（lab_up 启动的环境）与容器运行时可用性。"+
			"审计开工或准备动态验证前先调用它：available=false 时不要尝试搭环境，保留静态结论。",
		obj(map[string]any{}),
		func(ctx context.Context, _ json.RawMessage) (actool.Result, error) {
			if t.lab == nil {
				return jsonResult(map[string]any{"available": false, "reason": "平台未检测到 Docker/WSL2 或当前任务不是审计模式"})
			}
			backend, available, labs := t.lab.Status(ctx, t.taskID)
			return jsonResult(map[string]any{
				"available": available, "backend": backend, "labs": labs,
			})
		},
	)
}

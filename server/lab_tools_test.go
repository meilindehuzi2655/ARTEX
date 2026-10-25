package server

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/Autumn-27/artex/agent"
	actool "github.com/Autumn-27/norma/tool"
)

// TestLabToolsEndToEnd 验证 agent 工具层（ToolSet.WorkerTools → lab_up/lab_status/lab_down）
// 到 server 靶场后端的完整接线。需要 Docker（native 或 WSL2），默认跳过：
//
//	ARTEX_LAB_TEST=1 go test ./server -run TestLabToolsEndToEnd -v
func TestLabToolsEndToEnd(t *testing.T) {
	if os.Getenv("ARTEX_LAB_TEST") != "1" {
		t.Skip("set ARTEX_LAB_TEST=1 to run the real-container tool test")
	}
	m := newLabManager(nil, t.TempDir())
	ctx, cancel := context.WithTimeout(context.Background(), 12*time.Minute)
	defer cancel()
	if _, err := m.detect(ctx); err != nil {
		t.Skipf("no docker backend: %v", err)
	}

	// 与 worker 运行时一致的装配：NewToolSet + SetTaskID + SetLab。
	ts := agent.NewToolSet(nil, "worker")
	ts.SetTaskID(999998)
	ts.SetLab(m)
	tools := map[string]actool.CoreTool{}
	for _, tool := range ts.WorkerTools() {
		tools[tool.Name()] = tool
	}
	for _, name := range []string{"lab_up", "lab_down", "lab_status"} {
		if tools[name] == nil {
			t.Fatalf("tool %s not registered in WorkerTools", name)
		}
	}

	dir := t.TempDir()
	src := filepath.Join(dir, "src")
	if err := os.MkdirAll(src, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(src, "index.html"), []byte("<h1>tool-lab-ok</h1>"), 0o644); err != nil {
		t.Fatal(err)
	}
	compose := `services:
  web:
    image: php:8.2-apache
    volumes:
      - ${LAB_SOURCE:-./src}:/var/www/html:ro
    ports:
      - "${LAB_BIND:-127.0.0.1}:${LAB_PORT_1:-8080}:80"
`
	cf := filepath.Join(dir, "docker-compose.yml")
	if err := os.WriteFile(cf, []byte(compose), 0o644); err != nil {
		t.Fatal(err)
	}

	upIn, _ := json.Marshal(map[string]any{
		"mode": "compose", "compose_file": cf, "source_path": src, "port_count": 1,
	})
	res, err := tools["lab_up"].Call(ctx, upIn, &actool.ToolContext{})
	if err != nil {
		t.Fatalf("lab_up call: %v", err)
	}
	if res.IsError {
		t.Fatalf("lab_up error: %s", toolText(res))
	}
	var info struct {
		LabID   string   `json:"lab_id"`
		URLs    []string `json:"urls"`
		State   string   `json:"state"`
		Backend string   `json:"backend"`
	}
	if err := json.Unmarshal([]byte(toolText(res)), &info); err != nil {
		t.Fatalf("parse lab_up result %q: %v", toolText(res), err)
	}
	t.Logf("lab_up -> %+v", info)
	if info.LabID == "" || len(info.URLs) == 0 {
		t.Fatalf("lab_up returned incomplete info: %+v", info)
	}
	defer func() {
		downIn, _ := json.Marshal(map[string]any{"lab_id": info.LabID})
		if res, err := tools["lab_down"].Call(context.Background(), downIn, &actool.ToolContext{}); err != nil || res.IsError {
			t.Errorf("lab_down: err=%v res=%s", err, toolText(res))
		}
	}()

	// lab_status 应能看到该靶场。
	st, err := tools["lab_status"].Call(ctx, json.RawMessage(`{}`), &actool.ToolContext{})
	if err != nil || st.IsError {
		t.Fatalf("lab_status: err=%v res=%s", err, toolText(st))
	}
	if !strings.Contains(toolText(st), info.LabID) {
		t.Errorf("lab_status does not list %s: %s", info.LabID, toolText(st))
	}

	// 靶场可访问。
	var body string
	for i := 0; i < 15; i++ {
		resp, err := http.Get(info.URLs[0])
		if err == nil {
			b, _ := io.ReadAll(resp.Body)
			resp.Body.Close()
			body = string(b)
			if strings.Contains(body, "tool-lab-ok") {
				break
			}
		}
		time.Sleep(2 * time.Second)
	}
	if !strings.Contains(body, "tool-lab-ok") {
		t.Fatalf("lab not serving expected content: %.200q", body)
	}
	t.Logf("served OK from %s", info.URLs[0])
}

func toolText(res actool.Result) string {
	var b strings.Builder
	for _, c := range res.Content {
		b.WriteString(c.Text)
	}
	return b.String()
}

// TestLabGC 验证兜底回收：pg 为 nil 时所有靶场视为 stale，gcOnce 应删除容器。
func TestLabGC(t *testing.T) {
	if os.Getenv("ARTEX_LAB_TEST") != "1" {
		t.Skip("set ARTEX_LAB_TEST=1 to run the real-container GC test")
	}
	m := newLabManager(nil, t.TempDir())
	ctx, cancel := context.WithTimeout(context.Background(), 8*time.Minute)
	defer cancel()
	if _, err := m.detect(ctx); err != nil {
		t.Skipf("no docker backend: %v", err)
	}
	dir := t.TempDir()
	src := filepath.Join(dir, "src")
	if err := os.MkdirAll(src, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(src, "index.html"), []byte("gc"), 0o644); err != nil {
		t.Fatal(err)
	}
	cf := filepath.Join(dir, "docker-compose.yml")
	compose := "services:\n  web:\n    image: php:8.2-apache\n    volumes:\n      - ${LAB_SOURCE}:/var/www/html:ro\n    ports:\n      - \"${LAB_BIND}:${LAB_PORT_1}:80\"\n"
	if err := os.WriteFile(cf, []byte(compose), 0o644); err != nil {
		t.Fatal(err)
	}
	info, err := m.Up(ctx, agent.LabUpRequest{TaskID: 999997, Mode: "compose", ComposeFile: cf, SourcePath: src})
	if err != nil {
		t.Fatalf("lab_up: %v", err)
	}
	m.gcOnce(ctx)
	out, _ := m.runDocker(ctx, nil, "ps", "-a", "--filter", "name="+info.Project, "--format", "{{.Names}}")
	if strings.TrimSpace(out) != "" {
		t.Fatalf("GC did not remove lab containers: %s", out)
	}
	t.Logf("GC removed %s", info.Project)
}

var _ = fmt.Sprintf

// TestLabReuseRefCount 验证方向1：同任务多意图按 key 复用同一靶场，引用计数
// 归零才销毁；force=true 可无视其他使用方立即销毁。
func TestLabReuseRefCount(t *testing.T) {
	if os.Getenv("ARTEX_LAB_TEST") != "1" {
		t.Skip("set ARTEX_LAB_TEST=1 to run the real-container reuse test")
	}
	m := newLabManager(nil, t.TempDir()) // 独立状态文件，不污染真实数据目录
	ctx, cancel := context.WithTimeout(context.Background(), 12*time.Minute)
	defer cancel()
	if _, err := m.detect(ctx); err != nil {
		t.Skipf("no docker backend: %v", err)
	}
	mk := func(owner int64) map[string]actool.CoreTool {
		ts := agent.NewToolSet(nil, "worker")
		ts.SetTaskID(999996)
		ts.SetOwnerNode(owner)
		ts.SetLab(m)
		out := map[string]actool.CoreTool{}
		for _, tool := range ts.WorkerTools() {
			out[tool.Name()] = tool
		}
		return out
	}
	a, b := mk(501), mk(502)

	dir := t.TempDir()
	src := filepath.Join(dir, "src")
	if err := os.MkdirAll(src, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(src, "index.html"), []byte("<h1>reuse-ok</h1>"), 0o644); err != nil {
		t.Fatal(err)
	}
	cf := filepath.Join(dir, "docker-compose.yml")
	if err := os.WriteFile(cf, []byte("services:\n  web:\n    image: php:8.2-apache\n    volumes:\n      - ${LAB_SOURCE}:/var/www/html:ro\n    ports:\n      - \"${LAB_BIND}:${LAB_PORT_1}:80\"\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	upIn, _ := json.Marshal(map[string]any{"mode": "compose", "compose_file": cf, "source_path": src, "key": "shop"})
	res, err := a["lab_up"].Call(ctx, upIn, &actool.ToolContext{})
	if err != nil || res.IsError {
		t.Fatalf("A lab_up: %v %s", err, toolText(res))
	}
	var first struct {
		LabID   string `json:"lab_id"`
		Holders int    `json:"holders"`
	}
	_ = json.Unmarshal([]byte(toolText(res)), &first)
	if first.LabID == "" || first.Holders != 1 {
		t.Fatalf("first up: %+v", first)
	}
	defer func() {
		in, _ := json.Marshal(map[string]any{"lab_id": first.LabID, "force": true})
		_, _ = a["lab_down"].Call(context.Background(), in, &actool.ToolContext{})
	}()

	// B 复用它：同一个 lab_id，引用数 2。
	reuseIn, _ := json.Marshal(map[string]any{"mode": "compose", "compose_file": cf, "source_path": src, "key": "shop", "reuse": true})
	res, err = b["lab_up"].Call(ctx, reuseIn, &actool.ToolContext{})
	if err != nil || res.IsError {
		t.Fatalf("B reuse: %v %s", err, toolText(res))
	}
	var second struct {
		LabID   string `json:"lab_id"`
		Holders int    `json:"holders"`
	}
	_ = json.Unmarshal([]byte(toolText(res)), &second)
	if second.LabID != first.LabID || second.Holders != 2 {
		t.Fatalf("reuse mismatch: first=%+v second=%+v", first, second)
	}

	// B 释放：A 还在用，靶场必须继续运行。
	downB, _ := json.Marshal(map[string]any{"lab_id": first.LabID})
	if res, err := b["lab_down"].Call(ctx, downB, &actool.ToolContext{}); err != nil || res.IsError {
		t.Fatalf("B down: %v %s", err, toolText(res))
	}
	_, _, labs := m.Status(ctx, 999996)
	if len(labs) == 0 || labs[0].State != "running" {
		t.Fatalf("lab should stay running after B release: %+v", labs)
	}

	// A 释放（最后一个）：靶场销毁。
	if res, err := a["lab_down"].Call(ctx, downB, &actool.ToolContext{}); err != nil || res.IsError {
		t.Fatalf("A down: %v %s", err, toolText(res))
	}
	out, _ := m.runDocker(ctx, nil, "ps", "-a", "--filter", "name="+labProject(999996, first.LabID), "-q")
	if strings.TrimSpace(stripWSLNoise(out)) != "" {
		t.Fatalf("lab not removed after last holder released: %s", out)
	}

	// force 语义：A 重建，B force 直接销毁。
	res, err = a["lab_up"].Call(ctx, upIn, &actool.ToolContext{})
	if err != nil || res.IsError {
		t.Fatalf("A up again: %v %s", err, toolText(res))
	}
	var third struct {
		LabID string `json:"lab_id"`
	}
	_ = json.Unmarshal([]byte(toolText(res)), &third)
	forceIn, _ := json.Marshal(map[string]any{"lab_id": third.LabID, "force": true})
	if res, err := b["lab_down"].Call(ctx, forceIn, &actool.ToolContext{}); err != nil || res.IsError {
		t.Fatalf("B force down: %v %s", err, toolText(res))
	}
	out, _ = m.runDocker(ctx, nil, "ps", "-a", "--filter", "name="+labProject(999996, third.LabID), "-q")
	if strings.TrimSpace(stripWSLNoise(out)) != "" {
		t.Fatalf("force down left containers: %s", out)
	}
}

package server

import (
	"context"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/Autumn-27/artex/agent"
)

// TestLabIntegration 是真实容器集成测试：需要本机有 Docker（native 或 WSL2）。
// 默认跳过；手动运行：
//
//	ARTEX_LAB_TEST=1 go test ./server -run TestLabIntegration -v
func TestLabIntegration(t *testing.T) {
	if os.Getenv("ARTEX_LAB_TEST") != "1" {
		t.Skip("set ARTEX_LAB_TEST=1 to run the real-container integration test")
	}
	m := newLabManager(nil, t.TempDir())
	ctx, cancel := context.WithTimeout(context.Background(), 12*time.Minute)
	defer cancel()
	b, err := m.detect(ctx)
	if err != nil {
		t.Skipf("no docker backend: %v", err)
	}
	t.Logf("backend: %s", b.String())

	dir := t.TempDir()
	// 路径故意带空格：验证 WSL 后端对含空格参数（源码/compose 路径）的转义。
	src := filepath.Join(dir, "src with space")
	if err := os.MkdirAll(src, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(src, "index.html"), []byte("<h1>lab-ok</h1>"), 0o644); err != nil {
		t.Fatal(err)
	}
	compose := fmt.Sprintf(`services:
  web:
    image: php:8.2-apache
    volumes:
      - ${LAB_SOURCE:-./src}:/var/www/html:ro
    ports:
      - "${LAB_BIND:-127.0.0.1}:${LAB_PORT_1:-8080}:80"
`)
	cf := filepath.Join(dir, "lab dir")
	if err := os.MkdirAll(cf, 0o755); err != nil {
		t.Fatal(err)
	}
	cf = filepath.Join(cf, "docker-compose.yml")
	if err := os.WriteFile(cf, []byte(compose), 0o644); err != nil {
		t.Fatal(err)
	}

	info, err := m.Up(ctx, agent.LabUpRequest{
		TaskID: 999999, Mode: "compose", ComposeFile: cf, SourcePath: src, PortCount: 1,
	})
	if err != nil {
		t.Fatalf("lab_up: %v", err)
	}
	t.Logf("lab: %+v", info)
	if len(info.Containers) == 0 {
		t.Errorf("inspectProject returned no containers")
	}
	defer func() {
		if err := m.Down(context.Background(), 999999, info.LabID, "t999999", false); err != nil {
			t.Errorf("lab_down: %v", err)
		}
	}()
	if len(info.URLs) == 0 {
		t.Fatalf("no URL returned: %+v", info)
	}
	// 容器刚起来时 Apache 可能还在初始化，重试几次。
	var body string
	for i := 0; i < 15; i++ {
		resp, err := http.Get(info.URLs[0])
		if err == nil {
			b, _ := io.ReadAll(resp.Body)
			resp.Body.Close()
			body = string(b)
			if strings.Contains(body, "lab-ok") {
				break
			}
		}
		time.Sleep(2 * time.Second)
	}
	if !strings.Contains(body, "lab-ok") {
		t.Fatalf("container not serving expected content, got: %.200q", body)
	}
	t.Logf("served OK from %s", info.URLs[0])
}

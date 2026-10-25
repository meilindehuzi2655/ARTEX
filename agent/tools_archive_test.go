package agent

import (
	"archive/tar"
	"archive/zip"
	"bytes"
	"compress/gzip"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// ---- ModeDirective / ModeGoalDirective ----

func TestModeDirectivePentestEmpty(t *testing.T) {
	if got := ModeDirective("", "worker"); got != "" {
		t.Errorf("空模式应返回空串, got %q", got)
	}
	if got := ModeDirective("pentest", "planner"); got != "" {
		t.Errorf("pentest 应返回空串, got %q", got)
	}
	if got := ModeDirective("unknown-mode", "worker"); got != "" {
		t.Errorf("未知模式应返回空串, got %q", got)
	}
	if got := ModeGoalDirective("pentest"); got != "" {
		t.Errorf("ModeGoalDirective pentest 应返回空串, got %q", got)
	}
}

func TestModeDirectiveAuditModes(t *testing.T) {
	for _, mode := range []string{"code_audit", "miniprogram_audit"} {
		for _, role := range []string{"planner", "worker"} {
			got := ModeDirective(mode, role)
			if !strings.Contains(got, "任务模式") {
				t.Errorf("%s/%s 缺少任务模式标题", mode, role)
			}
			if !strings.Contains(got, "静态代码审计") {
				t.Errorf("%s/%s 缺少审计立场", mode, role)
			}
			if !strings.Contains(got, "insert_assets") || !strings.Contains(got, "report_finding") {
				t.Errorf("%s/%s 缺少资产关联规约", mode, role)
			}
			if !strings.Contains(got, "record_fact") {
				t.Errorf("%s/%s 缺少产出规约", mode, role)
			}
			if !strings.Contains(got, "lab_up") || !strings.Contains(got, "lab_status") {
				t.Errorf("%s/%s 缺少本地靶场指引", mode, role)
			}
			if !strings.Contains(got, "traffic_refs") {
				t.Errorf("%s/%s 缺少动态验证证据规约", mode, role)
			}
		}
	}
	// planner 版强调拆分方式,worker 版强调执行纪律
	if w, p := ModeDirective("code_audit", "worker"), ModeDirective("code_audit", "planner"); w == p {
		t.Error("worker 与 planner 指令块不应完全相同")
	}
	if got := ModeDirective("code_audit", "planner"); !strings.Contains(got, "不要派生网络侦察") {
		t.Error("planner 版缺少网络侦察禁令")
	}
	if got := ModeDirective("miniprogram_audit", "planner"); !strings.Contains(got, "分包") {
		t.Error("小程序 planner 版缺少分包拆分提示")
	}
	// 目标拆解指令
	got := ModeGoalDirective("code_audit")
	for _, want := range []string{"审计范围", "解包并梳理代码结构", "add_task_scope"} {
		if !strings.Contains(got, want) {
			t.Errorf("ModeGoalDirective(code_audit) 缺少 %q", want)
		}
	}
}

// TestModeDirectiveMainAgent locks the human-interface discipline: the main agent
// must probe lab_status (which also covers WSL2) and dispatch add_intent, instead
// of trying to install PHP/Docker itself with Bash.
func TestModeDirectiveMainAgent(t *testing.T) {
	for _, mode := range []string{"code_audit", "miniprogram_audit"} {
		got := ModeDirective(mode, "mainagent")
		for _, want := range []string{
			"lab_status", "add_intent", "WSL2", "Docker",
			"不亲自搭环境", "不要自己尝试下载 PHP", "127.0.0.1",
		} {
			if !strings.Contains(got, want) {
				t.Errorf("%s/mainagent 缺少 %q", mode, want)
			}
		}
		// 主 Agent 不负责产出规约/资产登记，不应携带 worker/planner 的长尾。
		if strings.Contains(got, "report_finding") || strings.Contains(got, "insert_assets") {
			t.Errorf("%s/mainagent 不应携带 worker 的产出/资产规约", mode)
		}
	}
	if got := ModeDirective("pentest", "mainagent"); got != "" {
		t.Errorf("pentest/mainagent 应为空串, got %q", got)
	}
}

// TestMainAgentToolsLabStatus verifies the main agent can only PROBE the lab
// runtime (lab_status), never start/stop environments itself (lab_up/lab_down
// stay worker-only).
func TestMainAgentToolsLabStatus(t *testing.T) {
	ts := NewToolSet(nil, "human")
	names := map[string]bool{}
	for _, tool := range ts.MainAgentTools() {
		names[tool.Name()] = true
	}
	if !names["lab_status"] {
		t.Fatal("MainAgentTools 缺少 lab_status")
	}
	if names["lab_up"] || names["lab_down"] {
		t.Fatalf("MainAgentTools 不应包含 lab_up/lab_down: %v", names)
	}
}

// ---- extract_archive ----

// makeZip 构造一个测试 zip：files 为 name→content。
func makeZip(t *testing.T, files map[string]string) []byte {
	t.Helper()
	var buf bytes.Buffer
	zw := zip.NewWriter(&buf)
	for name, content := range files {
		w, err := zw.Create(name)
		if err != nil {
			t.Fatal(err)
		}
		if _, err := w.Write([]byte(content)); err != nil {
			t.Fatal(err)
		}
	}
	if err := zw.Close(); err != nil {
		t.Fatal(err)
	}
	return buf.Bytes()
}

// makeTarGz 构造一个测试 tar.gz。
func makeTarGz(t *testing.T, files map[string]string) []byte {
	t.Helper()
	var tarBuf bytes.Buffer
	tw := tar.NewWriter(&tarBuf)
	for name, content := range files {
		hdr := &tar.Header{Name: name, Mode: 0o644, Size: int64(len(content)), Typeflag: tar.TypeReg}
		if err := tw.WriteHeader(hdr); err != nil {
			t.Fatal(err)
		}
		if _, err := tw.Write([]byte(content)); err != nil {
			t.Fatal(err)
		}
	}
	if err := tw.Close(); err != nil {
		t.Fatal(err)
	}
	var gzBuf bytes.Buffer
	gzw := gzip.NewWriter(&gzBuf)
	if _, err := gzw.Write(tarBuf.Bytes()); err != nil {
		t.Fatal(err)
	}
	if err := gzw.Close(); err != nil {
		t.Fatal(err)
	}
	return gzBuf.Bytes()
}

func writeFile(t *testing.T, path string, data []byte) string {
	t.Helper()
	if err := os.WriteFile(path, data, 0o644); err != nil {
		t.Fatal(err)
	}
	return path
}

func TestExtractArchiveZip(t *testing.T) {
	dir := t.TempDir()
	src := writeFile(t, filepath.Join(dir, "src.zip"), makeZip(t, map[string]string{
		"app/index.php":      "<?php echo 1;",
		"app/lib/config.php": "<?php $k='secret';",
		"readme.md":          "hello",
	}))
	dest, files, dirs, top, err := extractArchive(src, "")
	if err != nil {
		t.Fatal(err)
	}
	if dest != filepath.Join(dir, "src") {
		t.Errorf("默认 dest = %q, want %q", dest, filepath.Join(dir, "src"))
	}
	if files != 3 || dirs != 0 {
		t.Errorf("files=%d dirs=%d, want 3/0", files, dirs)
	}
	if len(top) != 2 || top[0] != "app" || top[1] != "readme.md" {
		t.Errorf("top_level = %v, want [app readme.md]", top)
	}
	if b, err := os.ReadFile(filepath.Join(dest, "app/lib/config.php")); err != nil || string(b) != "<?php $k='secret';" {
		t.Errorf("解包内容不对: %v %q", err, b)
	}
}

func TestExtractArchiveTarGz(t *testing.T) {
	dir := t.TempDir()
	src := writeFile(t, filepath.Join(dir, "src.tar.gz"), makeTarGz(t, map[string]string{
		"web/main.py": "print(1)",
	}))
	dest, files, _, top, err := extractArchive(src, filepath.Join(dir, "out"))
	if err != nil {
		t.Fatal(err)
	}
	if files != 1 || len(top) != 1 || top[0] != "web" {
		t.Errorf("files=%d top=%v", files, top)
	}
	if b, err := os.ReadFile(filepath.Join(dest, "web/main.py")); err != nil || string(b) != "print(1)" {
		t.Errorf("解包内容不对: %v %q", err, b)
	}
}

func TestExtractArchiveZipSlipRejected(t *testing.T) {
	dir := t.TempDir()
	// 构造带 zip-slip 攻击路径的包
	var buf bytes.Buffer
	zw := zip.NewWriter(&buf)
	w, err := zw.Create("../evil.txt")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := w.Write([]byte("pwn")); err != nil {
		t.Fatal(err)
	}
	if err := zw.Close(); err != nil {
		t.Fatal(err)
	}
	src := writeFile(t, filepath.Join(dir, "evil.zip"), buf.Bytes())
	_, _, _, _, err = extractArchive(src, filepath.Join(dir, "out"))
	if err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(filepath.Join(dir, "evil.txt")); !os.IsNotExist(err) {
		t.Error("zip-slip 路径不应逃出 dest")
	}
}

func TestExtractArchiveTarSymlinkSkipped(t *testing.T) {
	dir := t.TempDir()
	var tarBuf bytes.Buffer
	tw := tar.NewWriter(&tarBuf)
	hdr := &tar.Header{Name: "link", Linkname: "/etc/passwd", Typeflag: tar.TypeSymlink}
	if err := tw.WriteHeader(hdr); err != nil {
		t.Fatal(err)
	}
	if err := tw.Close(); err != nil {
		t.Fatal(err)
	}
	src := writeFile(t, filepath.Join(dir, "src.tar"), tarBuf.Bytes())
	_, files, _, _, err := extractArchive(src, "")
	if err != nil {
		t.Fatal(err)
	}
	if files != 0 {
		t.Errorf("符号链接条目应被跳过, files=%d", files)
	}
}

func TestExtractArchiveUnknownSuffix(t *testing.T) {
	dir := t.TempDir()
	src := writeFile(t, filepath.Join(dir, "blob.rar"), []byte("xx"))
	if _, _, _, _, err := extractArchive(src, ""); err == nil {
		t.Error("未知后缀应报错")
	}
}

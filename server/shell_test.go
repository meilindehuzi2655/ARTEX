package server

import (
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"

	actool "github.com/Autumn-27/norma/tool"
)

func TestResolveBashShellEmpty(t *testing.T) {
	for _, raw := range []string{"", "   ", `""`} {
		got, err := resolveBashShell(raw)
		if err != nil || got != "" {
			t.Errorf("resolveBashShell(%q) = (%q, %v), want empty", raw, got, err)
		}
	}
}

func TestResolveBashShellDirectory(t *testing.T) {
	dir := t.TempDir()
	bin := filepath.Join(dir, "bin")
	if err := os.MkdirAll(bin, 0o755); err != nil {
		t.Fatal(err)
	}
	bash := filepath.Join(bin, "bash.exe")
	if err := os.WriteFile(bash, []byte("fake"), 0o755); err != nil {
		t.Fatal(err)
	}
	got, err := resolveBashShell(dir)
	if err != nil {
		t.Fatal(err)
	}
	if got != bash {
		t.Errorf("got %q, want %q", got, bash)
	}
}

func TestResolveBashShellUsrBin(t *testing.T) {
	dir := t.TempDir()
	usrBin := filepath.Join(dir, "usr", "bin")
	if err := os.MkdirAll(usrBin, 0o755); err != nil {
		t.Fatal(err)
	}
	bash := filepath.Join(usrBin, "bash.exe")
	if err := os.WriteFile(bash, []byte("fake"), 0o755); err != nil {
		t.Fatal(err)
	}
	got, err := resolveBashShell(dir)
	if err != nil {
		t.Fatal(err)
	}
	if got != bash {
		t.Errorf("got %q, want %q", got, bash)
	}
}

func TestResolveBashShellDirectFile(t *testing.T) {
	dir := t.TempDir()
	bash := filepath.Join(dir, "bash.exe")
	if err := os.WriteFile(bash, []byte("fake"), 0o755); err != nil {
		t.Fatal(err)
	}
	got, err := resolveBashShell(bash)
	if err != nil {
		t.Fatal(err)
	}
	if got != bash {
		t.Errorf("got %q, want %q", got, bash)
	}
}

func TestResolveBashShellRejectsNonShell(t *testing.T) {
	dir := t.TempDir()
	exe := filepath.Join(dir, "powershell.exe")
	if err := os.WriteFile(exe, []byte("fake"), 0o755); err != nil {
		t.Fatal(err)
	}
	if _, err := resolveBashShell(exe); err == nil {
		t.Error("powershell.exe 应被拒绝")
	}
	// 目录里没有 bash
	empty := t.TempDir()
	if _, err := resolveBashShell(empty); err == nil {
		t.Error("无 bash 的目录应报错")
	}
	// 不存在的路径
	if _, err := resolveBashShell(filepath.Join(dir, "nope")); err == nil {
		t.Error("不存在的路径应报错")
	}
}

func TestShellOverrideApplied(t *testing.T) {
	// 先记录默认值，测试后恢复，避免影响其它测试。
	defShell, defFlags := actool.CurrentShell()
	defer actool.SetShellCommand("", nil)

	actool.SetShellCommand("/opt/git/bin/bash.exe", []string{"-c"})
	shell, flags := actool.CurrentShell()
	if shell != "/opt/git/bin/bash.exe" || len(flags) != 1 || flags[0] != "-c" {
		t.Fatalf("override not applied: %q %v", shell, flags)
	}

	actool.SetShellCommand("", nil)
	shell, flags = actool.CurrentShell()
	if shell != defShell || strings.Join(flags, " ") != strings.Join(defFlags, " ") {
		t.Errorf("reset mismatch: got %q %v, want %q %v", shell, flags, defShell, defFlags)
	}
	if runtime.GOOS == "windows" && shell != "powershell" {
		t.Errorf("windows default should be powershell, got %q", shell)
	}
}

func TestExpandPathVars(t *testing.T) {
	if runtime.GOOS != "windows" {
		t.Skip("windows-only expansion")
	}
	t.Setenv("ARTEX_TEST_SHELL_DIR", `C:\Tools\git-bash`)
	if got := expandPathVars(`%ARTEX_TEST_SHELL_DIR%\bin\bash.exe`); got != `C:\Tools\git-bash\bin\bash.exe` {
		t.Errorf("percent expansion: %q", got)
	}
}

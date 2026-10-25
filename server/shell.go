package server

import (
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"strings"

	actool "github.com/Autumn-27/norma/tool"
)

// settingBashShell 是「Bash 工具使用哪个 shell」的用户配置（settings KV）。
// 空 = 平台默认（Windows: PowerShell；其他: bash）。非空时指向 Git Bash 的
// bash.exe 或其所在目录（如 .../git-bash，内部自动探测 bin/bash.exe）。
const settingBashShell = "bash_shell_path"

// shellCandidates 是目录输入时的探测顺序（Git for Windows 布局：<root>/bin/bash.exe，
// 也有 usr/bin/bash.exe；个别发行版把 bash.exe 放在根目录）。
var shellCandidates = []string{
	filepath.Join("bin", "bash.exe"),
	filepath.Join("usr", "bin", "bash.exe"),
	"bash.exe",
	filepath.Join("bin", "bash"),
	filepath.Join("usr", "bin", "bash"),
	"bash",
}

// allowedShellBases 限制可执行文件名：只接受 bash/sh/zsh。避免用户误填
// powershell.exe/cmd.exe（-c 语义不同）或 git-bash.exe（GUI 启动器，无法 -c）。
var allowedShellBases = map[string]bool{
	"bash": true, "bash.exe": true,
	"sh": true, "sh.exe": true,
	"zsh": true, "zsh.exe": true,
}

// resolveBashShell 把用户填的路径归一成可直接 exec 的 shell 可执行文件。
// 返回空串表示"用平台默认"（raw 为空）。raw 可以是：
//   - 空：返回 ""，调用方应清除覆盖；
//   - bash.exe 的完整路径；
//   - Git Bash 根目录（探测 bin/bash.exe、usr/bin/bash.exe 等）。
//
// 只做存在性与文件名校验，不执行 --version（启动路径上避免阻塞）。
func resolveBashShell(raw string) (string, error) {
	raw = strings.TrimSpace(raw)
	raw = strings.Trim(raw, `"'`)
	raw = expandPathVars(raw)
	if raw == "" {
		return "", nil
	}
	info, err := os.Stat(raw)
	if err != nil {
		return "", fmt.Errorf("路径不存在：%s", raw)
	}
	if !info.IsDir() {
		if !allowedShellBases[strings.ToLower(filepath.Base(raw))] {
			return "", fmt.Errorf("不是受支持的 shell 可执行文件（需 bash/sh/zsh）：%s", filepath.Base(raw))
		}
		return raw, nil
	}
	for _, rel := range shellCandidates {
		candidate := filepath.Join(raw, rel)
		if fi, err := os.Stat(candidate); err == nil && !fi.IsDir() {
			if allowedShellBases[strings.ToLower(filepath.Base(candidate))] {
				return candidate, nil
			}
		}
	}
	return "", fmt.Errorf("目录下未找到 bash（已尝试 bin/bash.exe、usr/bin/bash.exe 等）：%s", raw)
}

// expandPathVars 展开 Windows 风格的 %VAR% 和 Unix 风格的 $VAR，方便用户填
// %ProgramFiles%\Git\bin\bash.exe 这类路径。未定义变量保持原样。
func expandPathVars(p string) string {
	if runtime.GOOS == "windows" {
		p = os.Expand(p, func(name string) string {
			// os.Expand 处理 $VAR / ${VAR}；%VAR% 单独处理。
			if v, ok := os.LookupEnv(name); ok {
				return v
			}
			return "$" + name
		})
		var b strings.Builder
		for {
			i := strings.IndexByte(p, '%')
			if i < 0 {
				b.WriteString(p)
				break
			}
			j := strings.IndexByte(p[i+1:], '%')
			if j < 0 {
				b.WriteString(p)
				break
			}
			name := p[i+1 : i+1+j]
			b.WriteString(p[:i])
			if v, ok := os.LookupEnv(name); ok {
				b.WriteString(v)
			} else {
				b.WriteString(p[i : i+2+j])
			}
			p = p[i+2+j:]
		}
		return b.String()
	}
	return os.ExpandEnv(p)
}

// bashShellSetting 读回用户填写的原始值（用于设置页回显；空=平台默认）。
func bashShellSetting(pg interface {
	GetSetting(string) (string, bool, error)
}) string {
	if pg == nil {
		return ""
	}
	v, _, _ := pg.GetSetting(settingBashShell)
	return strings.TrimSpace(v)
}

// applyBashShell 读取设置并应用到 norma 的 shell 覆盖。启动时与设置变更时调用。
// 非法路径不会中断启动：记录错误并回落到平台默认（Bash 工具保持可用）。
func (s *Server) applyBashShell() error {
	raw, _, err := s.m.pg.GetSetting(settingBashShell)
	if err != nil {
		return err
	}
	resolved, err := resolveBashShell(raw)
	if err != nil {
		// 存量配置失效（比如用户把 Git 删了）：回落默认，避免整个平台 Bash 不可用。
		actool.SetShellCommand("", nil)
		return err
	}
	if resolved == "" {
		actool.SetShellCommand("", nil)
		return nil
	}
	actool.SetShellCommand(resolved, []string{"-c"})
	return nil
}

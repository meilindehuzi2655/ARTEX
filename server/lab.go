package server

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"log"
	"net"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"runtime"
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"
	"unicode/utf16"

	"github.com/Autumn-27/artex/agent"
	"github.com/Autumn-27/artex/db"
)

// 本地靶场（audit lab）：代码审计任务把源码跑在 127.0.0.1 的一次性容器里，
// 用真实请求验证静态发现。容器运行时优先用本机 docker CLI（Linux/macOS 或
// 装了 Docker Desktop 的 Windows），否则用 WSL2 里的 Docker Engine
// （`wsl -d <distro> -u root -- docker ...`）。
//
// 命名约定：项目/容器名 = artex-t<taskID>-<labID>（labID 8 位 hex）。
// compose 的 project 同名，因此容器、网络、数据卷都能按 project 前缀/标签清理。
// 平台侧兜底：每 5 分钟 GC 一次，任务终态/删除或靶场超过 4 小时即回收。

const (
	labNamePrefix   = "artex-t"
	labDefaultPorts = 1
	labUpTimeout    = 15 * time.Minute
	labCmdTimeout   = 2 * time.Minute
	labGCTTL        = 4 * time.Hour
	labGCInterval   = 5 * time.Minute
	labMemLimit     = "2g"
	labCPULimit     = "2"
	labPidsLimit    = "1024"
)

// labBackend 是探测到的容器运行时。
type labBackend struct {
	kind   string // native | wsl
	distro string // wsl 发行版名
}

func (b *labBackend) String() string {
	if b == nil {
		return ""
	}
	if b.kind == "wsl" {
		return "wsl:" + b.distro
	}
	return "native"
}

// labRecord 是一个靶场的登记信息（复用 key + 引用计数），持久化在
// <dataDir>/labs-state.json，重启不丢。容器本体的真源仍是 docker（命名约定），
// 记录丢失时一切退化为旧的 force 语义，安全。
type labRecord struct {
	TaskID   int64           `json:"task_id"`
	LabID    string          `json:"lab_id"`
	Project  string          `json:"project"`
	Key      string          `json:"key,omitempty"`
	Holders  map[string]bool `json:"holders"`
	CreateAt time.Time       `json:"created_at"`
}

// labManager 管理靶场生命周期。零值不可用，用 newLabManager 构造。
type labManager struct {
	pg  *db.DB
	dir string // 数据目录，状态文件落在这里

	mu       sync.Mutex
	cached   *labBackend
	cachedAt time.Time
	cacheErr error

	stateMu   sync.Mutex
	records   map[string]*labRecord // key = project 名
	statePath string
}

func newLabManager(pg *db.DB, dir string) *labManager {
	m := &labManager{pg: pg, dir: dir, records: map[string]*labRecord{}}
	if dir != "" {
		m.statePath = filepath.Join(dir, "labs-state.json")
		m.loadState()
	}
	return m
}

// loadState 尽力恢复登记；缺失/损坏从空开始（靶场本体不受影响）。
func (m *labManager) loadState() {
	b, err := os.ReadFile(m.statePath)
	if err != nil {
		return
	}
	var records map[string]*labRecord
	if json.Unmarshal(b, &records) == nil && records != nil {
		m.records = records
	}
}

// saveState 原子落盘（temp+rename）。失败只记日志：内存态仍有效，
// 最坏情况是重启后引用丢失、退化为 force 语义。
func (m *labManager) saveState() {
	if m.statePath == "" {
		return
	}
	m.stateMu.Lock()
	snapshot := make(map[string]*labRecord, len(m.records))
	for k, v := range m.records {
		cp := *v
		cp.Holders = make(map[string]bool, len(v.Holders))
		for h := range v.Holders {
			cp.Holders[h] = true
		}
		snapshot[k] = &cp
	}
	m.stateMu.Unlock()
	b, err := json.Marshal(snapshot)
	if err != nil {
		return
	}
	tmp := m.statePath + ".tmp"
	if err := os.MkdirAll(filepath.Dir(m.statePath), 0o755); err != nil {
		return
	}
	if err := os.WriteFile(tmp, b, 0o600); err != nil {
		log.Printf("[lab] 保存状态失败: %v", err)
		return
	}
	if err := os.Rename(tmp, m.statePath); err != nil {
		log.Printf("[lab] 保存状态失败: %v", err)
	}
}

// recordAdd 登记使用方（不存在则建记录）。
func (m *labManager) recordAdd(rec *labRecord, holder string) {
	m.stateMu.Lock()
	if rec.Holders == nil {
		rec.Holders = map[string]bool{}
	}
	rec.Holders[holder] = true
	m.records[rec.Project] = rec
	m.stateMu.Unlock()
	m.saveState()
}

// recordRelease 注销使用方，返回是否已无使用方（可销毁）。
func (m *labManager) recordRelease(project, holder string) bool {
	m.stateMu.Lock()
	rec := m.records[project]
	empty := true
	if rec != nil {
		delete(rec.Holders, holder)
		empty = len(rec.Holders) == 0
		if empty {
			delete(m.records, project)
		}
	}
	m.stateMu.Unlock()
	m.saveState()
	// 无记录（旧靶场/状态丢失）返回 true：退化为立即销毁，与旧行为一致。
	return rec == nil || empty
}

// recordDrop 彻底删除记录（销毁/GC 后调用）。
func (m *labManager) recordDrop(project string) {
	m.stateMu.Lock()
	delete(m.records, project)
	m.stateMu.Unlock()
	m.saveState()
}

// recordGet 按 project 取记录。
func (m *labManager) recordGet(project string) *labRecord {
	m.stateMu.Lock()
	defer m.stateMu.Unlock()
	return m.records[project]
}

// findReusable 按 (taskID, key) 找在运行的靶场记录；同 key 多条时取最新。
func (m *labManager) findReusable(taskID int64, key string) *labRecord {
	m.stateMu.Lock()
	defer m.stateMu.Unlock()
	var best *labRecord
	for _, rec := range m.records {
		if rec.TaskID == taskID && rec.Key == key {
			if best == nil || rec.CreateAt.After(best.CreateAt) {
				best = rec
			}
		}
	}
	return best
}

// detect 探测可用后端并缓存 60 秒（探测失败缓存 30 秒，避免每次工具调用都等超时）。
func (m *labManager) detect(ctx context.Context) (*labBackend, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	ttl := 60 * time.Second
	if m.cached == nil {
		ttl = 30 * time.Second
	}
	if !m.cachedAt.IsZero() && time.Since(m.cachedAt) < ttl {
		if m.cached != nil {
			return m.cached, nil
		}
		return nil, m.cacheErr
	}
	b, err := m.probe(ctx)
	m.cached, m.cachedAt, m.cacheErr = b, time.Now(), err
	return b, err
}

func (m *labManager) probe(ctx context.Context) (*labBackend, error) {
	// 1) 本机 docker CLI 且 daemon 可达（Docker Desktop 停止时 CLI 在但 server 不通）。
	if _, err := exec.LookPath("docker"); err == nil {
		cctx, cancel := context.WithTimeout(ctx, 8*time.Second)
		out, err := exec.CommandContext(cctx, "docker", "version", "--format", "{{.Server.Version}}").CombinedOutput()
		cancel()
		if err == nil && strings.TrimSpace(string(out)) != "" {
			return &labBackend{kind: "native"}, nil
		}
	}
	// 2) Windows: WSL2 里的 Docker Engine。
	if runtime.GOOS == "windows" {
		if _, err := exec.LookPath("wsl"); err == nil {
			distros := []string{"Ubuntu"}
			if list, err := wslDistros(ctx); err == nil && len(list) > 0 {
				distros = list
			}
			for _, d := range distros {
				if d == "docker-desktop" || strings.TrimSpace(d) == "" {
					continue
				}
				cctx, cancel := context.WithTimeout(ctx, 20*time.Second)
				cmd := exec.CommandContext(cctx, "wsl", "-d", d, "-u", "root", "--",
					"docker", "version", "--format", "{{.Server.Version}}")
				cmd.Env = append(os.Environ(), "WSL_UTF8=1")
				out, err := cmd.CombinedOutput()
				cancel()
				if err == nil && strings.TrimSpace(stripWSLNoise(decodeWSLOutput(out))) != "" {
					return &labBackend{kind: "wsl", distro: d}, nil
				}
			}
		}
	}
	return nil, fmt.Errorf("未检测到可用的 Docker 运行时（本机 docker daemon 未运行，且 WSL2 内无 Docker Engine）")
}

// wslDistros 列出已安装发行版（跳过 docker-desktop 之外的都保留）。
func wslDistros(ctx context.Context) ([]string, error) {
	cctx, cancel := context.WithTimeout(ctx, 10*time.Second)
	defer cancel()
	cmd := exec.CommandContext(cctx, "wsl", "-l", "-q")
	cmd.Env = append(os.Environ(), "WSL_UTF8=1")
	out, err := cmd.Output()
	if err != nil {
		return nil, err
	}
	text := decodeWSLOutput(out)
	var list []string
	for _, line := range strings.Split(text, "\n") {
		line = strings.TrimSpace(strings.Trim(line, "\x00"))
		if line != "" {
			list = append(list, line)
		}
	}
	return list, nil
}

// decodeWSLOutput: wsl.exe 自身消息默认是 UTF-16LE，子进程 stdout 视管道环境可能
// 被转成 UTF-16LE（与 UTF-8 警告混在一条流里）。优先要求调用方设 WSL_UTF8=1；这里
// 再对混合流做兜底：定位第一段 UTF-16LE ASCII 模式，前缀按 UTF-8、其余按 UTF-16LE 解。
func decodeWSLOutput(b []byte) string {
	if len(b) == 0 {
		return ""
	}
	if len(b) >= 2 && b[0] == 0xFF && b[1] == 0xFE {
		return decodeUTF16LE(b[2:])
	}
	// 纯 UTF-16LE ASCII：奇数位大量 0x00。
	if isUTF16LEPattern(b) {
		return decodeUTF16LE(b)
	}
	// 混合流：找到第一个 UTF-16LE ASCII 序列的起点。
	for i := 0; i+3 < len(b); i++ {
		if b[i] != 0 && b[i+1] == 0 && b[i+2] != 0 && b[i+3] == 0 && isUTF16LEPattern(b[i:]) {
			return string(b[:i]) + decodeUTF16LE(b[i:])
		}
	}
	return string(b)
}

// isUTF16LEPattern 判断 b 是否像 UTF-16LE 文本（长度过半的奇数位为 0x00）。
func isUTF16LEPattern(b []byte) bool {
	if len(b) < 4 {
		return false
	}
	zeros, pairs := 0, len(b)/2
	for i := 1; i < len(b); i += 2 {
		if b[i] == 0 {
			zeros++
		}
	}
	return zeros*2 >= pairs
}

func decodeUTF16LE(b []byte) string {
	u := make([]uint16, 0, len(b)/2)
	for i := 0; i+1 < len(b); i += 2 {
		u = append(u, uint16(b[i])|uint16(b[i+1])<<8)
	}
	return string(utf16.Decode(u))
}

// runDocker 执行一条 docker 命令（带可选环境变量），返回合并输出。
//
// WSL 后端不能直接传 argv：wsl.exe 会把参数拼成命令行交给 Linux shell，带空格
// 或 `|` 的参数（compose 路径、--format 模板）会被拆开/当管道执行。因此 WSL 侧
// 统一把「export 环境变量 + exec docker <shell-quoted args>」写成脚本，经 stdin
// 交给 `bash -s`，彻底绕开转义问题。
func (m *labManager) runDocker(ctx context.Context, env map[string]string, args ...string) (string, error) {
	b, err := m.detect(ctx)
	if err != nil {
		return "", err
	}
	cctx, cancel := context.WithTimeout(ctx, labCmdTimeout)
	defer cancel()
	var cmd *exec.Cmd
	if b.kind == "native" {
		cmd = exec.CommandContext(cctx, "docker", args...)
		if len(env) > 0 {
			cmd.Env = append(os.Environ(), labEnvSlice(env)...)
		}
	} else {
		var script strings.Builder
		script.WriteString("set -e\n")
		for _, kv := range labEnvSlice(env) {
			i := strings.IndexByte(kv, '=')
			script.WriteString("export " + kv[:i] + "=" + labShellQuote(kv[i+1:]) + "\n")
		}
		script.WriteString("exec docker")
		for _, a := range args {
			script.WriteString(" " + labShellQuote(a))
		}
		script.WriteString("\n")
		cmd = exec.CommandContext(cctx, "wsl", "-d", b.distro, "-u", "root", "--", "bash", "-s")
		cmd.Env = append(os.Environ(), "WSL_UTF8=1")
		cmd.Stdin = strings.NewReader(script.String())
	}
	out, err := cmd.CombinedOutput()
	text := decodeWSLOutput(out)
	if b.kind == "wsl" {
		text = stripWSLNoise(text)
	}
	if err != nil {
		return text, fmt.Errorf("%v: %s", err, labTruncate(text, 600))
	}
	return text, nil
}

// stripWSLNoise 去掉 wsl.exe 自身写进 stderr 的提示（会混入 CombinedOutput，
// 干扰按行解析 docker 输出的调用方）。只匹配 wsl.exe 的固定前缀/文案。
func stripWSLNoise(s string) string {
	if s == "" {
		return s
	}
	var out []string
	for _, line := range strings.Split(s, "\n") {
		t := strings.TrimSpace(strings.TrimRight(line, "\r"))
		if strings.HasPrefix(t, "wsl:") || strings.Contains(t, "localhost 代理配置") {
			continue
		}
		out = append(out, line)
	}
	return strings.Join(out, "\n")
}

// labShellQuote 用单引号包裹 POSIX shell 参数（内部单引号转义为 '\''）。
func labShellQuote(s string) string {
	if s == "" {
		return "''"
	}
	return "'" + strings.ReplaceAll(s, "'", `'\''`) + "'"
}

func labEnvSlice(env map[string]string) []string {
	keys := make([]string, 0, len(env))
	for k := range env {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	out := make([]string, 0, len(keys))
	for _, k := range keys {
		out = append(out, k+"="+env[k])
	}
	return out
}

func labTruncate(s string, n int) string {
	s = strings.TrimSpace(s)
	if len(s) <= n {
		return s
	}
	return s[:n] + "…"
}

// labID 生成 8 位 hex。
func newLabID() string {
	var b [4]byte
	if _, err := rand.Read(b[:]); err != nil {
		return fmt.Sprintf("%08x", time.Now().UnixNano()&0xffffffff)
	}
	return hex.EncodeToString(b[:])
}

// labKeyRe 匹配 key 中不允许的字符（统一转 '-'）。保留 Unicode 字母/数字，
// 中文应用名也能作为 key（只做小写与分隔符归一，不做 Unicode 规范化）。
var labKeyRe = regexp.MustCompile(`[^\p{L}\p{N}._-]+`)

// normalizeLabKey 归一复用 key：trim + 小写 + 非法字符转 '-' + 去首尾 '-' + 限长 64。
// 目的是消除 "Shop" / "shop " / "shop-app" 这类拼写差异导致的静默不复用。
func normalizeLabKey(key string) string {
	key = strings.ToLower(strings.TrimSpace(key))
	key = labKeyRe.ReplaceAllString(key, "-")
	key = strings.Trim(key, "-")
	if len(key) > 64 {
		key = key[:64]
	}
	return key
}

// labProject 返回任务+靶场的项目名（容器名、compose project 同名）。
func labProject(taskID int64, labID string) string {
	return fmt.Sprintf("%s%d-%s", labNamePrefix, taskID, labID)
}

var labProjectRe = regexp.MustCompile(`^artex-t(\d+)-([0-9a-f]{8})`)

// parseLabProject 从容器名解析 taskID/labID/project。
func parseLabProject(name string) (taskID int64, labID, project string, ok bool) {
	name = strings.TrimPrefix(name, "/")
	m := labProjectRe.FindStringSubmatch(name)
	if m == nil {
		return 0, "", "", false
	}
	id, err := strconv.ParseInt(m[1], 10, 64)
	if err != nil {
		return 0, "", "", false
	}
	return id, m[2], fmt.Sprintf("%s%s-%s", labNamePrefix, m[1], m[2]), true
}

// translatePathForBackend: WSL 后端需要把 Windows 路径转成 /mnt/<drive>/...。
func translatePathForBackend(b *labBackend, p string) string {
	p = strings.TrimSpace(p)
	if p == "" || b == nil || b.kind != "wsl" {
		return p
	}
	if len(p) >= 2 && p[1] == ':' {
		drive := strings.ToLower(string(p[0]))
		rest := strings.TrimLeft(p[2:], `\/`)
		rest = strings.ReplaceAll(rest, `\`, "/")
		return "/mnt/" + drive + "/" + rest
	}
	return strings.ReplaceAll(p, `\`, "/")
}

// allocatePort 在宿主 127.0.0.1 上找一个空闲端口（WSL 后端同样用宿主端口，
// 容器发布到 WSL 的 0.0.0.0 后由 WSL localhost 转发到 Windows 的 127.0.0.1）。
func allocatePort() (int, error) {
	l, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		return 0, err
	}
	defer l.Close()
	return l.Addr().(*net.TCPAddr).Port, nil
}

// Up 启动一个靶场；Reuse+Key 命中本任务同 key 的在运行靶场时直接复用。
func (m *labManager) Up(ctx context.Context, req agent.LabUpRequest) (agent.LabInfo, error) {
	b, err := m.detect(ctx)
	if err != nil {
		return agent.LabInfo{}, err
	}
	if req.TaskID <= 0 {
		return agent.LabInfo{}, fmt.Errorf("缺少任务上下文")
	}
	holder := strings.TrimSpace(req.Holder)
	if holder == "" {
		holder = fmt.Sprintf("t%d", req.TaskID)
	}
	key := normalizeLabKey(req.Key)
	// 复用：同任务同 key 且容器仍在运行 → 登记引用并返回现状，不重建。
	if req.Reuse {
		if key == "" {
			return agent.LabInfo{}, fmt.Errorf("reuse=true 需要 key")
		}
		if rec := m.findReusable(req.TaskID, key); rec != nil {
			info := m.inspectProject(ctx, b, rec.TaskID, rec.LabID, rec.Project)
			if info.State == "running" {
				m.recordAdd(rec, holder)
				info.Key = key
				info.Holders = len(rec.Holders)
				info.URLs = labURLsFromContainers(info.Containers)
				info.Notes = "复用已有靶场（" + strconv.Itoa(info.Holders) + " 个使用方）；用完 lab_down 释放引用。"
				return info, nil
			}
			// 同 key 的旧靶场已死：清掉残留再新建。
			_ = m.removeProject(ctx, rec.Project)
			m.recordDrop(rec.Project)
		}
	}
	mode := strings.TrimSpace(req.Mode)
	if mode == "" {
		mode = "compose"
	}
	labID := newLabID()
	project := labProject(req.TaskID, labID)

	ports := req.PortCount
	if ports <= 0 {
		ports = labDefaultPorts
	}
	hostPorts := make([]int, 0, ports)
	for i := 0; i < ports; i++ {
		p, err := allocatePort()
		if err != nil {
			return agent.LabInfo{}, fmt.Errorf("分配端口失败: %w", err)
		}
		hostPorts = append(hostPorts, p)
	}
	bind := "127.0.0.1"
	if b.kind == "wsl" {
		// WSL 的 localhost 转发只对 0.0.0.0 监听生效；WSL 本身是 NAT，不会暴露到局域网。
		bind = "0.0.0.0"
	}
	env := map[string]string{}
	for k, v := range req.Env {
		env[k] = v
	}
	env["LAB_BIND"] = bind
	env["LAB_PORT"] = strconv.Itoa(hostPorts[0])
	for i, p := range hostPorts {
		env[fmt.Sprintf("LAB_PORT_%d", i+1)] = strconv.Itoa(p)
	}
	if req.SourcePath != "" {
		src, err := filepath.Abs(strings.TrimSpace(req.SourcePath))
		if err != nil {
			return agent.LabInfo{}, fmt.Errorf("source_path 无效: %w", err)
		}
		if _, err := os.Stat(src); err != nil {
			return agent.LabInfo{}, fmt.Errorf("source_path 不存在: %s", src)
		}
		env["LAB_SOURCE"] = translatePathForBackend(b, src)
	}

	upCtx, cancel := context.WithTimeout(ctx, labUpTimeout)
	defer cancel()

	switch mode {
	case "compose":
		if strings.TrimSpace(req.ComposeFile) == "" {
			return agent.LabInfo{}, fmt.Errorf("compose 模式需要 compose_file")
		}
		cf, err := filepath.Abs(strings.TrimSpace(req.ComposeFile))
		if err != nil {
			return agent.LabInfo{}, fmt.Errorf("compose_file 无效: %w", err)
		}
		if _, err := os.Stat(cf); err != nil {
			return agent.LabInfo{}, fmt.Errorf("compose_file 不存在: %s", cf)
		}
		if _, err := m.runDocker(upCtx, env, "compose", "-p", project, "-f", translatePathForBackend(b, cf),
			"up", "-d", "--remove-orphans"); err != nil {
			return agent.LabInfo{}, err
		}
		m.applyLimits(ctx, project)
	case "image":
		if strings.TrimSpace(req.Image) == "" {
			return agent.LabInfo{}, fmt.Errorf("image 模式需要 image")
		}
		args := []string{"run", "-d", "--name", project,
			"--label", "artex.lab=1",
			"--label", "artex.task=" + strconv.FormatInt(req.TaskID, 10),
			"--label", "artex.lab_id=" + labID,
			"--memory", labMemLimit, "--memory-swap", labMemLimit,
			"--cpus", labCPULimit, "--pids-limit", labPidsLimit,
			"--security-opt", "no-new-privileges"}
		if req.ContainerPort > 0 {
			args = append(args, "-p", fmt.Sprintf("%s:%d:%d", bind, hostPorts[0], req.ContainerPort))
		}
		for _, k := range sortedEnvKeys(req.Env) {
			args = append(args, "-e", k+"="+req.Env[k])
		}
		if req.SourcePath != "" {
			args = append(args, "-v", env["LAB_SOURCE"]+":/app:ro")
		}
		args = append(args, req.Image)
		if strings.TrimSpace(req.Command) != "" {
			args = append(args, "sh", "-c", req.Command)
		}
		if _, err := m.runDocker(upCtx, nil, args...); err != nil {
			return agent.LabInfo{}, err
		}
	default:
		return agent.LabInfo{}, fmt.Errorf("mode 只支持 compose/image，收到 %q", mode)
	}

	info := m.inspectProject(ctx, b, req.TaskID, labID, project)
	info.URLs = labURLs(hostPorts)
	info.Key = key
	m.recordAdd(&labRecord{
		TaskID: req.TaskID, LabID: labID, Project: project, Key: key,
		Holders: map[string]bool{holder: true}, CreateAt: time.Now(),
	}, holder)
	info.Holders = 1
	info.Notes = "靶场仅在 127.0.0.1 可访问；独自使用时用完 lab_down。"
	return info, nil
}

// labURLsFromContainers 从容器端口映射提取访问 URL（复用路径用）。
func labURLsFromContainers(cs []agent.LabContainer) []string {
	var out []string
	seen := map[string]bool{}
	for _, c := range cs {
		for _, u := range labURLsFromPorts(c.Ports) {
			if !seen[u] {
				seen[u] = true
				out = append(out, u)
			}
		}
	}
	return out
}

func sortedEnvKeys(env map[string]string) []string {
	keys := make([]string, 0, len(env))
	for k := range env {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	return keys
}

func labURLs(ports []int) []string {
	out := make([]string, 0, len(ports))
	for _, p := range ports {
		out = append(out, fmt.Sprintf("http://127.0.0.1:%d", p))
	}
	return out
}

// applyLimits 对 compose 起的容器统一套资源限额（best-effort）。
func (m *labManager) applyLimits(ctx context.Context, project string) {
	out, err := m.runDocker(ctx, nil, "ps", "-q", "--filter", "label=com.docker.compose.project="+project)
	if err != nil {
		return
	}
	ids := labNonEmptyLines(out)
	if len(ids) == 0 {
		return
	}
	args := append([]string{"update", "--memory", labMemLimit, "--memory-swap", labMemLimit, "--cpus", labCPULimit}, ids...)
	_, _ = m.runDocker(ctx, nil, args...)
}

// inspectProject 读取项目下容器状态。
func (m *labManager) inspectProject(ctx context.Context, b *labBackend, taskID int64, labID, project string) agent.LabInfo {
	info := agent.LabInfo{LabID: labID, Project: project, Backend: b.String(), State: "unknown"}
	out, err := m.runDocker(ctx, nil, "ps", "-a", "--filter", "name="+project,
		"--format", "{{.Names}}\t{{.State}}\t{{.Ports}}")
	if err != nil {
		info.Notes = labTruncate(err.Error(), 300)
		return info
	}
	running := 0
	for _, line := range labNonEmptyLines(out) {
		parts := strings.SplitN(line, "\t", 3)
		if len(parts) < 2 {
			continue
		}
		name := strings.TrimPrefix(parts[0], "/")
		if !strings.HasPrefix(name, project) {
			continue
		}
		c := agent.LabContainer{Name: name, State: parts[1]}
		if len(parts) > 2 {
			c.Ports = parts[2]
		}
		if c.State == "running" {
			running++
		}
		info.Containers = append(info.Containers, c)
	}
	if running > 0 {
		info.State = "running"
	} else if len(info.Containers) > 0 {
		info.State = "stopped"
	}
	return info
}

// Down 释放引用：force=false 时仅注销 holder，最后一个使用方才真正销毁；
// force=true 立即销毁。无登记记录（旧靶场/状态丢失）时无条件销毁（向后兼容）。
func (m *labManager) Down(ctx context.Context, taskID int64, labID, holder string, force bool) error {
	if strings.TrimSpace(labID) == "" {
		return fmt.Errorf("lab_id 不能为空")
	}
	project := labProject(taskID, labID)
	if !force {
		if rec := m.recordGet(project); rec != nil && len(rec.Holders) > 0 {
			if !m.recordRelease(project, holder) {
				// 还有其他使用方：保持运行。
				return nil
			}
			// 最后一个使用方释放 → 落到下方销毁。
		}
	}
	if err := m.removeProject(ctx, project); err != nil {
		return err
	}
	m.recordDrop(project)
	return nil
}

// removeProject 按项目名清理容器，再按 compose project 标签清理网络/卷。
func (m *labManager) removeProject(ctx context.Context, project string) error {
	names := m.projectContainers(ctx, project)
	if len(names) > 0 {
		if _, err := m.runDocker(ctx, nil, append([]string{"rm", "-f"}, names...)...); err != nil {
			// 幂等：容器可能已被并发清理（agent lab_down 与 GC 撞车）或 Docker 正在删除，
			// 复查后确实已不存在则视为成功，避免清理链路因竞态报错。
			if remaining := m.projectContainers(ctx, project); len(remaining) > 0 {
				return err
			}
		}
	}
	// 网络与数据卷按 compose project 标签回收。
	if out, err := m.runDocker(ctx, nil, "network", "ls", "--filter", "label=com.docker.compose.project="+project, "-q"); err == nil {
		if nets := labNonEmptyLines(out); len(nets) > 0 {
			_, _ = m.runDocker(ctx, nil, append([]string{"network", "rm"}, nets...)...)
		}
	}
	if out, err := m.runDocker(ctx, nil, "volume", "ls", "--filter", "label=com.docker.compose.project="+project, "-q"); err == nil {
		if vols := labNonEmptyLines(out); len(vols) > 0 {
			_, _ = m.runDocker(ctx, nil, append([]string{"volume", "rm", "-f"}, vols...)...)
		}
	}
	return nil
}

// projectContainers 返回项目名下仍存在的容器名（用于清理后的幂等复查）。
func (m *labManager) projectContainers(ctx context.Context, project string) []string {
	out, err := m.runDocker(ctx, nil, "ps", "-a", "--filter", "name="+project, "--format", "{{.Names}}")
	if err != nil {
		return nil
	}
	var names []string
	for _, name := range labNonEmptyLines(out) {
		name = strings.TrimPrefix(name, "/")
		if strings.HasPrefix(name, project) {
			names = append(names, name)
		}
	}
	return names
}

// Status 列出本任务的靶场并报告后端可用性。
func (m *labManager) Status(ctx context.Context, taskID int64) (string, bool, []agent.LabInfo) {
	b, err := m.detect(ctx)
	if err != nil {
		return "", false, nil
	}
	prefix := fmt.Sprintf("%s%d-", labNamePrefix, taskID)
	out, err := m.runDocker(ctx, nil, "ps", "-a", "--filter", "name="+prefix,
		"--format", "{{.Names}}\t{{.State}}\t{{.Ports}}")
	if err != nil {
		return b.String(), true, nil
	}
	byLab := map[string]*agent.LabInfo{}
	var order []string
	for _, line := range labNonEmptyLines(out) {
		parts := strings.SplitN(line, "\t", 3)
		if len(parts) < 2 {
			continue
		}
		name := strings.TrimPrefix(parts[0], "/")
		_, labID, project, ok := parseLabProject(name)
		if !ok {
			continue
		}
		info := byLab[labID]
		if info == nil {
			info = &agent.LabInfo{LabID: labID, Project: project, Backend: b.String(), State: "stopped"}
			byLab[labID] = info
			order = append(order, labID)
		}
		c := agent.LabContainer{Name: name, State: parts[1]}
		if len(parts) > 2 {
			c.Ports = parts[2]
		}
		if c.State == "running" {
			info.State = "running"
		}
		info.Containers = append(info.Containers, c)
		info.URLs = appendUnique(info.URLs, labURLsFromPorts(c.Ports)...)
	}
	// 合并登记信息（复用 key 与引用计数）。
	m.stateMu.Lock()
	for _, labID := range order {
		if info := byLab[labID]; info != nil {
			if rec := m.records[info.Project]; rec != nil {
				info.Key = rec.Key
				info.Holders = len(rec.Holders)
			}
		}
	}
	m.stateMu.Unlock()
	labs := make([]agent.LabInfo, 0, len(order))
	for _, id := range order {
		labs = append(labs, *byLab[id])
	}
	return b.String(), true, labs
}

// labURLsFromPorts 从 docker ps 的 Ports 列解析宿主端口（同一端口 IPv4/IPv6 两条记录去重）。
func labURLsFromPorts(ports string) []string {
	var out []string
	seen := map[int]bool{}
	for _, part := range strings.Split(ports, ",") {
		part = strings.TrimSpace(part)
		// 形如 0.0.0.0:18080->80/tcp 或 :::18080->80/tcp
		i := strings.Index(part, "->")
		if i < 0 {
			continue
		}
		left := part[:i]
		j := strings.LastIndex(left, ":")
		if j < 0 {
			continue
		}
		p, err := strconv.Atoi(left[j+1:])
		if err != nil || p <= 0 || seen[p] {
			continue
		}
		seen[p] = true
		out = append(out, fmt.Sprintf("http://127.0.0.1:%d", p))
	}
	return out
}

func appendUnique(dst []string, vals ...string) []string {
	seen := make(map[string]bool, len(dst))
	for _, v := range dst {
		seen[v] = true
	}
	for _, v := range vals {
		if !seen[v] {
			dst = append(dst, v)
			seen[v] = true
		}
	}
	return dst
}

// DownTask 清理某任务的全部靶场（任务删除时调用）。
func (m *labManager) DownTask(ctx context.Context, taskID int64) {
	prefix := fmt.Sprintf("%s%d-", labNamePrefix, taskID)
	out, err := m.runDocker(ctx, nil, "ps", "-a", "--filter", "name="+prefix, "--format", "{{.Names}}")
	if err != nil {
		return
	}
	projects := map[string]bool{}
	for _, name := range labNonEmptyLines(out) {
		if _, _, project, ok := parseLabProject(strings.TrimPrefix(name, "/")); ok {
			projects[project] = true
		}
	}
	for project := range projects {
		if err := m.removeProject(ctx, project); err != nil {
			log.Printf("[lab] 清理 %s 失败: %v", project, err)
			continue
		}
		m.recordDrop(project)
	}
}

// RunGC 周期清理：任务已终态/已删除，或靶场超过 TTL。
func (m *labManager) RunGC(ctx context.Context, interval time.Duration) {
	if interval <= 0 {
		interval = labGCInterval
	}
	ticker := time.NewTicker(interval)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			m.gcOnce(ctx)
		}
	}
}

func (m *labManager) gcOnce(ctx context.Context) {
	out, err := m.runDocker(ctx, nil, "ps", "-a", "--filter", "name="+labNamePrefix, "--format", "{{.Names}}")
	if err != nil {
		return
	}
	projects := map[string]bool{}
	for _, name := range labNonEmptyLines(out) {
		taskID, _, project, ok := parseLabProject(strings.TrimPrefix(name, "/"))
		if !ok {
			continue
		}
		stale := false
		if m.pg == nil {
			stale = true
		} else if t, err := m.pg.GetTask(taskID); err != nil || t == nil || db.IsTerminal(t.Status) {
			stale = true
		}
		if !stale {
			// TTL：容器创建超过 labGCTTL 也回收（防止任务被永久暂停时残留）。
			if created, err := m.runDocker(ctx, nil, "inspect", "-f", "{{.Created}}", strings.TrimPrefix(name, "/")); err == nil {
				if ts, perr := time.Parse(time.RFC3339Nano, strings.TrimSpace(created)); perr == nil && time.Since(ts) > labGCTTL {
					stale = true
				}
			}
		}
		if stale {
			projects[project] = true
		}
	}
	for project := range projects {
		if err := m.removeProject(ctx, project); err != nil {
			log.Printf("[lab] GC 清理 %s 失败: %v", project, err)
			continue
		}
		m.recordDrop(project)
		log.Printf("[lab] GC 已清理 %s", project)
	}
	// 容器已不存在的陈旧记录（如被外部手动删除）一并出清。
	// 快照在清理删除之后取，刚处理的 project 已不在记录里。
	m.stateMu.Lock()
	recordProjects := make([]string, 0, len(m.records))
	for project := range m.records {
		recordProjects = append(recordProjects, project)
	}
	m.stateMu.Unlock()
	for _, project := range recordProjects {
		out, err := m.runDocker(ctx, nil, "ps", "-a", "--filter", "name="+project, "-q")
		if err == nil && strings.TrimSpace(stripWSLNoise(out)) == "" {
			m.recordDrop(project)
		}
	}
}

func labNonEmptyLines(s string) []string {
	var out []string
	for _, line := range strings.Split(s, "\n") {
		line = strings.TrimSpace(strings.Trim(line, "\x00"))
		if line != "" {
			out = append(out, line)
		}
	}
	return out
}

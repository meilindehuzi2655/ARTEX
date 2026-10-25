package server

import (
	"strings"
	"testing"
	"time"
	"unicode/utf16"
)

func TestParseLabProject(t *testing.T) {
	cases := []struct {
		name    string
		wantID  int64
		wantLab string
		wantPrj string
		ok      bool
	}{
		{"artex-t5-ab12cd34", 5, "ab12cd34", "artex-t5-ab12cd34", true},
		{"artex-t5-ab12cd34-app-1", 5, "ab12cd34", "artex-t5-ab12cd34", true},
		{"/artex-t12-0123abcd-db-1", 12, "0123abcd", "artex-t12-0123abcd", true},
		{"other-container", 0, "", "", false},
		{"artex-t5-XYZ", 0, "", "", false}, // labID 必须是 8 位 hex
	}
	for _, c := range cases {
		id, lab, prj, ok := parseLabProject(c.name)
		if ok != c.ok || id != c.wantID || lab != c.wantLab || prj != c.wantPrj {
			t.Errorf("parseLabProject(%q) = (%d,%q,%q,%v), want (%d,%q,%q,%v)",
				c.name, id, lab, prj, ok, c.wantID, c.wantLab, c.wantPrj, c.ok)
		}
	}
}

func TestTranslatePathForBackend(t *testing.T) {
	wsl := &labBackend{kind: "wsl", distro: "Ubuntu"}
	native := &labBackend{kind: "native"}
	cases := []struct {
		b    *labBackend
		in   string
		want string
	}{
		{wsl, `C:\artex-new\data\tasks\5\i1`, "/mnt/c/artex-new/data/tasks/5/i1"},
		{wsl, `D:/src/app`, "/mnt/d/src/app"},
		{wsl, `/already/posix`, "/already/posix"},
		{native, `C:\src`, `C:\src`},
	}
	for _, c := range cases {
		if got := translatePathForBackend(c.b, c.in); got != c.want {
			t.Errorf("translatePathForBackend(%q) = %q, want %q", c.in, got, c.want)
		}
	}
}

func TestLabURLsFromPorts(t *testing.T) {
	cases := []struct {
		in   string
		want []string
	}{
		{"0.0.0.0:18080->80/tcp, :::18080->80/tcp", []string{"http://127.0.0.1:18080"}},
		{"127.0.0.1:32768->8080/tcp", []string{"http://127.0.0.1:32768"}},
		{"", nil},
	}
	for _, c := range cases {
		got := labURLsFromPorts(c.in)
		if len(got) != len(c.want) {
			t.Errorf("labURLsFromPorts(%q) = %v, want %v", c.in, got, c.want)
			continue
		}
		for i := range got {
			if got[i] != c.want[i] {
				t.Errorf("labURLsFromPorts(%q)[%d] = %q, want %q", c.in, i, got[i], c.want[i])
			}
		}
	}
}

func TestDecodeWSLOutputUTF16(t *testing.T) {
	s := "Ubuntu\r\ndocker-desktop\r\n"
	u := utf16.Encode([]rune(s))
	buf := []byte{0xFF, 0xFE}
	for _, r := range u {
		buf = append(buf, byte(r), byte(r>>8))
	}
	if got := decodeWSLOutput(buf); got != s {
		t.Errorf("decodeWSLOutput UTF-16 = %q, want %q", got, s)
	}
	// UTF-8 透传不受影响
	if got := decodeWSLOutput([]byte("Hello from Docker!")); got != "Hello from Docker!" {
		t.Errorf("decodeWSLOutput UTF-8 = %q", got)
	}
}

func TestAllocatePort(t *testing.T) {
	p, err := allocatePort()
	if err != nil {
		t.Fatal(err)
	}
	if p <= 0 || p > 65535 {
		t.Errorf("allocatePort = %d", p)
	}
}

func TestLabEnvSliceSorted(t *testing.T) {
	got := labEnvSlice(map[string]string{"B": "2", "A": "1"})
	if len(got) != 2 || got[0] != "A=1" || got[1] != "B=2" {
		t.Errorf("labEnvSlice = %v", got)
	}
}

func TestLabRecordRefCount(t *testing.T) {
	dir := t.TempDir()
	m := newLabManager(nil, dir)
	rec := &labRecord{TaskID: 7, LabID: "abcd1234", Project: labProject(7, "abcd1234"), Key: "shop", CreateAt: time.Now()}
	m.recordAdd(rec, "i101")
	if got := m.recordGet(rec.Project); got == nil || len(got.Holders) != 1 {
		t.Fatalf("recordAdd: %+v", got)
	}
	m.recordAdd(rec, "i102")
	if got := m.recordGet(rec.Project); got == nil || len(got.Holders) != 2 {
		t.Fatalf("second holder: %+v", got)
	}
	if empty := m.recordRelease(rec.Project, "i101"); empty {
		t.Fatal("release should not empty with one holder left")
	}
	if got := m.recordGet(rec.Project); got == nil || len(got.Holders) != 1 {
		t.Fatalf("after release: %+v", got)
	}
	if empty := m.recordRelease(rec.Project, "i102"); !empty {
		t.Fatal("release should empty when last holder leaves")
	}
	if got := m.recordGet(rec.Project); got != nil {
		t.Fatalf("record should be dropped: %+v", got)
	}
	// 无记录时释放返回 true（旧靶场/状态丢失 → 立即销毁，向后兼容）。
	if empty := m.recordRelease("artex-t7-deadbeef", "i1"); !empty {
		t.Fatal("missing record should release to empty")
	}
}

func TestLabStatePersistence(t *testing.T) {
	dir := t.TempDir()
	m := newLabManager(nil, dir)
	rec := &labRecord{TaskID: 9, LabID: "11112222", Project: labProject(9, "11112222"), Key: "app", CreateAt: time.Now()}
	m.recordAdd(rec, "i501")
	m.recordAdd(rec, "i502")

	// 模拟重启：新 manager 从同一目录恢复。
	m2 := newLabManager(nil, dir)
	got := m2.recordGet(rec.Project)
	if got == nil || got.Key != "app" || len(got.Holders) != 2 {
		t.Fatalf("state not restored: %+v", got)
	}
	if best := m2.findReusable(9, "app"); best == nil || best.Project != rec.Project {
		t.Fatalf("findReusable: %+v", best)
	}
	if best := m2.findReusable(9, "other"); best != nil {
		t.Fatalf("findReusable should miss wrong key: %+v", best)
	}
}

func TestNormalizeLabKey(t *testing.T) {
	cases := []struct{ in, want string }{
		{"shop", "shop"},
		{"  Shop  ", "shop"},
		{"Shop-App", "shop-app"},
		{"shop app", "shop-app"},
		{"shop/app_v2.1", "shop-app_v2.1"},
		{"  应用 名  ", "应用-名"}, // 中文保留（非 a-z 字符被替换）
		{"--edge--", "edge"},
		{"", ""},
	}
	for _, c := range cases {
		if got := normalizeLabKey(c.in); got != c.want {
			t.Errorf("normalizeLabKey(%q) = %q, want %q", c.in, got, c.want)
		}
	}
	long := strings.Repeat("a", 80)
	if got := normalizeLabKey(long); len(got) != 64 {
		t.Errorf("long key length = %d, want 64", len(got))
	}
}

package agent

import (
	"archive/tar"
	"archive/zip"
	"compress/bzip2"
	"compress/gzip"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"sort"
	"strings"

	actool "github.com/Autumn-27/norma/tool"
)

// extract_archive：把上传的源码压缩包解到工作目录，供代码审计任务使用。
// 纯标准库实现（archive/zip、archive/tar、compress/gzip、compress/bzip2），无外部依赖。
//
// 安全约束（extractArchive 内强制）：
//   - zip-slip 防护：每个 entry 的目标路径经清洗后必须仍落在 dest 内；
//   - 符号链接/硬链接条目直接跳过（拒绝还原链接，防指向 dest 外的逃逸）；
//   - entry 数量上限（5 万）与总解压体积上限（2 GiB），防 zip bomb；
//   - 只接受 .zip/.tar/.tar.gz/.tgz/.tar.bz2，未知后缀报错而非猜格式。

const (
	extractMaxEntries = 50000
	extractMaxBytes   = int64(2) << 30 // 2 GiB
)

// extractArchiveTool builds the extract_archive domain tool. It needs no store
// binding — it operates purely on the local filesystem, so it works for worker
// runs and chat sessions alike.
func (t *ToolSet) extractArchiveTool() actool.CoreTool {
	return writeTool(
		"extract_archive",
		"把压缩包解压到指定目录（代码审计任务第一步）。支持 .zip / .tar / .tar.gz / .tgz / .tar.bz2。\n"+
			"dest 省略时解到压缩包同目录下与包同名的文件夹（去掉扩展名）。符号链接条目会跳过；超过 5 万条目或解压总量 2GiB 会中止。\n"+
			"返回 {dest, files, dirs, top_level:[顶层条目]}，top_level 供判断代码结构（哪个目录是站点根）。",
		obj(map[string]any{
			"path": str("压缩包路径（绝对路径，或相对当前工作目录）"),
			"dest": str("目标目录（可选；省略=压缩包同目录下去扩展名文件夹；不存在会创建）"),
		}),
		func(_ context.Context, in json.RawMessage) (actool.Result, error) {
			var a struct {
				Path string `json:"path"`
				Dest string `json:"dest"`
			}
			if err := json.Unmarshal(in, &a); err != nil {
				return actool.Errorf("invalid input: " + err.Error()), nil
			}
			src := strings.TrimSpace(a.Path)
			if src == "" {
				return actool.Errorf("path 不能为空"), nil
			}
			dest, files, dirs, topLevel, err := extractArchive(src, strings.TrimSpace(a.Dest))
			if err != nil {
				return actool.Errorf(err.Error()), nil
			}
			return jsonResult(map[string]any{
				"dest": dest, "files": files, "dirs": dirs, "top_level": topLevel,
			})
		},
	)
}

// archiveKind identifies the compression format by filename suffix.
type archiveKind int

const (
	archiveUnknown archiveKind = iota
	archiveZip
	archiveTar
	archiveTarGz
	archiveTarBz2
)

func archiveKindOf(name string) archiveKind {
	lower := strings.ToLower(name)
	switch {
	case strings.HasSuffix(lower, ".zip"):
		return archiveZip
	case strings.HasSuffix(lower, ".tar.gz"), strings.HasSuffix(lower, ".tgz"):
		return archiveTarGz
	case strings.HasSuffix(lower, ".tar.bz2"):
		return archiveTarBz2
	case strings.HasSuffix(lower, ".tar"):
		return archiveTar
	}
	return archiveUnknown
}

// defaultDestDir 返回省略 dest 时的目标目录：压缩包同目录、去掉扩展名的文件夹。
func defaultDestDir(src string) string {
	dir := filepath.Dir(src)
	base := filepath.Base(src)
	for _, ext := range []string{".tar.gz", ".tar.bz2", ".tgz", ".zip", ".tar"} {
		if strings.HasSuffix(strings.ToLower(base), ext) {
			base = base[:len(base)-len(ext)]
			break
		}
	}
	if base == "" {
		base = "extracted"
	}
	return filepath.Join(dir, base)
}

// extractArchive 解压 src 到 dest（空 dest 用默认目录），返回目标目录、文件数、
// 目录数、顶层条目列表。中途失败时已写入的部分保留（调用方可重试到同一 dest）。
func extractArchive(src, dest string) (string, int, int, []string, error) {
	kind := archiveKindOf(src)
	if kind == archiveUnknown {
		return "", 0, 0, nil, fmt.Errorf("不支持的压缩格式：%s（支持 .zip/.tar/.tar.gz/.tgz/.tar.bz2）", filepath.Base(src))
	}
	info, err := os.Stat(src)
	if err != nil {
		return "", 0, 0, nil, fmt.Errorf("读取压缩包失败: %v", err)
	}
	if info.IsDir() {
		return "", 0, 0, nil, fmt.Errorf("path 是目录不是压缩包: %s", src)
	}
	if dest == "" {
		dest = defaultDestDir(src)
	}
	if err := os.MkdirAll(dest, 0o755); err != nil {
		return "", 0, 0, nil, fmt.Errorf("创建目标目录失败: %v", err)
	}
	absDest, err := filepath.Abs(dest)
	if err != nil {
		return "", 0, 0, nil, err
	}

	var files, dirs int
	var topLevel = map[string]bool{}
	switch kind {
	case archiveZip:
		files, dirs, err = extractZip(src, absDest, topLevel)
	case archiveTar, archiveTarGz, archiveTarBz2:
		files, dirs, err = extractTar(src, absDest, kind, topLevel)
	}
	if err != nil {
		return dest, files, dirs, nil, err
	}
	names := make([]string, 0, len(topLevel))
	for n := range topLevel {
		names = append(names, n)
	}
	sort.Strings(names)
	return dest, files, dirs, names, nil
}

// joinEntry 拼接并校验 entry 目标路径：清洗后的路径必须仍在 root 内（zip-slip 防护）。
// 返回空串表示该 entry 被拒绝（调用方跳过并计入 skipped）。
func joinEntry(root, name string) string {
	name = strings.TrimPrefix(filepath.ToSlash(name), "./")
	if name == "" || name == "." {
		return ""
	}
	clean := filepath.Clean(filepath.FromSlash(name))
	if filepath.IsAbs(clean) || clean == ".." || strings.HasPrefix(clean, ".."+string(filepath.Separator)) {
		return ""
	}
	target := filepath.Join(root, clean)
	rel, err := filepath.Rel(root, target)
	if err != nil || rel == ".." || strings.HasPrefix(rel, ".."+string(filepath.Separator)) {
		return ""
	}
	return target
}

// noteTopLevel 把 entry 的顶层路径段记进 topLevel 集合。
func noteTopLevel(name string, topLevel map[string]bool) {
	name = strings.TrimPrefix(filepath.ToSlash(name), "./")
	if name == "" {
		return
	}
	if i := strings.IndexByte(name, '/'); i > 0 {
		topLevel[name[:i]] = true
	} else {
		topLevel[name] = true
	}
}

func extractZip(src, dest string, topLevel map[string]bool) (files, dirs int, err error) {
	zr, err := zip.OpenReader(src)
	if err != nil {
		return 0, 0, fmt.Errorf("打开 zip 失败: %v", err)
	}
	defer zr.Close()
	if len(zr.File) > extractMaxEntries {
		return 0, 0, fmt.Errorf("zip 条目数 %d 超过上限 %d", len(zr.File), extractMaxEntries)
	}
	var total int64
	for _, f := range zr.File {
		if f.FileInfo().IsDir() {
			if target := joinEntry(dest, f.Name); target != "" {
				if err := os.MkdirAll(target, 0o755); err != nil {
					return files, dirs, err
				}
				dirs++
				noteTopLevel(f.Name, topLevel)
			}
			continue
		}
		total += int64(f.UncompressedSize64)
		if total > extractMaxBytes {
			return files, dirs, fmt.Errorf("解压总量超过 %d 字节上限（疑似压缩炸弹），已中止", extractMaxBytes)
		}
		if err := extractZipFile(f, dest, topLevel); err != nil {
			return files, dirs, err
		}
		files++
		noteTopLevel(f.Name, topLevel)
	}
	return files, dirs, nil
}

func extractZipFile(f *zip.File, dest string, topLevel map[string]bool) error {
	target := joinEntry(dest, f.Name)
	if target == "" {
		return nil // 越界条目：跳过而非报错，继续解其余内容
	}
	if err := os.MkdirAll(filepath.Dir(target), 0o755); err != nil {
		return err
	}
	rc, err := f.Open()
	if err != nil {
		return fmt.Errorf("打开 zip 条目 %s 失败: %v", f.Name, err)
	}
	defer rc.Close()
	out, err := os.OpenFile(target, os.O_WRONLY|os.O_CREATE|os.O_TRUNC, 0o644)
	if err != nil {
		return err
	}
	defer out.Close()
	_, err = io.Copy(out, rc)
	return err
}

func extractTar(src, dest string, kind archiveKind, topLevel map[string]bool) (files, dirs int, err error) {
	f, err := os.Open(src)
	if err != nil {
		return 0, 0, err
	}
	defer f.Close()
	var fr io.Reader = f
	if kind == archiveTarGz {
		gz, err := gzip.NewReader(fr)
		if err != nil {
			return 0, 0, fmt.Errorf("gzip 流无效: %v", err)
		}
		defer gz.Close()
		fr = gz
	} else if kind == archiveTarBz2 {
		fr = bzip2.NewReader(fr)
	}
	tr := tar.NewReader(fr)
	var total int64
	for {
		hdr, err := tr.Next()
		if err == io.EOF {
			break
		}
		if err != nil {
			return files, dirs, fmt.Errorf("读 tar 条目失败: %v", err)
		}
		if files+dirs >= extractMaxEntries {
			return files, dirs, fmt.Errorf("tar 条目数超过上限 %d", extractMaxEntries)
		}
		if hdr.Typeflag == tar.TypeSymlink || hdr.Typeflag == tar.TypeLink || hdr.Typeflag == tar.TypeChar || hdr.Typeflag == tar.TypeBlock || hdr.Typeflag == tar.TypeFifo {
			continue // 链接/设备/管道条目一律跳过：不还原链接，防逃逸
		}
		target := joinEntry(dest, hdr.Name)
		if target == "" {
			continue // 越界或空名条目
		}
		switch hdr.Typeflag {
		case tar.TypeDir:
			if err := os.MkdirAll(target, 0o755); err != nil {
				return files, dirs, err
			}
			dirs++
			noteTopLevel(hdr.Name, topLevel)
		case tar.TypeReg:
			total += hdr.Size
			if total > extractMaxBytes {
				return files, dirs, fmt.Errorf("解压总量超过 %d 字节上限（疑似压缩炸弹），已中止", extractMaxBytes)
			}
			if err := os.MkdirAll(filepath.Dir(target), 0o755); err != nil {
				return files, dirs, err
			}
			out, err := os.OpenFile(target, os.O_WRONLY|os.O_CREATE|os.O_TRUNC, 0o644)
			if err != nil {
				return files, dirs, err
			}
			if _, err := io.Copy(out, tr); err != nil {
				out.Close()
				return files, dirs, fmt.Errorf("写 %s 失败: %v", hdr.Name, err)
			}
			out.Close()
			files++
			noteTopLevel(hdr.Name, topLevel)
		}
	}
	return files, dirs, nil
}

package postproc

import (
	"archive/zip"
	"context"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
)

var (
	rarPartRE = regexp.MustCompile(`(?i)(.+)\.part(\d+)\.rar$`)
)

type archiveKind int

const (
	archiveNone archiveKind = iota
	archiveZip
	archiveRAR
	archive7z
)

type archiveTarget struct {
	kind archiveKind
	path string // first part or whole archive
}

func findArchive(dir string) (archiveTarget, bool) {
	entries, err := os.ReadDir(dir)
	if err != nil {
		return archiveTarget{}, false
	}
	var (
		zipFile string
		rarMain string
		rarPart int
		sevenZ  string
	)
	for _, e := range entries {
		if e.IsDir() {
			continue
		}
		name := e.Name()
		lower := strings.ToLower(name)
		path := filepath.Join(dir, name)

		if strings.HasSuffix(lower, ".zip") && zipFile == "" {
			zipFile = path
			continue
		}
		if strings.HasSuffix(lower, ".7z") && sevenZ == "" {
			sevenZ = path
			continue
		}
		if strings.HasSuffix(lower, ".rar") {
			if m := rarPartRE.FindStringSubmatch(name); m != nil {
				n, _ := parseInt(m[2])
				if rarMain == "" || n < rarPart {
					rarMain = path
					rarPart = n
				}
				continue
			}
			if rarMain == "" || !strings.Contains(strings.ToLower(filepath.Base(rarMain)), ".part") {
				rarMain = path
				rarPart = 0
			}
		}
	}
	// Prefer RAR (common usenet), then 7z, then zip.
	if rarMain != "" {
		return archiveTarget{kind: archiveRAR, path: rarMain}, true
	}
	if sevenZ != "" {
		return archiveTarget{kind: archive7z, path: sevenZ}, true
	}
	if zipFile != "" {
		return archiveTarget{kind: archiveZip, path: zipFile}, true
	}
	return archiveTarget{}, false
}

func parseInt(s string) (int, bool) {
	n := 0
	for _, c := range s {
		if c < '0' || c > '9' {
			return 0, false
		}
		n = n*10 + int(c-'0')
	}
	return n, true
}

func extractArchive(ctx context.Context, dir string, target archiveTarget, cfg Config) (string, error) {
	outDir := filepath.Join(dir, "_unpack")
	if err := os.RemoveAll(outDir); err != nil && !os.IsNotExist(err) {
		return "", err
	}
	if err := os.MkdirAll(outDir, 0o755); err != nil {
		return "", err
	}

	var err error
	switch target.kind {
	case archiveZip:
		err = extractZip(target.path, outDir)
	case archiveRAR:
		err = extractRAR(ctx, target.path, outDir, cfg)
	case archive7z:
		err = extract7z(ctx, target.path, outDir, cfg)
	default:
		return dir, nil
	}
	if err != nil {
		return "", err
	}
	return outDir, nil
}

func extractZip(archive, outDir string) error {
	r, err := zip.OpenReader(archive)
	if err != nil {
		return fmt.Errorf("open zip: %w", err)
	}
	defer r.Close()
	for _, f := range r.File {
		if err := extractZipFile(f, outDir); err != nil {
			return err
		}
	}
	return nil
}

func extractZipFile(f *zip.File, outDir string) error {
	name := filepath.Clean(f.Name)
	if filepath.IsAbs(name) {
		return fmt.Errorf("zip absolute path: %s", f.Name)
	}
	if strings.HasPrefix(name, ".."+string(os.PathSeparator)) || name == ".." {
		return fmt.Errorf("zip path traversal: %s", f.Name)
	}
	dest := filepath.Join(outDir, name)
	if !containedIn(outDir, dest) {
		return fmt.Errorf("zip path escapes output dir: %s", f.Name)
	}
	if f.FileInfo().IsDir() {
		return os.MkdirAll(dest, 0o755)
	}
	if err := os.MkdirAll(filepath.Dir(dest), 0o755); err != nil {
		return err
	}
	rc, err := f.Open()
	if err != nil {
		return err
	}
	defer rc.Close()
	out, err := os.OpenFile(dest, os.O_CREATE|os.O_WRONLY|os.O_TRUNC, f.Mode())
	if err != nil {
		return err
	}
	defer out.Close()
	_, err = io.Copy(out, rc)
	return err
}

func containedIn(base, target string) bool {
	base = filepath.Clean(base)
	target = filepath.Clean(target)
	rel, err := filepath.Rel(base, target)
	if err != nil {
		return false
	}
	if rel == ".." || strings.HasPrefix(rel, ".."+string(os.PathSeparator)) {
		return false
	}
	return true
}

func extractRAR(ctx context.Context, archive, outDir string, cfg Config) error {
	cmdPath, err := resolveUnrar(cfg.UnrarCmd)
	if err != nil {
		return err
	}
	// unrar x -o+ -y -p- archive outDir/
	args := []string{"x", "-o+", "-y", "-p-", archive, outDir + string(os.PathSeparator)}
	return runCmd(ctx, cmdPath, args...)
}

func extract7z(ctx context.Context, archive, outDir string, cfg Config) error {
	cmdPath, err := resolveSevenZip(cfg.SevenZipCmd)
	if err != nil {
		return err
	}
	args := []string{"x", "-y", "-o" + outDir, archive}
	return runCmd(ctx, cmdPath, args...)
}

func resolveUnrar(preferred string) (string, error) {
	if preferred != "" {
		if p, err := exec.LookPath(preferred); err == nil {
			return p, nil
		}
		return preferred, nil
	}
	for _, name := range []string{"unrar", "unar", "bsdtar", "rar"} {
		if p, err := exec.LookPath(name); err == nil {
			return p, nil
		}
	}
	return "", fmt.Errorf("unrar not found: set UNRAR_CMD or install unrar")
}

func resolveSevenZip(candidates string) (string, error) {
	parts := strings.FieldsFunc(candidates, func(r rune) bool {
		return r == ',' || r == ' '
	})
	for _, name := range parts {
		if name == "" {
			continue
		}
		if p, err := exec.LookPath(name); err == nil {
			return p, nil
		}
	}
	if len(parts) == 1 && parts[0] != "" {
		return parts[0], nil
	}
	return "", fmt.Errorf("7z not found: set SEVENZIP_CMD or install p7zip/7zip")
}

func runCmd(ctx context.Context, bin string, args ...string) error {
	cmd := exec.CommandContext(ctx, bin, args...)
	out, err := cmd.CombinedOutput()
	if err != nil {
		return fmt.Errorf("%s %s: %w: %s", bin, strings.Join(args, " "), err, strings.TrimSpace(string(out)))
	}
	return nil
}

// sortRARParts orders multi-part rar paths (utility for future direct-read).
func sortRARParts(paths []string) {
	sort.Slice(paths, func(i, j int) bool {
		return strings.ToLower(paths[i]) < strings.ToLower(paths[j])
	})
}

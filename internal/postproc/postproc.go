package postproc

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"strings"

	par2lib "github.com/hobeone/par2engine/par2"
)

var par2VolRE = regexp.MustCompile(`(?i)\.vol\d+\+\d+\.par2$`)

// Result is the outcome of post-processing a download directory.
type Result struct {
	Storage   string
	Verified  bool
	Repaired  bool
	Extracted bool
}

// StatusFunc receives SABnzbd-like queue status updates.
type StatusFunc func(status string)

const (
	StatusVerifying  = "Verifying"
	StatusRepairing  = "Repairing"
	StatusExtracting = "Extracting"
)

// Process runs PAR2 verify/repair and archive extraction on a completed download dir.
func Process(ctx context.Context, dir string, cfg Config, status StatusFunc) (Result, error) {
	if status == nil {
		status = func(string) {}
	}
	res := Result{Storage: dir}

	par2Index, hasPar2 := findPar2Index(dir)
	if cfg.PAR2.enabled() {
		if !hasPar2 {
			if cfg.PAR2 == ModeRequire {
				return res, fmt.Errorf("par2 required but no .par2 index found in %s", dir)
			}
		} else {
			status(StatusVerifying)
			repaired, err := verifyRepairPAR2(ctx, par2Index, status)
			if err != nil {
				return res, err
			}
			res.Verified = true
			res.Repaired = repaired
		}
	}

	archive, hasArchive := findArchive(dir)
	if cfg.Unpack.enabled() {
		if !hasArchive {
			if cfg.Unpack == ModeRequire {
				return res, fmt.Errorf("unpack required but no archive found in %s", dir)
			}
		} else {
			status(StatusExtracting)
			out, err := extractArchive(ctx, dir, archive, cfg)
			if err != nil {
				return res, err
			}
			res.Extracted = true
			if out != "" {
				res.Storage = out
			}
		}
	}

	if cfg.Cleanup {
		cleanupArtifacts(dir, res.Storage)
	}

	if media := findLargestMediaFile(res.Storage); media != "" {
		res.Storage = media
	} else if info, err := os.Stat(res.Storage); err == nil && info.IsDir() {
		// keep directory when no single media file dominates
	} else if res.Storage == dir {
		if f := firstRegularFile(dir); f != "" {
			res.Storage = f
		}
	}
	return res, nil
}

func findPar2Index(dir string) (string, bool) {
	entries, err := os.ReadDir(dir)
	if err != nil {
		return "", false
	}
	var base []string
	for _, e := range entries {
		if e.IsDir() {
			continue
		}
		name := e.Name()
		lower := strings.ToLower(name)
		if !strings.HasSuffix(lower, ".par2") {
			continue
		}
		if par2VolRE.MatchString(name) {
			continue
		}
		base = append(base, filepath.Join(dir, name))
	}
	if len(base) == 0 {
		return "", false
	}
	// Prefer shortest basename (main index, not duplicate sets).
	best := base[0]
	for _, p := range base[1:] {
		if len(filepath.Base(p)) < len(filepath.Base(best)) {
			best = p
		}
	}
	return best, true
}

func verifyRepairPAR2(ctx context.Context, par2Index string, status StatusFunc) (repaired bool, err error) {
	opts := par2lib.DecoderOptions{
		NumGoroutines: parseThreads(),
		MemoryLimit:   parseMemMB() * 1024 * 1024,
	}
	dec, err := par2lib.NewDecoder(ctx, par2Index, opts)
	if err != nil {
		return false, fmt.Errorf("par2 open: %w", err)
	}

	drainProgress := func(ch chan par2lib.Progress) {
		go func() {
			for range ch {
			}
		}()
	}

	verifyProgress := make(chan par2lib.Progress, 8)
	drainProgress(verifyProgress)
	if err := dec.VerifyScans(ctx, verifyProgress); err != nil {
		close(verifyProgress)
		return false, fmt.Errorf("par2 verify: %w", err)
	}
	close(verifyProgress)

	counts := dec.ShardCounts()
	if !counts.RepairNeeded() {
		return false, nil
	}
	if !counts.RepairPossible() {
		return false, fmt.Errorf("par2 repair not possible: missing %d blocks, have %d parity",
			counts.UnusableDataShardCount, counts.UsableParityShardCount)
	}

	status(StatusRepairing)
	repairProgress := make(chan par2lib.Progress, 8)
	drainProgress(repairProgress)
	if err := dec.Repair(ctx, repairProgress); err != nil {
		close(repairProgress)
		return false, fmt.Errorf("par2 repair: %w", err)
	}
	close(repairProgress)

	reverifyProgress := make(chan par2lib.Progress, 8)
	drainProgress(reverifyProgress)
	if err := dec.VerifyScans(ctx, reverifyProgress); err != nil {
		close(reverifyProgress)
		return true, fmt.Errorf("par2 re-verify: %w", err)
	}
	close(reverifyProgress)

	final := dec.ShardCounts()
	if final.RepairNeeded() {
		return true, fmt.Errorf("par2 set still damaged after repair")
	}
	return true, nil
}

var mediaExt = map[string]struct{}{
	".mkv": {}, ".mp4": {}, ".avi": {}, ".m4v": {}, ".wmv": {},
	".mpg": {}, ".mpeg": {}, ".ts": {}, ".flv": {},
}

func findLargestMediaFile(root string) string {
	info, err := os.Stat(root)
	if err != nil {
		return ""
	}
	if !info.IsDir() {
		if isMediaExt(filepath.Base(root)) {
			return root
		}
		return ""
	}
	var best string
	var bestSize int64
	_ = filepath.WalkDir(root, func(path string, d os.DirEntry, err error) error {
		if err != nil || d.IsDir() {
			return nil
		}
		if !isMediaExt(d.Name()) {
			return nil
		}
		fi, err := d.Info()
		if err != nil {
			return nil
		}
		if fi.Size() > bestSize {
			bestSize = fi.Size()
			best = path
		}
		return nil
	})
	return best
}

func isMediaExt(name string) bool {
	ext := strings.ToLower(filepath.Ext(name))
	_, ok := mediaExt[ext]
	return ok
}

func firstRegularFile(dir string) string {
	entries, err := os.ReadDir(dir)
	if err != nil {
		return ""
	}
	for _, e := range entries {
		if e.IsDir() {
			continue
		}
		name := strings.ToLower(e.Name())
		if strings.HasSuffix(name, ".par2") || strings.HasSuffix(name, ".sfv") {
			continue
		}
		return filepath.Join(dir, e.Name())
	}
	return ""
}

func cleanupArtifacts(dir, keep string) {
	entries, err := os.ReadDir(dir)
	if err != nil {
		return
	}
	keepDir := filepath.Dir(keep)
	for _, e := range entries {
		if e.IsDir() {
			continue
		}
		name := strings.ToLower(e.Name())
		if strings.HasSuffix(name, ".par2") ||
			strings.HasSuffix(name, ".rar") ||
			strings.HasSuffix(name, ".r00") ||
			strings.HasSuffix(name, ".7z") ||
			strings.HasSuffix(name, ".zip") ||
			strings.HasSuffix(name, ".nfo") ||
			strings.HasSuffix(name, ".sfv") {
			_ = os.Remove(filepath.Join(dir, e.Name()))
		}
	}
	_ = keepDir
}

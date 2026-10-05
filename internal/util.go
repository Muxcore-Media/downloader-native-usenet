package internal

import (
	"context"
	"fmt"
	"io"
	"io/fs"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"sync"

	"github.com/Muxcore-Media/core/pkg/contracts"
)

// jobStatus values mirror SABnzbd-ish queue states for automation compatibility.
const (
	jobStatusQueued      = "Queued"
	jobStatusDownloading = "Downloading"
	jobStatusPaused      = "Paused"
	jobStatusVerifying   = "Verifying"
	jobStatusRepairing   = "Repairing"
	jobStatusExtracting  = "Extracting"
	jobStatusCompleted   = "Completed"
	jobStatusFailed      = "Failed"
)

type segmentError struct {
	MessageID  string `json:"message_id"`
	ArticleNum int    `json:"article_number"`
	Reason     string `json:"reason"`
}

type job struct {
	mu sync.Mutex

	id       string
	name     string
	category string
	nzbURL   string
	status   string
	progress float64
	total    int64
	done     int64
	storage  string
	errMsg   string
	paused   bool
	deleted  bool

	segmentErrors      []segmentError
	articlesExpected   int
	articlesDownloaded int
	verificationStatus string

	cancel context.CancelFunc
}

func (j *job) snapshot() (status, name, storage, errMsg string) {
	j.mu.Lock()
	defer j.mu.Unlock()
	return j.status, j.name, j.storage, j.errMsg
}

func (j *job) setStatus(status string) {
	j.mu.Lock()
	j.status = status
	j.mu.Unlock()
}

func (j *job) setProgress(total, done int64, progress float64) {
	j.mu.Lock()
	j.total = total
	j.done = done
	j.progress = progress
	j.mu.Unlock()
}

func (j *job) markCompleted(storage string) {
	j.mu.Lock()
	j.storage = storage
	j.done = j.total
	j.progress = 100
	j.status = jobStatusCompleted
	j.mu.Unlock()
}

func (j *job) markFailed(msg string) {
	j.mu.Lock()
	j.status = jobStatusFailed
	j.errMsg = msg
	j.mu.Unlock()
}

func (j *job) queueView() (id, name, category, status string, progress float64, total, remaining int64) {
	j.mu.Lock()
	defer j.mu.Unlock()
	rem := j.total - j.done
	if rem < 0 {
		rem = 0
	}
	return j.id, j.name, j.category, j.status, j.progress, j.total, rem
}

func (j *job) historyView() (id, name, category, status, storage string) {
	j.mu.Lock()
	defer j.mu.Unlock()
	return j.id, j.name, j.category, j.status, j.storage
}

func (j *job) isPaused() bool {
	j.mu.Lock()
	defer j.mu.Unlock()
	return j.paused
}

func (j *job) setPaused(paused bool) {
	j.mu.Lock()
	j.paused = paused
	j.mu.Unlock()
}

func (j *job) markDeleted() {
	j.mu.Lock()
	j.deleted = true
	j.mu.Unlock()
}

func (j *job) isDeleted() bool {
	j.mu.Lock()
	defer j.mu.Unlock()
	return j.deleted
}

func (j *job) addSegmentError(messageID string, articleNum int, reason string) {
	j.mu.Lock()
	j.segmentErrors = append(j.segmentErrors, segmentError{
		MessageID: messageID, ArticleNum: articleNum, Reason: reason,
	})
	j.mu.Unlock()
}

func (j *job) segmentErrorsSnapshot() []segmentError {
	j.mu.Lock()
	defer j.mu.Unlock()
	out := make([]segmentError, len(j.segmentErrors))
	copy(out, j.segmentErrors)
	return out
}

func (j *job) setArticleCounts(expected, downloaded int) {
	j.mu.Lock()
	j.articlesExpected = expected
	j.articlesDownloaded = downloaded
	j.mu.Unlock()
}

func (j *job) setVerificationStatus(status string) {
	j.mu.Lock()
	j.verificationStatus = status
	j.mu.Unlock()
}

func (j *job) verificationSnapshot() (expected, downloaded int, status string) {
	j.mu.Lock()
	defer j.mu.Unlock()
	return j.articlesExpected, j.articlesDownloaded, j.verificationStatus
}

func fetchNZB(ctx context.Context, client *http.Client, nzbURL string, allowPrivate bool) ([]byte, error) {
	if err := classifyNZBURL(nzbURL, allowPrivate); err != nil {
		return nil, err
	}
	if client == nil {
		client = newGuardedClient(allowPrivate)
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, nzbURL, nil)
	if err != nil {
		return nil, err
	}
	resp, err := client.Do(req)
	if err != nil {
		return nil, err
	}
	defer func() { _ = resp.Body.Close() }()
	if resp.StatusCode >= 300 {
		body, _ := io.ReadAll(io.LimitReader(resp.Body, 4096))
		return nil, fmt.Errorf("fetch nzb: HTTP %d: %s", resp.StatusCode, strings.TrimSpace(string(body)))
	}
	return io.ReadAll(io.LimitReader(resp.Body, 64<<20))
}

func sanitizeName(name string) string {
	name = strings.TrimSpace(name)
	if name == "" {
		return "release"
	}
	name = filepath.Base(name)
	name = strings.TrimSuffix(name, ".nzb")
	replacer := strings.NewReplacer("/", "_", "\\", "_", ":", "_", "..", "_")
	return replacer.Replace(name)
}

func jobDir(base, category, name string) string {
	parts := []string{base}
	if category != "" {
		parts = append(parts, sanitizeName(category))
	}
	parts = append(parts, sanitizeName(name))
	return filepath.Join(parts...)
}

func writeFixtureMedia(dir, name string, size int64) (string, error) {
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return "", err
	}
	base := sanitizeName(name)
	rel := base + ".mkv"
	abs := filepath.Join(dir, rel)
	payload := make([]byte, size)
	for i := range payload {
		payload[i] = byte(i % 251)
	}
	if err := os.WriteFile(abs, payload, 0o644); err != nil {
		return "", err
	}
	return abs, nil
}

func filesFromStorage(storage string) []contracts.DownloadEventFile {
	if storage == "" {
		return nil
	}
	info, err := os.Stat(storage)
	if err != nil {
		return nil
	}
	if !info.IsDir() {
		return []contracts.DownloadEventFile{{Path: storage, Size: info.Size()}}
	}
	var files []contracts.DownloadEventFile
	_ = filepath.WalkDir(storage, func(path string, d fs.DirEntry, err error) error {
		if err != nil || d.IsDir() {
			return nil
		}
		fi, err := d.Info()
		if err != nil {
			return nil
		}
		files = append(files, contracts.DownloadEventFile{Path: path, Size: fi.Size()})
		return nil
	})
	return files
}

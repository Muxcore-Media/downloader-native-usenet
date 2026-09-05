package internal

import (
	"context"
	"fmt"
	"log/slog"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"github.com/go-newsgroups/nzb"
	"github.com/google/uuid"

	"github.com/Muxcore-Media/downloader-native-usenet/internal/postproc"
)

type usenetEngine interface {
	RunJob(ctx context.Context, j *job, nzbData []byte, destDir string, pp postproc.Config) error
	NZBHealthCheck(ctx context.Context, nzbData []byte) (*NZBHealthReport, error)
}

type fixtureEngine struct{}

func (fixtureEngine) NZBHealthCheck(_ context.Context, nzbData []byte) (*NZBHealthReport, error) {
	doc, err := nzb.Parse(nzbData)
	if err != nil {
		return nil, fmt.Errorf("parse nzb: %w", err)
	}
	report := &NZBHealthReport{Files: make([]FileHealth, 0, len(doc.Files))}
	for i, f := range doc.Files {
		subject := strings.TrimSpace(f.Subject)
		if subject == "" {
			subject = fmt.Sprintf("file-%d", i+1)
		}
		msgID := ""
		if len(f.Segments) > 0 {
			msgID = f.Segments[0].MessageID
		}
		report.Files = append(report.Files, FileHealth{
			Subject: subject, Status: FileHealthOK, MessageID: msgID,
		})
	}
	return report, nil
}

func (fixtureEngine) RunJob(ctx context.Context, j *job, _ []byte, destDir string, pp postproc.Config) error {
	j.setProgress(8*1024, 0, 0)
	j.setStatus(jobStatusDownloading)

	select {
	case <-ctx.Done():
		return ctx.Err()
	default:
	}

	dir := jobDir(destDir, j.category, j.name)
	if _, err := writeFixtureMedia(dir, j.name, 8*1024); err != nil {
		return failJob(j, err)
	}
	_, err := finalizeDownload(ctx, j, dir, pp)
	return err
}

type jobManager struct {
	mu               sync.RWMutex
	jobs             map[string]*job
	history          []*job
	engine           usenetEngine
	destDir          string
	http             *httpClientWrap
	postprocCfg      postproc.Config
	allowPrivateURLs bool
}

type httpClientWrap struct {
	client *http.Client
}

func newJobManager(engine usenetEngine, destDir string, pp postproc.Config) *jobManager {
	if destDir == "" {
		destDir = "/var/lib/downloader-native-usenet/downloads"
	}
	return &jobManager{
		jobs:        make(map[string]*job),
		engine:      engine,
		destDir:     destDir,
		postprocCfg: pp,
	}
}

func (m *jobManager) setEngine(engine usenetEngine) {
	m.mu.Lock()
	m.engine = engine
	m.mu.Unlock()
}

func (m *jobManager) add(ctx context.Context, nzbURL, name, category string, paused bool) (*job, error) {
	if err := classifyNZBURL(nzbURL, m.allowPrivateURLs); err != nil {
		return nil, err
	}
	if name == "" {
		name = nzbURL
	}
	raw, err := fetchNZB(ctx, m.httpClient(), nzbURL, m.allowPrivateURLs)
	if err != nil {
		return nil, err
	}
	id := uuid.NewString()
	if err := m.cacheNZB(id, raw); err != nil {
		return nil, fmt.Errorf("cache nzb: %w", err)
	}
	j := &job{
		id:       id,
		name:     name,
		category: category,
		nzbURL:   nzbURL,
		status:   jobStatusQueued,
	}
	if paused {
		j.setStatus(jobStatusPaused)
		j.setPaused(true)
	}
	m.mu.Lock()
	m.jobs[id] = j
	m.mu.Unlock()
	m.persistSession()

	if !paused {
		m.startDownload(id, raw)
	}
	return j, nil
}

func (m *jobManager) httpClient() *http.Client {
	if m.http != nil && m.http.client != nil {
		return m.http.client
	}
	return nil
}

func (m *jobManager) startDownload(id string, nzbData []byte) {
	m.mu.RLock()
	j, ok := m.jobs[id]
	engine := m.engine
	pp := m.postprocCfg
	m.mu.RUnlock()
	if !ok || j.isDeleted() {
		return
	}
	ctx, cancel := context.WithCancel(context.Background())
	j.cancel = cancel
	go func() {
		err := engine.RunJob(ctx, j, nzbData, m.destDir, pp)
		st, _, _, errMsg := j.snapshot()
		if err != nil && st != jobStatusCompleted {
			if ctx.Err() != nil {
				j.setStatus(jobStatusPaused)
				j.setPaused(true)
				m.persistSession()
				return
			}
			if errMsg == "" {
				j.markFailed(err.Error())
			}
		}
		m.mu.Lock()
		if j.isDeleted() {
			m.mu.Unlock()
			return
		}
		delete(m.jobs, id)
		m.history = prependHistory(m.history, j, 100)
		m.mu.Unlock()
		m.persistSession()
	}()
}

func prependHistory(hist []*job, j *job, limit int) []*job {
	out := []*job{j}
	for _, old := range hist {
		if old.id == j.id {
			continue
		}
		out = append(out, old)
		if len(out) >= limit {
			break
		}
	}
	return out
}

func (m *jobManager) queueSnapshot(category string) []*job {
	m.mu.RLock()
	defer m.mu.RUnlock()
	out := make([]*job, 0, len(m.jobs))
	for _, j := range m.jobs {
		if category != "" {
			_, _, cat, _, _, _, _ := j.queueView()
			if cat != category {
				continue
			}
		}
		out = append(out, j)
	}
	return out
}

func (m *jobManager) historySnapshot(limit int) []*job {
	m.mu.RLock()
	defer m.mu.RUnlock()
	if limit <= 0 || limit > len(m.history) {
		limit = len(m.history)
	}
	out := make([]*job, limit)
	copy(out, m.history[:limit])
	return out
}

func (m *jobManager) pause(id string) error {
	m.mu.Lock()
	if id == "" {
		for _, j := range m.jobs {
			j.setPaused(true)
			j.setStatus(jobStatusPaused)
			if j.cancel != nil {
				j.cancel()
			}
		}
		m.mu.Unlock()
		m.persistSession()
		return nil
	}
	j, ok := m.jobs[id]
	if !ok {
		m.mu.Unlock()
		return fmt.Errorf("job %q not found", id)
	}
	j.setPaused(true)
	j.setStatus(jobStatusPaused)
	if j.cancel != nil {
		j.cancel()
	}
	m.mu.Unlock()
	m.persistSession()
	return nil
}

func (m *jobManager) resume(id string, nzbFetcher func(context.Context, string) ([]byte, error)) error {
	m.mu.Lock()
	if id == "" {
		ids := make([]string, 0, len(m.jobs))
		for jid, j := range m.jobs {
			if j.isPaused() {
				ids = append(ids, jid)
			}
		}
		m.mu.Unlock()
		for _, jid := range ids {
			if err := m.resumeOne(jid, nzbFetcher); err != nil {
				return err
			}
		}
		return nil
	}
	j, ok := m.jobs[id]
	if !ok {
		m.mu.Unlock()
		return fmt.Errorf("job %q not found", id)
	}
	if !j.isPaused() {
		m.mu.Unlock()
		return nil
	}
	j.setPaused(false)
	j.setStatus(jobStatusQueued)
	m.mu.Unlock()
	m.persistSession()
	return m.resumeOne(id, nzbFetcher)
}

func (m *jobManager) resumeOne(id string, nzbFetcher func(context.Context, string) ([]byte, error)) error {
	go func() {
		raw, err := m.loadCachedNZB(id)
		if err != nil {
			m.mu.RLock()
			j, ok := m.jobs[id]
			var url string
			if ok {
				url = j.nzbURL
			}
			m.mu.RUnlock()
			if !ok {
				return
			}
			ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
			raw, err = nzbFetcher(ctx, url)
			cancel()
			if err != nil {
				slog.Warn("resume fetch nzb failed", "job", id, "error", err)
				return
			}
			_ = m.cacheNZB(id, raw)
		}
		m.startDownload(id, raw)
	}()
	return nil
}

func (m *jobManager) delete(id string, deleteFiles bool) error {
	m.mu.Lock()
	j, ok := m.jobs[id]
	if ok {
		j.markDeleted()
		if j.cancel != nil {
			j.cancel()
		}
		delete(m.jobs, id)
	}
	category, name := "", ""
	if ok {
		_, name, category, _, _, _, _ = j.queueView()
	}
	m.mu.Unlock()
	if !ok {
		return fmt.Errorf("job %q not found", id)
	}
	if deleteFiles {
		dir := jobDir(m.destDir, category, name)
		_ = os.RemoveAll(dir)
	}
	_ = os.Remove(filepath.Join(m.nzbCacheDir(), id+".nzb"))
	m.persistSession()
	return nil
}

func (m *jobManager) findHistory(id string) *job {
	m.mu.RLock()
	defer m.mu.RUnlock()
	for _, j := range m.history {
		if j.id == id {
			return j
		}
	}
	return nil
}

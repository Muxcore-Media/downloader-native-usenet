package internal

import (
	"encoding/json"
	"log/slog"
	"os"
	"path/filepath"
)

const usenetSessionFile = ".muxcore-usenet.json"

type persistedJob struct {
	ID       string `json:"id"`
	Name     string `json:"name"`
	Category string `json:"category"`
	NZBURL   string `json:"nzb_url"`
	Status   string `json:"status"`
	Paused   bool   `json:"paused,omitempty"`
	Storage  string `json:"storage,omitempty"`
}

type usenetSession struct {
	Queue   []persistedJob `json:"queue"`
	History []persistedJob `json:"history"`
}

func (m *jobManager) sessionPath() string {
	return filepath.Join(m.destDir, usenetSessionFile)
}

func (m *jobManager) nzbCacheDir() string {
	return filepath.Join(m.destDir, ".nzb-cache")
}

func (m *jobManager) cacheNZB(id string, data []byte) error {
	dir := m.nzbCacheDir()
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return err
	}
	return os.WriteFile(filepath.Join(dir, id+".nzb"), data, 0o600)
}

func (m *jobManager) loadCachedNZB(id string) ([]byte, error) {
	return os.ReadFile(filepath.Join(m.nzbCacheDir(), id+".nzb"))
}

func (m *jobManager) persistSession() {
	m.mu.RLock()
	queue := make([]persistedJob, 0, len(m.jobs))
	for _, j := range m.jobs {
		queue = append(queue, jobToPersisted(j))
	}
	history := make([]persistedJob, 0, len(m.history))
	for _, j := range m.history {
		history = append(history, jobToPersisted(j))
	}
	m.mu.RUnlock()

	data, err := json.MarshalIndent(usenetSession{Queue: queue, History: history}, "", "  ")
	if err != nil {
		slog.Warn("persist usenet session marshal", "error", err)
		return
	}
	path := m.sessionPath()
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		slog.Warn("persist usenet session mkdir", "error", err)
		return
	}
	tmp := path + ".tmp"
	if err := os.WriteFile(tmp, data, 0o600); err != nil {
		slog.Warn("persist usenet session write", "error", err)
		return
	}
	if err := os.Rename(tmp, path); err != nil {
		_ = os.Remove(tmp)
		slog.Warn("persist usenet session rename", "error", err)
	}
}

func jobToPersisted(j *job) persistedJob {
	st, name, storage, _ := j.snapshot()
	_, _, category, _, _, _, _ := j.queueView()
	return persistedJob{
		ID:       j.id,
		Name:     name,
		Category: category,
		NZBURL:   j.nzbURL,
		Status:   st,
		Paused:   j.isPaused(),
		Storage:  storage,
	}
}

func (m *jobManager) restoreSession() {
	data, err := os.ReadFile(m.sessionPath())
	if err != nil {
		return
	}
	var sess usenetSession
	if err := json.Unmarshal(data, &sess); err != nil {
		slog.Warn("parse usenet session", "error", err)
		return
	}
	for _, rec := range sess.History {
		j := persistedToJob(rec)
		m.mu.Lock()
		m.history = prependHistory(m.history, j, 100)
		m.mu.Unlock()
	}
	for _, rec := range sess.Queue {
		if err := classifyNZBURL(rec.NZBURL, m.allowPrivateURLs); err != nil {
			slog.Warn("skip persisted queue job", "id", rec.ID, "error", err)
			continue
		}
		j := persistedToJob(rec)
		m.mu.Lock()
		m.jobs[j.id] = j
		m.mu.Unlock()
		if rec.Paused {
			continue
		}
		raw, err := m.loadCachedNZB(j.id)
		if err != nil {
			slog.Warn("restore job missing cached nzb", "id", j.id, "error", err)
			j.markFailed("cached nzb missing after restart")
			m.finishJob(j)
			continue
		}
		m.startDownload(j.id, raw)
	}
}

func persistedToJob(rec persistedJob) *job {
	j := &job{
		id:       rec.ID,
		name:     rec.Name,
		category: rec.Category,
		nzbURL:   rec.NZBURL,
		status:   rec.Status,
	}
	if rec.Paused {
		j.setPaused(true)
		j.setStatus(jobStatusPaused)
	}
	if rec.Storage != "" {
		j.markCompleted(rec.Storage)
	}
	return j
}

func (m *jobManager) finishJob(j *job) {
	m.mu.Lock()
	delete(m.jobs, j.id)
	m.history = prependHistory(m.history, j, 100)
	m.mu.Unlock()
	m.persistSession()
}

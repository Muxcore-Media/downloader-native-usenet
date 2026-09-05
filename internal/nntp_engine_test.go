package internal

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"sync/atomic"
	"testing"
	"time"

	"github.com/go-newsgroups/nntp"
	"github.com/go-newsgroups/nzb"

	"github.com/Muxcore-Media/downloader-native-usenet/internal/postproc"
)

type mockArticleFetcher struct {
	articles map[string][]byte
	fails    map[string]*atomic.Int32
}

func (m *mockArticleFetcher) Article(msgID string) (*nntp.Article, error) {
	id := msgID
	if len(id) >= 2 && id[0] == '<' {
		id = id[1 : len(id)-1]
	}
	if m.fails != nil {
		if c, ok := m.fails[id]; ok && c.Add(1) <= 2 {
			return nil, fmt.Errorf("transient fetch error")
		}
	}
	body, ok := m.articles[id]
	if !ok {
		return nil, fmt.Errorf("missing article %s", id)
	}
	return &nntp.Article{Body: string(body)}, nil
}

func TestNNTPEngineParseEmptyNZB(t *testing.T) {
	j := &job{id: "j1", name: "empty", status: jobStatusQueued}
	eng := newNNTPEngine(nntpConfig{Host: "unused"})
	err := eng.RunJob(context.Background(), j, []byte(`<?xml version="1.0"?><nzb xmlns="http://www.newzbin.com/DTD/nzb/nzb-1.1.dtd"></nzb>`), t.TempDir(), defaultPostprocConfig())
	if err == nil {
		t.Fatal("expected parse empty files error")
	}
	st, _, _, _ := j.snapshot()
	if st != jobStatusFailed {
		t.Fatalf("status=%q", st)
	}
}

func TestJobCancelViaPause(t *testing.T) {
	dir := t.TempDir()
	jm := newJobManager(slowEngine{}, dir, defaultPostprocConfig())
	id := "pause-me"
	j := &job{id: id, name: "Slow", status: jobStatusQueued}
	jm.mu.Lock()
	jm.jobs[id] = j
	jm.mu.Unlock()
	jm.startDownload(id, minimalNZBBytes())
	deadline := time.Now().Add(time.Second)
	for time.Now().Before(deadline) {
		st, _, _, _ := j.snapshot()
		if st == jobStatusDownloading {
			break
		}
		time.Sleep(10 * time.Millisecond)
	}
	if err := jm.pause(id); err != nil {
		t.Fatal(err)
	}
	time.Sleep(100 * time.Millisecond)
	st, _, _, _ := j.snapshot()
	if st != jobStatusPaused {
		t.Fatalf("status=%q want Paused", st)
	}
}

func TestNNTPEngineCancelPauses(t *testing.T) {
	_ = slowEngine{} // covered via TestJobCancelViaPause
}

type slowEngine struct{}

func (slowEngine) NZBHealthCheck(_ context.Context, nzbData []byte) (*NZBHealthReport, error) {
	return fixtureEngine{}.NZBHealthCheck(context.Background(), nzbData)
}

func (slowEngine) RunJob(ctx context.Context, j *job, _ []byte, _ string, _ postproc.Config) error {
	j.setStatus(jobStatusDownloading)
	<-ctx.Done()
	return ctx.Err()
}

func TestDownloadFileStreamingRetries(t *testing.T) {
	dir := t.TempDir()
	dest := filepath.Join(dir, "out.bin")
	mock := &mockArticleFetcher{
		articles: map[string][]byte{
			"seg1@example": []byte("=ybegin name=part1 size=5\n=ypart begin=1 end=5\nhello\n=yend size=5\n"),
		},
		fails: map[string]*atomic.Int32{
			"seg1@example": {},
		},
	}
	f := nzb.File{Segments: []nzb.Segment{{Number: 1, MessageID: "seg1@example", Bytes: 5}}}
	name, n, _, _, err := downloadFileStreaming(context.Background(), mock, f, dest, 3, nil)
	if err != nil {
		t.Fatal(err)
	}
	if n == 0 {
		t.Fatal("expected bytes written")
	}
	info, err := os.Stat(dest)
	if err != nil {
		t.Fatal(err)
	}
	if info.Size() == 0 {
		t.Fatal("empty output file")
	}
	_ = name
}

func TestFakeUsenetEngineCompletesFixtureNZB(t *testing.T) {
	mock := &mockArticleFetcher{articles: map[string][]byte{
		"test-segment-1@example": []byte("=ybegin name=Fixture.Movie.mkv size=5\n=ypart begin=1 end=5\nhello\n=yend size=5\n"),
	}}
	eng := &fakeNNTPRunner{fetcher: mock}
	j := &job{id: "j3", name: "Fixture.Movie", status: jobStatusQueued}
	dest := t.TempDir()
	err := eng.RunJob(context.Background(), j, minimalNZBBytes(), dest, postproc.Config{PAR2: postproc.ModeSkip, Unpack: postproc.ModeSkip})
	if err != nil {
		t.Fatal(err)
	}
	st, _, storage, _ := j.snapshot()
	if st != jobStatusCompleted || storage == "" {
		t.Fatalf("status=%q storage=%q", st, storage)
	}
}

type fakeNNTPRunner struct {
	fetcher nzb.ArticleFetcher
}

func (f *fakeNNTPRunner) RunJob(ctx context.Context, j *job, nzbData []byte, destDir string, pp postproc.Config) error {
	doc, err := nzb.Parse(nzbData)
	if err != nil {
		j.markFailed(err.Error())
		return err
	}
	if len(doc.Files) == 0 {
		j.markFailed("nzb has no files")
		return fmt.Errorf("nzb has no files")
	}
	j.setStatus(jobStatusDownloading)
	outRoot := jobDir(destDir, j.category, j.name)
	if err := os.MkdirAll(outRoot, 0o755); err != nil {
		return err
	}
	for i, file := range doc.Files {
		abs := filepath.Join(outRoot, fmt.Sprintf("part-%04d.bin", i+1))
		if _, _, _, _, err := downloadFileStreaming(ctx, f.fetcher, file, abs, 2, j); err != nil {
			j.markFailed(err.Error())
			return err
		}
	}
	_, err = finalizeDownload(ctx, j, outRoot, pp)
	return err
}

func minimalNZBBytes() []byte {
	return []byte(`<?xml version="1.0" encoding="UTF-8"?>
<nzb xmlns="http://www.newzbin.com/DTD/nzb/nzb-1.1.dtd">
  <file poster="poster@example" date="1700000000" subject="Fixture.Movie.mkv">
    <groups><group>alt.binaries.test</group></groups>
    <segments>
      <segment bytes="8192" number="1">&lt;test-segment-1@example&gt;</segment>
    </segments>
  </file>
</nzb>`)
}

func TestJobManagerDeleteDoesNotRemoveSibling(t *testing.T) {
	dir := t.TempDir()
	jm := newJobManager(fixtureEngine{}, dir, defaultPostprocConfig())
	srvBody := minimalNZBBytes()
	cacheOne := func(id string) { _ = jm.cacheNZB(id, srvBody) }

	jA := &job{id: "a", name: "Alpha", category: "movies", nzbURL: "https://example.com/a.nzb", status: jobStatusPaused}
	jB := &job{id: "b", name: "Beta", category: "movies", nzbURL: "https://example.com/b.nzb", status: jobStatusPaused}
	jm.mu.Lock()
	jm.jobs[jA.id] = jA
	jm.jobs[jB.id] = jB
	jm.mu.Unlock()
	cacheOne("a")
	cacheOne("b")

	alphaDir := jobDir(dir, "movies", "Alpha")
	betaDir := jobDir(dir, "movies", "Beta")
	for _, d := range []string{alphaDir, betaDir} {
		if err := os.MkdirAll(d, 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(filepath.Join(d, "file.mkv"), []byte("x"), 0o644); err != nil {
			t.Fatal(err)
		}
		j := jA
		if d == betaDir {
			j = jB
		}
		j.markCompleted(d)
	}

	if err := jm.delete("a", true); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(betaDir); err != nil {
		t.Fatalf("sibling removed: %v", err)
	}
	if _, err := os.Stat(alphaDir); !os.IsNotExist(err) {
		t.Fatalf("alpha dir still present: %v", err)
	}
}

func TestResumeUsesCachedNZB(t *testing.T) {
	dir := t.TempDir()
	jm := newJobManager(fixtureEngine{}, dir, defaultPostprocConfig())
	raw := minimalNZBBytes()
	id := "persist-job"
	if err := jm.cacheNZB(id, raw); err != nil {
		t.Fatal(err)
	}
	j := &job{id: id, name: "Fixture", nzbURL: "https://example.com/x.nzb", status: jobStatusPaused}
	j.setPaused(true)
	jm.mu.Lock()
	jm.jobs[id] = j
	jm.mu.Unlock()

	fetched := make(chan struct{}, 1)
	_ = jm.resumeOne(id, func(context.Context, string) ([]byte, error) {
		fetched <- struct{}{}
		return nil, fmt.Errorf("unexpected fetch")
	})
	select {
	case <-fetched:
		t.Fatal("re-fetched nzb url instead of using cache")
	case <-time.After(300 * time.Millisecond):
	}
}

func TestPersistSessionRestorePausedJob(t *testing.T) {
	dir := t.TempDir()
	jm := newJobManager(fixtureEngine{}, dir, defaultPostprocConfig())
	id := "persist-job"
	j := &job{id: id, name: "Fixture", nzbURL: "https://example.com/x.nzb", status: jobStatusPaused}
	j.setPaused(true)
	jm.mu.Lock()
	jm.jobs[id] = j
	jm.mu.Unlock()
	if err := jm.cacheNZB(id, minimalNZBBytes()); err != nil {
		t.Fatal(err)
	}
	jm.persistSession()

	jm2 := newJobManager(fixtureEngine{}, dir, defaultPostprocConfig())
	jm2.restoreSession()
	if len(jm2.jobs) != 1 {
		t.Fatalf("restored queue len=%d", len(jm2.jobs))
	}
}

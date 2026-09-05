package internal_test

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"sync"
	"testing"
	"time"

	"google.golang.org/grpc"
	"google.golang.org/grpc/credentials/insecure"

	"github.com/Muxcore-Media/core/pkg/contracts"
	"github.com/Muxcore-Media/downloader-native-usenet/internal"
	usenetv1 "github.com/Muxcore-Media/downloader-native-usenet/proto/gen/muxcore/usenet/v1"
)

const minimalNZB = `<?xml version="1.0" encoding="UTF-8"?>
<!DOCTYPE nzb PUBLIC "-//newzBin//DTD NZB 1.1//EN" "http://www.newzbin.com/DTD/nzb/nzb-1.1.dtd">
<nzb xmlns="http://www.newzbin.com/DTD/nzb/nzb-1.1.dtd">
  <file poster="poster@example" date="1700000000" subject="Fixture.Movie.mkv">
    <groups><group>alt.binaries.test</group></groups>
    <segments>
      <segment bytes="8192" number="1">&lt;test-segment-1@example&gt;</segment>
    </segments>
  </file>
</nzb>`

type recPub struct {
	mu   sync.Mutex
	evts []recEvt
}

type recEvt struct {
	Type    string
	Payload contracts.DownloadEventPayload
}

func (r *recPub) Publish(_ context.Context, eventType string, payload []byte) error {
	var p contracts.DownloadEventPayload
	_ = json.Unmarshal(payload, &p)
	r.mu.Lock()
	r.evts = append(r.evts, recEvt{Type: eventType, Payload: p})
	r.mu.Unlock()
	return nil
}

func (r *recPub) wait(t *testing.T, typ string, d time.Duration) recEvt {
	t.Helper()
	deadline := time.Now().Add(d)
	for time.Now().Before(deadline) {
		r.mu.Lock()
		for _, e := range r.evts {
			if e.Type == typ {
				r.mu.Unlock()
				return e
			}
		}
		r.mu.Unlock()
		time.Sleep(5 * time.Millisecond)
	}
	t.Fatalf("timeout waiting for %s", typ)
	return recEvt{}
}

func nzbFixtureServer(t *testing.T) *httptest.Server {
	t.Helper()
	return httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/x-nzb")
		_, _ = w.Write([]byte(minimalNZB))
	}))
}

func startTestModule(t *testing.T, pub *recPub) (*internal.Module, usenetv1.UsenetDownloaderServiceClient, *httptest.Server) {
	t.Helper()
	srv := nzbFixtureServer(t)
	m := internal.NewModule(internal.Config{
		Engine:              "fixture",
		DestDir:             t.TempDir(),
		GRPCAddr:            "127.0.0.1:0",
		HTTPAddr:            "127.0.0.1:0",
		Publish:             pub.Publish,
		HTTPClient:          srv.Client(),
		AllowPrivateNZBURLs: true,
	})
	ctx := context.Background()
	if err := m.Init(ctx); err != nil {
		t.Fatal(err)
	}
	if err := m.Start(ctx); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = m.Stop(ctx) })
	conn, err := grpc.NewClient(m.GRPCListenAddr(), grpc.WithTransportCredentials(insecure.NewCredentials()))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = conn.Close() })
	return m, usenetv1.NewUsenetDownloaderServiceClient(conn), srv
}

func TestOfflineDispatchFixture(t *testing.T) {
	srv := nzbFixtureServer(t)
	defer srv.Close()

	pub := &recPub{}
	m := internal.NewModule(internal.Config{
		Engine:              "fixture",
		DestDir:             t.TempDir(),
		GRPCAddr:            "127.0.0.1:0",
		HTTPAddr:            "127.0.0.1:0",
		Publish:             pub.Publish,
		HTTPClient:          srv.Client(),
		AllowPrivateNZBURLs: true,
	})

	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()

	jobID, err := m.OfflineDispatch(ctx, srv.URL+"/fixture.nzb", "Fixture.Movie", "movies")
	if err != nil {
		t.Fatal(err)
	}
	if jobID == "" {
		t.Fatal("expected job id")
	}
	started := pub.wait(t, contracts.EventDownloadStarted, time.Second)
	if started.Payload.Name != "Fixture.Movie" {
		t.Fatalf("started name: %+v", started.Payload)
	}
	completed := pub.wait(t, contracts.EventDownloadCompleted, time.Second)
	if completed.Payload.SavePath == "" {
		t.Fatalf("expected save path: %+v", completed.Payload)
	}
	if len(completed.Payload.Files) == 0 {
		t.Fatalf("expected completed files: %+v", completed.Payload)
	}
	if completed.Payload.Files[0].Size <= 0 {
		t.Fatalf("expected file size: %+v", completed.Payload.Files)
	}
}

func TestModuleInfo(t *testing.T) {
	m := internal.NewModule(internal.Config{GRPCAddr: ":9622"})
	info := m.Info()
	if info.ID != "downloader-native-usenet" {
		t.Fatalf("id: %s", info.ID)
	}
	if info.HTTPAddr != ":9622" {
		t.Fatalf("HTTPAddr should be gRPC addr, got %q", info.HTTPAddr)
	}
	found := false
	for _, c := range info.Capabilities {
		if c == "downloader.native.usenet" {
			found = true
		}
	}
	if !found {
		t.Fatal("missing downloader.native.usenet capability")
	}
}

func TestGetCapabilitiesBackend(t *testing.T) {
	m := internal.NewModule(internal.Config{Engine: "fixture"})
	if got := m.CapabilitiesBackend(); got != "native-usenet" {
		t.Fatalf("backend: %s", got)
	}
}

func TestGRPCAddNZBPausedListPauseResumeDeleteHistory(t *testing.T) {
	pub := &recPub{}
	_, client, srv := startTestModule(t, pub)
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	addResp, err := client.AddNZB(ctx, &usenetv1.AddNZBRequest{
		NzbUrl: srv.URL + "/paused.nzb", Name: "Paused.Release", Category: "tv", Paused: true,
	})
	if err != nil {
		t.Fatal(err)
	}
	if addResp.GetJobId() == "" {
		t.Fatal("empty job id")
	}

	queue, err := client.ListQueue(ctx, &usenetv1.ListQueueRequest{Category: "tv"})
	if err != nil {
		t.Fatal(err)
	}
	if len(queue.GetItems()) != 1 || queue.GetItems()[0].GetStatus() != "Paused" {
		t.Fatalf("queue: %+v", queue.GetItems())
	}

	if _, err := client.Pause(ctx, &usenetv1.PauseRequest{Id: addResp.GetJobId()}); err != nil {
		t.Fatal(err)
	}
	if _, err := client.Resume(ctx, &usenetv1.ResumeRequest{Id: addResp.GetJobId()}); err != nil {
		t.Fatal(err)
	}
	pub.wait(t, contracts.EventDownloadCompleted, 3*time.Second)

	hist, err := client.GetHistory(ctx, &usenetv1.GetHistoryRequest{Limit: 10})
	if err != nil {
		t.Fatal(err)
	}
	if len(hist.GetItems()) == 0 {
		t.Fatal("expected history")
	}

	add2, err := client.AddNZB(ctx, &usenetv1.AddNZBRequest{
		NzbUrl: srv.URL + "/delete.nzb", Name: "Delete.Me", Paused: true,
	})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := client.Delete(ctx, &usenetv1.DeleteRequest{Id: add2.GetJobId(), DeleteFiles: true}); err != nil {
		t.Fatal(err)
	}
}

func TestMeshSettingsUpdateSetting(t *testing.T) {
	pub := &recPub{}
	m, _, _ := startTestModule(t, pub)
	if err := m.UpdateSetting("nntp_host", "grpc-test.example.com"); err != nil {
		t.Fatal(err)
	}
	if err := m.UpdateSetting("engine", "live"); err != nil {
		t.Fatal(err)
	}
	defs := m.Settings()
	var host string
	for _, d := range defs {
		if d.Key == "nntp_host" {
			host = d.Value
		}
	}
	if host != "grpc-test.example.com" {
		t.Fatalf("host=%q", host)
	}
	if err := m.UpdateSetting("engine", "fixture"); err != nil {
		t.Fatal(err)
	}
}

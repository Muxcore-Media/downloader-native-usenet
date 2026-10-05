package internal

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"net"
	"net/http"
	"os"
	"strings"
	"sync"
	"time"

	"google.golang.org/grpc"

	"github.com/Muxcore-Media/core/pkg/contracts"
	"github.com/Muxcore-Media/core/sdk/go/client"
	modulesdk "github.com/Muxcore-Media/core/sdk/go/module"
	manifest "github.com/Muxcore-Media/downloader-native-usenet"
	"github.com/Muxcore-Media/downloader-native-usenet/internal/postproc"
	usenetv1 "github.com/Muxcore-Media/downloader-native-usenet/proto/gen/muxcore/usenet/v1"
)

type EventPublisher func(ctx context.Context, eventType string, payload []byte) error

type Module struct {
	id       string
	grpcAddr string
	httpAddr string
	destDir  string
	engine   string

	cfgMu       sync.RWMutex
	nntpCfg     nntpConfig
	postprocCfg postproc.Config

	jobs *jobManager

	grpcSrv *grpc.Server
	lis     net.Listener
	httpSrv *http.Server

	pubMu   sync.RWMutex
	publish EventPublisher

	mc *client.Client
}

type Config struct {
	ID         string
	GRPCAddr   string
	HTTPAddr   string
	DestDir    string
	Engine     string
	Publish    EventPublisher
	HTTPClient *http.Client
	// AllowPrivateNZBURLs disables SSRF host blocking (httptest fixtures only).
	AllowPrivateNZBURLs bool
}

func NewModule(cfg Config) *Module {
	if cfg.ID == "" {
		cfg.ID = "downloader-native-usenet"
	}
	if cfg.GRPCAddr == "" {
		if v := os.Getenv("USENET_GRPC_ADDR"); v != "" {
			cfg.GRPCAddr = v
		}
		if v := os.Getenv("MUXCORE_GRPC_ADDR_OVERRIDE"); v != "" {
			cfg.GRPCAddr = v
		}
		if cfg.GRPCAddr == "" {
			cfg.GRPCAddr = ":9622"
		}
	}
	if cfg.HTTPAddr == "" {
		if v := os.Getenv("MUXCORE_HTTP_ADDR"); v != "" {
			cfg.HTTPAddr = v
		}
		if cfg.HTTPAddr == "" {
			cfg.HTTPAddr = ":9623"
		}
	}
	if cfg.DestDir == "" {
		if v := os.Getenv("USENET_DOWNLOAD_DIR"); v != "" {
			cfg.DestDir = v
		} else if v := os.Getenv("DOWNLOAD_DIR"); v != "" {
			cfg.DestDir = v
		}
		if cfg.DestDir == "" {
			cfg.DestDir = "/var/lib/downloader-native-usenet/downloads"
		}
	}
	engine := cfg.Engine
	if engine == "" {
		engine = os.Getenv("USENET_ENGINE")
	}
	if engine == "" {
		engine = "fixture"
	}

	m := &Module{
		id:          cfg.ID,
		grpcAddr:    cfg.GRPCAddr,
		httpAddr:    cfg.HTTPAddr,
		destDir:     cfg.DestDir,
		engine:      engine,
		nntpCfg:     defaultNNTPConfigFromEnv(),
		postprocCfg: defaultPostprocConfig(),
		publish:     cfg.Publish,
	}
	m.loadPersistedSettings()
	if cfg.DestDir != "" {
		m.destDir = cfg.DestDir
	}
	if cfg.Engine != "" {
		m.engine = cfg.Engine
	}
	m.jobs = newJobManager(m.buildEngine(), m.destDir, m.postprocConfig())
	m.jobs.allowPrivateURLs = cfg.AllowPrivateNZBURLs
	if cfg.HTTPClient != nil {
		m.jobs.http = &httpClientWrap{client: cfg.HTTPClient}
	}
	return m
}

func (m *Module) postprocConfig() postproc.Config {
	m.cfgMu.RLock()
	defer m.cfgMu.RUnlock()
	return m.postprocCfg
}

func (m *Module) buildEngine() usenetEngine {
	m.cfgMu.RLock()
	defer m.cfgMu.RUnlock()
	return m.buildEngineLocked()
}

func (m *Module) buildEngineLocked() usenetEngine {
	switch strings.ToLower(m.engine) {
	case "live", "nntp":
		return newNNTPEngine(m.nntpCfg)
	default:
		return fixtureEngine{}
	}
}

func (m *Module) SetPublisher(p EventPublisher) {
	m.pubMu.Lock()
	m.publish = p
	m.pubMu.Unlock()
}

func (m *Module) Info() contracts.ModuleInfo {
	return contracts.ModuleInfo{
		ID:           m.id,
		Name:         "Native Usenet Downloader",
		Version:      modulesdk.ManifestVersion(manifest.ManifestJSON),
		Roles:        []string{"downloader", "usenet"},
		Description:  "Native NZB/usenet engine with NNTP download and yEnc decode",
		Capabilities: []string{"downloader", "downloader.usenet", "downloader.native.usenet", "usenet", "settings"},
		HTTPAddr:     m.grpcAddr,
	}
}

func (m *Module) Init(ctx context.Context) error {
	if strings.EqualFold(m.engine, "live") || strings.EqualFold(m.engine, "nntp") {
		m.cfgMu.RLock()
		configured := m.nntpCfg.configured()
		m.cfgMu.RUnlock()
		if !configured {
			return fmt.Errorf("USENET_ENGINE=live requires NNTP_HOST")
		}
	}
	if err := os.MkdirAll(m.destDir, 0o755); err != nil {
		return fmt.Errorf("mkdir download dir: %w", err)
	}
	return nil
}

func (m *Module) Start(ctx context.Context) error {
	m.jobs.restoreSession()
	lis, err := net.Listen("tcp", m.grpcAddr)
	if err != nil {
		return fmt.Errorf("listen gRPC %s: %w", m.grpcAddr, err)
	}
	m.lis = lis
	m.grpcSrv = grpc.NewServer()
	usenetv1.RegisterUsenetDownloaderServiceServer(m.grpcSrv, &usenetServer{m: m})
	modulesdk.RegisterSettings(m.grpcSrv, m.id, m)

	m.cfgMu.RLock()
	engine := m.engine
	m.cfgMu.RUnlock()
	go func() {
		slog.Info("native usenet gRPC listening", "addr", m.grpcAddr, "engine", engine)
		if err := m.grpcSrv.Serve(lis); err != nil {
			slog.Error("gRPC serve", "error", err)
		}
	}()

	mux := http.NewServeMux()
	mux.HandleFunc("/healthz", func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte("ok"))
	})
	m.httpSrv = &http.Server{Addr: m.httpAddr, Handler: mux}
	go func() {
		slog.Info("health listening", "addr", m.httpAddr)
		if err := m.httpSrv.ListenAndServe(); err != nil && err != http.ErrServerClosed {
			slog.Error("health serve", "error", err)
		}
	}()
	go m.dialCore(context.Background())
	return nil
}

func (m *Module) GRPCListenAddr() string {
	if m.lis != nil {
		return m.lis.Addr().String()
	}
	return m.grpcAddr
}

func (m *Module) Stop(ctx context.Context) error {
	if m.grpcSrv != nil {
		m.grpcSrv.GracefulStop()
	}
	if m.httpSrv != nil {
		_ = m.httpSrv.Shutdown(ctx)
	}
	if m.mc != nil {
		_ = m.mc.Close()
	}
	return nil
}

func (m *Module) dialCore(ctx context.Context) {
	meshAddr := os.Getenv("MUXCORE_GRPC_ADDR")
	if meshAddr == "" {
		return
	}
	insecureMode := os.Getenv("MUXCORE_INSECURE_DISABLE_TLS") == "true" || os.Getenv("MUXCORE_GRPC_INSECURE") == "true"
	var opts []client.Option
	if insecureMode {
		opts = append(opts, client.WithInsecure())
	}
	c, err := client.Dial(meshAddr, opts...)
	if err != nil {
		slog.Warn("native-usenet: dial core failed", "error", err)
		return
	}
	m.mc = c
	m.SetPublisher(func(ctx context.Context, eventType string, payload []byte) error {
		return c.Events.Publish(ctx, eventType, m.id, payload)
	})
	slog.Info("native-usenet: connected to core mesh", "addr", meshAddr)
}

func (m *Module) Health(ctx context.Context) error {
	if strings.EqualFold(m.engine, "live") || strings.EqualFold(m.engine, "nntp") {
		m.cfgMu.RLock()
		cfg := m.nntpCfg
		m.cfgMu.RUnlock()
		if !cfg.configured() {
			return fmt.Errorf("live engine unconfigured")
		}
		eng := newNNTPEngine(cfg)
		conn, err := eng.dial(ctx)
		if err != nil {
			return err
		}
		_ = conn.Close()
	}
	return nil
}

func (m *Module) publishDownload(eventType, id, name, savePath, errStr string) {
	m.pubMu.RLock()
	pub := m.publish
	m.pubMu.RUnlock()
	if pub == nil {
		return
	}
	payload, err := json.Marshal(contracts.DownloadEventPayload{
		ID: id, Name: name, SavePath: savePath, Label: "usenet", Error: errStr,
		Files: filesFromStorage(savePath),
	})
	if err != nil {
		return
	}
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	if err := pub(ctx, eventType, payload); err != nil {
		slog.Warn("native-usenet: publish event failed", "type", eventType, "error", err)
	}
}

func (m *Module) watchJob(j *job) {
	go func() {
		ticker := time.NewTicker(20 * time.Millisecond)
		defer ticker.Stop()
		started := false
		for range ticker.C {
			st, name, storage, errMsg := j.snapshot()
			switch st {
			case jobStatusDownloading, jobStatusVerifying, jobStatusRepairing, jobStatusExtracting:
				if !started {
					started = true
					_, n, _, _ := j.snapshot()
					m.publishDownload(contracts.EventDownloadStarted, j.id, n, "", "")
				}
			case jobStatusCompleted:
				if !started {
					_, n, _, _ := j.snapshot()
					m.publishDownload(contracts.EventDownloadStarted, j.id, n, "", "")
				}
				m.publishDownload(contracts.EventDownloadCompleted, j.id, name, storage, "")
				return
			case jobStatusFailed:
				m.publishDownload(contracts.EventDownloadFailed, j.id, name, storage, errMsg)
				return
			}
		}
	}()
}

type usenetServer struct {
	usenetv1.UnimplementedUsenetDownloaderServiceServer
	m *Module
}

func (s *usenetServer) AddNZB(ctx context.Context, req *usenetv1.AddNZBRequest) (*usenetv1.AddNZBResponse, error) {
	j, err := s.m.jobs.add(ctx, req.GetNzbUrl(), req.GetName(), req.GetCategory(), req.GetPaused())
	if err != nil {
		return nil, err
	}
	if !req.GetPaused() {
		s.m.watchJob(j)
	}
	name := req.GetName()
	if name == "" {
		name = req.GetNzbUrl()
	}
	return &usenetv1.AddNZBResponse{JobId: j.id, Name: name}, nil
}

func (s *usenetServer) ListQueue(_ context.Context, req *usenetv1.ListQueueRequest) (*usenetv1.ListQueueResponse, error) {
	items := s.m.jobs.queueSnapshot(req.GetCategory())
	out := make([]*usenetv1.QueueItem, 0, len(items))
	for _, j := range items {
		id, name, category, status, progress, total, remaining := j.queueView()
		out = append(out, &usenetv1.QueueItem{
			Id: id, Name: name, Status: status, Category: category,
			Progress: progress, Size: total, Remaining: remaining,
		})
	}
	return &usenetv1.ListQueueResponse{Items: out}, nil
}

func (s *usenetServer) Pause(_ context.Context, req *usenetv1.PauseRequest) (*usenetv1.PauseResponse, error) {
	if err := s.m.jobs.pause(req.GetId()); err != nil && req.GetId() != "" {
		return nil, err
	}
	return &usenetv1.PauseResponse{Success: true}, nil
}

func (s *usenetServer) Resume(ctx context.Context, req *usenetv1.ResumeRequest) (*usenetv1.ResumeResponse, error) {
	fetch := func(ctx context.Context, url string) ([]byte, error) {
		return fetchNZB(ctx, s.m.jobs.httpClient(), url, s.m.jobs.allowPrivateURLs)
	}
	if err := s.m.jobs.resume(req.GetId(), fetch); err != nil && req.GetId() != "" {
		return nil, err
	}
	if id := req.GetId(); id != "" {
		if j := s.m.jobs.queueSnapshot(""); len(j) > 0 {
			for _, jj := range j {
				if jj.id == id {
					s.m.watchJob(jj)
					break
				}
			}
		}
	}
	return &usenetv1.ResumeResponse{Success: true}, nil
}

func (s *usenetServer) Delete(_ context.Context, req *usenetv1.DeleteRequest) (*usenetv1.DeleteResponse, error) {
	if err := s.m.jobs.delete(req.GetId(), req.GetDeleteFiles()); err != nil {
		return nil, err
	}
	return &usenetv1.DeleteResponse{Success: true}, nil
}

func (s *usenetServer) GetHistory(_ context.Context, req *usenetv1.GetHistoryRequest) (*usenetv1.GetHistoryResponse, error) {
	items := s.m.jobs.historySnapshot(int(req.GetLimit()))
	out := make([]*usenetv1.HistoryItem, 0, len(items))
	for _, j := range items {
		id, name, category, status, storage := j.historyView()
		out = append(out, &usenetv1.HistoryItem{
			Id: id, Name: name, Status: status, Category: category, Storage: storage,
		})
	}
	return &usenetv1.GetHistoryResponse{Items: out}, nil
}

func (s *usenetServer) GetCapabilities(context.Context, *usenetv1.GetCapabilitiesRequest) (*usenetv1.GetCapabilitiesResponse, error) {
	return &usenetv1.GetCapabilitiesResponse{
		SupportsCategories: true,
		SupportsPausing:    true,
		SupportedProtocols: []string{"nzb", "usenet"},
		Backend:            "native-usenet",
	}, nil
}

// CapabilitiesBackend exposes backend id for tests.
func (m *Module) CapabilitiesBackend() string {
	return "native-usenet"
}

// NZBHealthCheck verifies NZB segment availability without downloading payloads.
func (m *Module) NZBHealthCheck(ctx context.Context, nzbData []byte) (*NZBHealthReport, error) {
	return m.buildEngine().NZBHealthCheck(ctx, nzbData)
}

// OfflineDispatch queues an NZB and waits until completion. Used by tests and offline automation.
func (m *Module) OfflineDispatch(ctx context.Context, nzbURL, name, category string) (jobID string, err error) {
	j, err := m.jobs.add(ctx, nzbURL, name, category, false)
	if err != nil {
		return "", err
	}
	m.watchJob(j)
	ticker := time.NewTicker(50 * time.Millisecond)
	defer ticker.Stop()
	for {
		if h := m.jobs.findHistory(j.id); h != nil {
			st, _, _, errMsg := h.snapshot()
			if st == jobStatusCompleted {
				return j.id, nil
			}
			if st == jobStatusFailed {
				return j.id, fmt.Errorf("usenet job %s failed: %s", j.id, errMsg)
			}
		}
		select {
		case <-ctx.Done():
			return j.id, ctx.Err()
		case <-ticker.C:
		}
	}
}

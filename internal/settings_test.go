package internal

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/Muxcore-Media/downloader-native-usenet/internal/postproc"
)

func TestUpdateSettingEngineSwapsJobEngine(t *testing.T) {
	dir := t.TempDir()
	m := NewModule(Config{Engine: "fixture", DestDir: dir})

	if err := m.UpdateSetting("engine", "live"); err == nil {
		t.Fatal("expected live without host to fail")
	}
	if err := m.UpdateSetting("nntp_host", "news.example.com"); err != nil {
		t.Fatal(err)
	}
	if err := m.UpdateSetting("engine", "live"); err != nil {
		t.Fatal(err)
	}
	if m.engine != "live" {
		t.Fatalf("engine=%q", m.engine)
	}
	if _, ok := m.jobs.engine.(*nntpEngine); !ok {
		t.Fatalf("jobs.engine type %T", m.jobs.engine)
	}
	if err := m.UpdateSetting("engine", "fixture"); err != nil {
		t.Fatal(err)
	}
	if _, ok := m.jobs.engine.(fixtureEngine); !ok {
		t.Fatalf("jobs.engine type %T", m.jobs.engine)
	}
}

func TestSettingsPersistAndReload(t *testing.T) {
	dir := t.TempDir()
	m1 := NewModule(Config{Engine: "fixture", DestDir: dir})
	if err := m1.UpdateSetting("nntp_host", "persist.example.com"); err != nil {
		t.Fatal(err)
	}
	if err := m1.UpdateSetting("nntp_pass", "secret123"); err != nil {
		t.Fatal(err)
	}

	raw, err := os.ReadFile(filepath.Join(dir, settingsFile))
	if err != nil {
		t.Fatal(err)
	}
	if string(raw) == "" || !strings.Contains(string(raw), "persist.example.com") || !strings.Contains(string(raw), "secret123") {
		t.Fatalf("settings.json: %s", raw)
	}

	m2 := NewModule(Config{DestDir: dir})
	m2.cfgMu.RLock()
	host := m2.nntpCfg.Host
	pass := m2.nntpCfg.Pass
	m2.cfgMu.RUnlock()
	if host != "persist.example.com" || pass != "secret123" {
		t.Fatalf("reload host=%q pass=%q", host, pass)
	}

	defs := m2.Settings()
	var masked string
	for _, d := range defs {
		if d.Key == "nntp_pass" {
			masked = d.Value
		}
	}
	if masked != "********" {
		t.Fatalf("masked pass=%q", masked)
	}
}

func TestUpdateSettingPostprocConfig(t *testing.T) {
	m := NewModule(Config{Engine: "fixture", DestDir: t.TempDir()})
	if err := m.UpdateSetting("par2_mode", "skip"); err != nil {
		t.Fatal(err)
	}
	if m.jobs.postprocCfg.PAR2 != postproc.ModeSkip {
		t.Fatalf("par2=%q", m.jobs.postprocCfg.PAR2)
	}
}

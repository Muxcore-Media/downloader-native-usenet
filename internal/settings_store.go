package internal

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"strings"

	"github.com/Muxcore-Media/downloader-native-usenet/internal/postproc"
)

const settingsFile = "settings.json"

type persistedSettings struct {
	DownloadDir string `json:"download_dir,omitempty"`
	Engine      string `json:"engine,omitempty"`
	PAR2Mode    string `json:"par2_mode,omitempty"`
	UnpackMode  string `json:"unpack_mode,omitempty"`
	Cleanup     string `json:"cleanup,omitempty"`
	NNTPHost    string `json:"nntp_host,omitempty"`
	NNTPPort    string `json:"nntp_port,omitempty"`
	NNTPUser    string `json:"nntp_user,omitempty"`
	NNTPPass    string `json:"nntp_pass,omitempty"`
	NNTPSSL     string `json:"nntp_ssl,omitempty"`
}

func (m *Module) loadPersistedSettings() {
	path := filepath.Join(m.destDir, settingsFile)
	data, err := os.ReadFile(path)
	if err != nil {
		return
	}
	var ps persistedSettings
	if err := json.Unmarshal(data, &ps); err != nil {
		return
	}
	m.cfgMu.Lock()
	defer m.cfgMu.Unlock()
	if ps.DownloadDir != "" {
		m.destDir = ps.DownloadDir
	}
	if ps.Engine != "" {
		m.engine = ps.Engine
	}
	if ps.PAR2Mode != "" {
		m.postprocCfg.PAR2 = postproc.Mode(ps.PAR2Mode)
	}
	if ps.UnpackMode != "" {
		m.postprocCfg.Unpack = postproc.Mode(ps.UnpackMode)
	}
	if ps.Cleanup != "" {
		m.postprocCfg.Cleanup = strings.EqualFold(ps.Cleanup, "true") || ps.Cleanup == "1"
	}
	if ps.NNTPHost != "" {
		m.nntpCfg.Host = ps.NNTPHost
	}
	if ps.NNTPPort != "" {
		if n, err := strconv.Atoi(ps.NNTPPort); err == nil && n > 0 {
			m.nntpCfg.Port = n
		}
	}
	if ps.NNTPUser != "" {
		m.nntpCfg.User = ps.NNTPUser
	}
	if ps.NNTPPass != "" {
		m.nntpCfg.Pass = ps.NNTPPass
	}
	if ps.NNTPSSL != "" {
		m.nntpCfg.UseTLS = strings.EqualFold(ps.NNTPSSL, "true") || ps.NNTPSSL == "1"
	}
}

func (m *Module) savePersistedSettings() error {
	m.cfgMu.RLock()
	ps := persistedSettings{
		DownloadDir: m.destDir,
		Engine:      m.engine,
		PAR2Mode:    string(m.postprocCfg.PAR2),
		UnpackMode:  string(m.postprocCfg.Unpack),
		Cleanup:     fmt.Sprintf("%v", m.postprocCfg.Cleanup),
		NNTPHost:    m.nntpCfg.Host,
		NNTPUser:    m.nntpCfg.User,
		NNTPPass:    m.nntpCfg.Pass,
		NNTPSSL:     fmt.Sprintf("%v", m.nntpCfg.UseTLS),
	}
	if m.nntpCfg.Port > 0 {
		ps.NNTPPort = fmt.Sprintf("%d", m.nntpCfg.Port)
	}
	m.cfgMu.RUnlock()

	if err := os.MkdirAll(m.destDir, 0o755); err != nil {
		return err
	}
	path := filepath.Join(m.destDir, settingsFile)
	tmp := path + ".tmp"
	data, err := json.MarshalIndent(ps, "", "  ")
	if err != nil {
		return err
	}
	if err := os.WriteFile(tmp, data, 0o600); err != nil {
		return err
	}
	if err := os.Rename(tmp, path); err != nil {
		_ = os.Remove(tmp)
		return err
	}
	return nil
}

func defaultPostprocConfig() postproc.Config {
	return postproc.Config{
		PAR2:   postproc.ModeAuto,
		Unpack: postproc.ModeAuto,
	}
}

func defaultNNTPConfigFromEnv() nntpConfig {
	return nntpConfigFromEnv()
}

package internal

import (
	"fmt"
	"strconv"
	"strings"

	"github.com/Muxcore-Media/core/pkg/contracts"
	"github.com/Muxcore-Media/downloader-native-usenet/internal/postproc"
)

func (m *Module) Settings() []contracts.SettingDef {
	m.cfgMu.RLock()
	cfg := m.nntpCfg
	pp := m.postprocCfg
	destDir := m.destDir
	engine := m.engine
	m.cfgMu.RUnlock()

	pass := cfg.Pass
	if pass != "" {
		pass = "********"
	}
	port := cfg.Port
	if port == 0 {
		if cfg.UseTLS {
			port = 563
		} else {
			port = 119
		}
	}
	return []contracts.SettingDef{
		{
			Key: "download_dir", Label: "Download directory", Type: contracts.SettingTypeString,
			Value: destDir, Description: "Root directory for completed usenet downloads", Group: "Paths",
		},
		{
			Key: "engine", Label: "Engine", Type: contracts.SettingTypeString,
			Value: engine, Description: "fixture (offline smoke) or live (NNTP)", Group: "Engine",
		},
		{
			Key: "par2_mode", Label: "PAR2 mode", Type: contracts.SettingTypeString,
			Value: string(pp.PAR2), Description: "auto, skip, or require", Group: "Post-processing",
		},
		{
			Key: "unpack_mode", Label: "Unpack mode", Type: contracts.SettingTypeString,
			Value: string(pp.Unpack), Description: "auto, skip, or require", Group: "Post-processing",
		},
		{
			Key: "cleanup", Label: "Delete archives after unpack", Type: contracts.SettingTypeBool,
			Value: fmt.Sprintf("%v", pp.Cleanup), Group: "Post-processing",
		},
		{
			Key: "nntp_host", Label: "NNTP host", Type: contracts.SettingTypeString,
			Value: cfg.Host, Description: "Usenet provider hostname (required for live engine)", Group: "NNTP",
		},
		{
			Key: "nntp_port", Label: "NNTP port", Type: contracts.SettingTypeString,
			Value: fmt.Sprintf("%d", port), Description: "Default 563 (TLS) or 119", Group: "NNTP",
		},
		{
			Key: "nntp_user", Label: "NNTP username", Type: contracts.SettingTypeString,
			Value: cfg.User, Group: "NNTP",
		},
		{
			Key: "nntp_pass", Label: "NNTP password", Type: contracts.SettingTypeSecret,
			Value: pass, Group: "NNTP",
		},
		{
			Key: "nntp_ssl", Label: "Use TLS", Type: contracts.SettingTypeBool,
			Value: fmt.Sprintf("%v", cfg.UseTLS), Group: "NNTP",
		},
	}
}

func (m *Module) UpdateSetting(key, value string) error {
	m.cfgMu.Lock()
	switch key {
	case "download_dir":
		if value == "" {
			m.cfgMu.Unlock()
			return fmt.Errorf("download_dir required")
		}
		m.destDir = value
		m.jobs.destDir = value
	case "engine":
		if value != "fixture" && value != "live" {
			m.cfgMu.Unlock()
			return fmt.Errorf("engine must be fixture or live")
		}
		if value == "live" && !m.nntpCfg.configured() {
			m.cfgMu.Unlock()
			return fmt.Errorf("live engine requires nntp_host")
		}
		m.engine = value
		m.jobs.setEngine(m.buildEngineLocked())
	case "par2_mode":
		m.postprocCfg.PAR2 = postproc.Mode(value)
		m.jobs.postprocCfg = m.postprocCfg
	case "unpack_mode":
		m.postprocCfg.Unpack = postproc.Mode(value)
		m.jobs.postprocCfg = m.postprocCfg
	case "cleanup":
		m.postprocCfg.Cleanup = strings.EqualFold(value, "true") || value == "1"
		m.jobs.postprocCfg = m.postprocCfg
	case "nntp_host":
		m.nntpCfg.Host = strings.TrimSpace(value)
	case "nntp_port":
		if value == "" {
			m.nntpCfg.Port = 0
		} else if n, err := strconv.Atoi(value); err != nil || n <= 0 {
			m.cfgMu.Unlock()
			return fmt.Errorf("invalid nntp_port")
		} else {
			m.nntpCfg.Port = n
		}
	case "nntp_user":
		m.nntpCfg.User = value
	case "nntp_pass":
		if value != "" && value != "********" {
			m.nntpCfg.Pass = value
		}
	case "nntp_ssl":
		m.nntpCfg.UseTLS = strings.EqualFold(value, "true") || value == "1"
	default:
		m.cfgMu.Unlock()
		return fmt.Errorf("unknown setting %q", key)
	}
	m.cfgMu.Unlock()
	return m.savePersistedSettings()
}

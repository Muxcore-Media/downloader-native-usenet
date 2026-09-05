package postproc

import (
	"os"
	"strconv"
	"strings"
)

// Mode controls optional post-processing stages (SABnzbd parity).
type Mode string

const (
	ModeAuto    Mode = "auto"    // run when inputs detected
	ModeSkip    Mode = "skip"    // never run
	ModeRequire Mode = "require" // fail job if stage cannot run
)

// Config from environment / settings.
type Config struct {
	PAR2    Mode
	Unpack  Mode
	Cleanup bool

	UnrarCmd    string
	SevenZipCmd string
}

func ConfigFromEnv() Config {
	return Config{
		PAR2:        parseMode(os.Getenv("USENET_PAR2"), ModeAuto),
		Unpack:      parseMode(os.Getenv("USENET_UNPACK"), ModeAuto),
		Cleanup:     envBool("USENET_CLEANUP"),
		UnrarCmd:    firstNonEmpty(os.Getenv("UNRAR_CMD"), os.Getenv("USENET_UNRAR_CMD")),
		SevenZipCmd: firstNonEmpty(os.Getenv("SEVENZIP_CMD"), os.Getenv("USENET_SEVENZIP_CMD"), "7z", "7zz", "7za"),
	}
}

func parseMode(v string, def Mode) Mode {
	switch strings.ToLower(strings.TrimSpace(v)) {
	case "", "auto":
		if v == "" {
			return def
		}
		return ModeAuto
	case "skip", "off", "0", "false":
		return ModeSkip
	case "require", "on", "1", "true":
		return ModeRequire
	default:
		return def
	}
}

func envBool(key string) bool {
	v := strings.TrimSpace(os.Getenv(key))
	return v == "1" || strings.EqualFold(v, "true")
}

func firstNonEmpty(vals ...string) string {
	for _, v := range vals {
		if strings.TrimSpace(v) != "" {
			return strings.TrimSpace(v)
		}
	}
	return ""
}

func (m Mode) enabled() bool { return m != ModeSkip }

func parseThreads() int {
	if v := strings.TrimSpace(os.Getenv("USENET_PAR2_THREADS")); v != "" {
		if n, err := strconv.Atoi(v); err == nil && n > 0 {
			return n
		}
	}
	return 0
}

func parseMemMB() int64 {
	if v := strings.TrimSpace(os.Getenv("USENET_PAR2_MEM_MB")); v != "" {
		if n, err := strconv.ParseInt(v, 10, 64); err == nil && n > 0 {
			return n
		}
	}
	return 16
}

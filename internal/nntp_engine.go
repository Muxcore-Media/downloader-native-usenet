package internal

import (
	"context"
	"crypto/tls"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"

	"github.com/go-newsgroups/nntp"
	"github.com/go-newsgroups/nzb"
	"github.com/go-newsgroups/yenc"

	"github.com/Muxcore-Media/downloader-native-usenet/internal/postproc"
)

const defaultArticleRetries = 3

type nntpConfig struct {
	Host     string
	Port     int
	User     string
	Pass     string
	UseTLS   bool
	Insecure bool
}

func nntpConfigFromEnv() nntpConfig {
	cfg := nntpConfig{
		Host:   strings.TrimSpace(os.Getenv("NNTP_HOST")),
		User:   os.Getenv("NNTP_USER"),
		Pass:   os.Getenv("NNTP_PASS"),
		UseTLS: strings.EqualFold(os.Getenv("NNTP_SSL"), "true") || os.Getenv("NNTP_SSL") == "1",
	}
	if p := strings.TrimSpace(os.Getenv("NNTP_PORT")); p != "" {
		if n, err := strconv.Atoi(p); err == nil && n > 0 {
			cfg.Port = n
		}
	}
	if cfg.Port == 0 {
		if cfg.UseTLS {
			cfg.Port = 563
		} else {
			cfg.Port = 119
		}
	}
	cfg.Insecure = strings.EqualFold(os.Getenv("NNTP_TLS_INSECURE"), "true")
	return cfg
}

func (c nntpConfig) addr() string {
	return fmt.Sprintf("%s:%d", c.Host, c.Port)
}

func (c nntpConfig) configured() bool {
	return c.Host != ""
}

type nntpEngine struct {
	cfg     nntpConfig
	retries int
}

func newNNTPEngine(cfg nntpConfig) *nntpEngine {
	retries := defaultArticleRetries
	if v := strings.TrimSpace(os.Getenv("NNTP_ARTICLE_RETRIES")); v != "" {
		if n, err := strconv.Atoi(v); err == nil && n >= 0 {
			retries = n
		}
	}
	return &nntpEngine{cfg: cfg, retries: retries}
}

func (e *nntpEngine) dial(ctx context.Context) (*nntp.Conn, error) {
	if !e.cfg.configured() {
		return nil, fmt.Errorf("nntp unconfigured: set NNTP_HOST (+ NNTP_USER/NNTP_PASS for auth)")
	}
	var (
		conn *nntp.Conn
		err  error
	)
	if e.cfg.UseTLS {
		tlsCfg := &tls.Config{MinVersion: tls.VersionTLS12}
		if e.cfg.Insecure {
			tlsCfg.InsecureSkipVerify = true //nolint:gosec // operator opt-in for misconfigured providers
		}
		conn, err = nntp.DialTLS(ctx, e.cfg.addr(), tlsCfg)
	} else {
		conn, err = nntp.Dial(ctx, e.cfg.addr())
	}
	if err != nil {
		return nil, fmt.Errorf("nntp dial %s: %w", e.cfg.addr(), err)
	}
	if e.cfg.User != "" {
		if err := conn.Authenticate(e.cfg.User, e.cfg.Pass); err != nil {
			_ = conn.Close()
			return nil, fmt.Errorf("nntp auth: %w", err)
		}
	}
	return conn, nil
}

func (e *nntpEngine) RunJob(ctx context.Context, j *job, nzbData []byte, destDir string, pp postproc.Config) error {
	doc, err := nzb.Parse(nzbData)
	if err != nil {
		j.markFailed(err.Error())
		return fmt.Errorf("parse nzb: %w", err)
	}
	if len(doc.Files) == 0 {
		j.markFailed("nzb has no files")
		return fmt.Errorf("nzb has no files")
	}

	var total int64
	articlesExpected := 0
	for _, f := range doc.Files {
		articlesExpected += len(f.Segments)
		for _, seg := range f.Segments {
			total += int64(seg.Bytes)
		}
	}
	j.setArticleCounts(articlesExpected, 0)
	j.setProgress(total, 0, 0)
	j.setStatus(jobStatusDownloading)

	conn, err := e.dial(ctx)
	if err != nil {
		j.markFailed(err.Error())
		return err
	}
	defer func() { _ = conn.Close() }()

	_, name, category, _, _, _, _ := j.queueView()
	outRoot := jobDir(destDir, category, name)
	if err := os.MkdirAll(outRoot, 0o755); err != nil {
		j.markFailed(err.Error())
		return err
	}

	var done int64
	articlesDownloaded := 0
	crcFailures := 0
	for i, f := range doc.Files {
		select {
		case <-ctx.Done():
			return ctx.Err()
		default:
		}
		safeName := sanitizeName(f.Subject)
		if safeName == "" {
			safeName = fmt.Sprintf("part-%04d.bin", i+1)
		}
		abs := filepath.Join(outRoot, safeName)
		fileName, written, downloaded, crcFailed, err := downloadFileStreaming(ctx, conn, f, abs, e.retries, j)
		articlesDownloaded += downloaded
		crcFailures += crcFailed
		if err != nil {
			j.markFailed(err.Error())
			return fmt.Errorf("download file %d: %w", i, err)
		}
		if fileName != "" {
			_ = fileName
		}
		done += written
		progress := float64(0)
		if total > 0 {
			progress = float64(done) / float64(total) * 100
		}
		j.setProgress(total, done, progress)
		j.setArticleCounts(articlesExpected, articlesDownloaded)
	}

	if err := ctx.Err(); err != nil {
		return err
	}

	verify := verifyNZBDownload(doc, articlesDownloaded, crcFailures)
	j.setVerificationStatus(verify.Status)
	if verify.Status != "verified" {
		j.markFailed(verify.Status)
		return fmt.Errorf("post-download verification: %s", verify.Status)
	}

	_, err = finalizeDownload(ctx, j, outRoot, pp)
	return err
}

func bracketID(id string) string {
	id = strings.TrimSpace(id)
	if len(id) >= 2 && id[0] == '<' && id[len(id)-1] == '>' {
		return id
	}
	return "<" + id + ">"
}

func downloadFileStreaming(ctx context.Context, af nzb.ArticleFetcher, f nzb.File, destPath string, retries int, j *job) (name string, written int64, articlesDownloaded int, crcFailures int, err error) {
	segs := make([]nzb.Segment, len(f.Segments))
	copy(segs, f.Segments)
	sort.Slice(segs, func(i, j int) bool { return segs[i].Number < segs[j].Number })

	out, err := os.OpenFile(destPath, os.O_CREATE|os.O_WRONLY|os.O_TRUNC, 0o644)
	if err != nil {
		return "", 0, 0, 0, err
	}
	defer func() {
		if cerr := out.Close(); cerr != nil && err == nil {
			err = cerr
		}
	}()

	for _, seg := range segs {
		if err := ctx.Err(); err != nil {
			return "", written, articlesDownloaded, crcFailures, fmt.Errorf("nzb: download segment %d: %w", seg.Number, err)
		}
		var part *yenc.Part
		var lastErr error
		for attempt := 0; attempt <= retries; attempt++ {
			art, ferr := af.Article(bracketID(seg.MessageID))
			if ferr != nil {
				lastErr = ferr
			} else {
				part, lastErr = yenc.Decode([]byte(art.Body))
				if lastErr != nil {
					recordYencCRCFailure(j, seg, lastErr)
					if errors.Is(lastErr, yenc.ErrCRCMismatch) {
						crcFailures++
					}
				}
			}
			if lastErr == nil {
				break
			}
			if j != nil {
				j.addSegmentError(seg.MessageID, seg.Number, lastErr.Error())
			}
			if attempt == retries {
				return "", written, articlesDownloaded, crcFailures, fmt.Errorf("nzb: fetch segment %d after %d retries: %w", seg.Number, retries, lastErr)
			}
		}
		articlesDownloaded++
		if name == "" && part.Name != "" {
			name = part.Name
		}
		if part.Begin > 0 {
			end := part.End
			if end > 0 {
				if err := out.Truncate(end); err != nil {
					return "", written, articlesDownloaded, crcFailures, err
				}
			}
			if _, err := out.WriteAt(part.Data, part.Begin-1); err != nil {
				return "", written, articlesDownloaded, crcFailures, err
			}
			if end > written {
				written = end
			}
		} else {
			n, err := out.Write(part.Data)
			if err != nil {
				return "", written, articlesDownloaded, crcFailures, err
			}
			written += int64(n)
		}
	}
	return name, written, articlesDownloaded, crcFailures, nil
}

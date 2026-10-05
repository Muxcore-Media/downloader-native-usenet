package internal

import (
	"context"
	"errors"
	"fmt"
	"sort"
	"strings"

	"github.com/go-newsgroups/nntp"
	"github.com/go-newsgroups/nzb"
	"github.com/go-newsgroups/yenc"
)

// FileHealthStatus is the per-file NZB pre-check result.
type FileHealthStatus string

const (
	FileHealthOK      FileHealthStatus = "ok"
	FileHealthMissing FileHealthStatus = "missing"
	FileHealthCorrupt FileHealthStatus = "corrupt"
)

// FileHealth is one NZB file's health outcome.
type FileHealth struct {
	Subject   string           `json:"subject"`
	Status    FileHealthStatus `json:"status"`
	MessageID string           `json:"message_id,omitempty"`
	Message   string           `json:"message,omitempty"`
}

// NZBHealthReport summarizes NZB health without downloading payloads.
type NZBHealthReport struct {
	Files []FileHealth `json:"files"`
}

func (e *nntpEngine) NZBHealthCheck(ctx context.Context, nzbData []byte) (*NZBHealthReport, error) {
	doc, err := nzb.Parse(nzbData)
	if err != nil {
		return nil, fmt.Errorf("parse nzb: %w", err)
	}
	conn, err := e.dial(ctx)
	if err != nil {
		return nil, err
	}
	defer func() { _ = conn.Close() }()

	report := &NZBHealthReport{Files: make([]FileHealth, 0, len(doc.Files))}
	for i, f := range doc.Files {
		if err := ctx.Err(); err != nil {
			return nil, err
		}
		report.Files = append(report.Files, e.checkFileHealth(ctx, conn, f, i))
	}
	return report, nil
}

func (e *nntpEngine) checkFileHealth(ctx context.Context, conn *nntp.Conn, f nzb.File, fileIdx int) FileHealth {
	subject := strings.TrimSpace(f.Subject)
	if subject == "" {
		subject = fmt.Sprintf("file-%d", fileIdx+1)
	}
	if len(f.Segments) == 0 {
		return FileHealth{Subject: subject, Status: FileHealthMissing, Message: "no segments"}
	}
	segs := append([]nzb.Segment(nil), f.Segments...)
	sort.Slice(segs, func(i, j int) bool { return segs[i].Number < segs[j].Number })
	first := segs[0]
	msgID := bracketID(first.MessageID)

	if len(f.Groups) == 0 {
		return FileHealth{Subject: subject, Status: FileHealthMissing, MessageID: first.MessageID, Message: "no groups"}
	}
	groupName := strings.TrimSpace(f.Groups[0])
	grp, err := conn.Group(groupName)
	if err != nil {
		return FileHealth{Subject: subject, Status: FileHealthMissing, MessageID: first.MessageID, Message: fmt.Sprintf("group %q: %v", groupName, err)}
	}

	if err := xoverHasMessageID(ctx, conn, grp, msgID); err != nil {
		return FileHealth{Subject: subject, Status: FileHealthMissing, MessageID: first.MessageID, Message: err.Error()}
	}

	art, err := conn.Article(msgID)
	if err != nil {
		return FileHealth{Subject: subject, Status: FileHealthMissing, MessageID: first.MessageID, Message: err.Error()}
	}
	if _, err := yenc.Decode([]byte(art.Body)); err != nil {
		if errors.Is(err, yenc.ErrCRCMismatch) {
			return FileHealth{Subject: subject, Status: FileHealthCorrupt, MessageID: first.MessageID, Message: err.Error()}
		}
		return FileHealth{Subject: subject, Status: FileHealthCorrupt, MessageID: first.MessageID, Message: fmt.Sprintf("yenc decode: %v", err)}
	}
	return FileHealth{Subject: subject, Status: FileHealthOK, MessageID: first.MessageID}
}

func xoverHasMessageID(ctx context.Context, conn *nntp.Conn, grp *nntp.Group, msgID string) error {
	const chunk = 500
	want := strings.TrimSpace(msgID)
	for low := grp.Low; low <= grp.High; low += chunk {
		if err := ctx.Err(); err != nil {
			return err
		}
		high := low + chunk - 1
		if high > grp.High {
			high = grp.High
		}
		rows, err := conn.Over(low, high)
		if err != nil {
			return fmt.Errorf("xover %d-%d: %w", low, high, err)
		}
		for _, row := range rows {
			if strings.EqualFold(strings.TrimSpace(row.MessageID), want) {
				return nil
			}
		}
		if high >= grp.High {
			break
		}
	}
	return fmt.Errorf("message-id %s not found via XOVER in %s", msgID, grp.Name)
}

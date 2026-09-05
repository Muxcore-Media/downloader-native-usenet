package internal

import (
	"context"
	"testing"

	"github.com/go-newsgroups/nzb"
)

func TestVerifyNZBDownload(t *testing.T) {
	doc, err := nzb.Parse(minimalNZBBytes())
	if err != nil {
		t.Fatal(err)
	}
	res := verifyNZBDownload(doc, 1, 0)
	if res.Status != "verified" {
		t.Fatalf("status=%q", res.Status)
	}
	res = verifyNZBDownload(doc, 0, 0)
	if res.Status == "verified" {
		t.Fatalf("expected incomplete status, got %q", res.Status)
	}
	res = verifyNZBDownload(doc, 1, 1)
	if res.Status == "verified" {
		t.Fatalf("expected crc failure status, got %q", res.Status)
	}
}

func TestFixtureNZBHealthCheck(t *testing.T) {
	m := NewModule(Config{Engine: "fixture", DestDir: t.TempDir()})
	report, err := m.NZBHealthCheck(context.Background(), minimalNZBBytes())
	if err != nil {
		t.Fatal(err)
	}
	if len(report.Files) != 1 || report.Files[0].Status != FileHealthOK {
		t.Fatalf("report=%+v", report)
	}
}

func TestJobSegmentErrors(t *testing.T) {
	j := &job{id: "seg-test"}
	j.addSegmentError("<a@b>", 3, "timeout")
	errs := j.segmentErrorsSnapshot()
	if len(errs) != 1 || errs[0].ArticleNum != 3 {
		t.Fatalf("errs=%+v", errs)
	}
	j.setArticleCounts(10, 8)
	j.setVerificationStatus("incomplete")
	exp, got, status := j.verificationSnapshot()
	if exp != 10 || got != 8 || status != "incomplete" {
		t.Fatalf("exp=%d got=%d status=%q", exp, got, status)
	}
}

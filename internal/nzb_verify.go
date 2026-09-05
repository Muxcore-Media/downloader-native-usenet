package internal

import (
	"errors"
	"fmt"

	"github.com/go-newsgroups/nzb"
	"github.com/go-newsgroups/yenc"
)

type downloadVerifyResult struct {
	Expected   int
	Downloaded int
	CRCFailed  int
	Status     string
}

func verifyNZBDownload(doc *nzb.NZB, downloaded int, crcFailures int) downloadVerifyResult {
	expected := 0
	for _, f := range doc.Files {
		expected += len(f.Segments)
	}
	res := downloadVerifyResult{
		Expected:   expected,
		Downloaded: downloaded,
		CRCFailed:  crcFailures,
	}
	switch {
	case downloaded < expected:
		res.Status = fmt.Sprintf("incomplete: downloaded %d of %d articles", downloaded, expected)
	case crcFailures > 0:
		res.Status = fmt.Sprintf("crc_failed: %d segment(s) failed yEnc CRC", crcFailures)
	default:
		res.Status = "verified"
	}
	return res
}

func recordYencCRCFailure(j *job, seg nzb.Segment, err error) {
	if j == nil {
		return
	}
	if errors.Is(err, yenc.ErrCRCMismatch) {
		j.addSegmentError(seg.MessageID, seg.Number, "yenc crc mismatch")
	}
}

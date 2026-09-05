package internal

import (
	"context"
	"fmt"

	"github.com/Muxcore-Media/downloader-native-usenet/internal/postproc"
)

func finalizeDownload(ctx context.Context, j *job, outRoot string, cfg postproc.Config) (string, error) {
	res, err := postproc.Process(ctx, outRoot, cfg, func(st string) {
		switch st {
		case postproc.StatusVerifying:
			j.setStatus(jobStatusVerifying)
		case postproc.StatusRepairing:
			j.setStatus(jobStatusRepairing)
		case postproc.StatusExtracting:
			j.setStatus(jobStatusExtracting)
		}
	})
	if err != nil {
		j.markFailed(err.Error())
		return "", err
	}
	storage := res.Storage
	if storage == "" {
		storage = outRoot
	}
	j.markCompleted(storage)
	return storage, nil
}

func failJob(j *job, err error) error {
	if err == nil {
		return nil
	}
	j.markFailed(err.Error())
	return err
}

func wrapFail(j *job, msg string, err error) error {
	if err == nil {
		return nil
	}
	j.markFailed(fmt.Sprintf("%s: %v", msg, err))
	return fmt.Errorf("%s: %w", msg, err)
}

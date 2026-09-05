package postproc_test

import (
	"archive/zip"
	"context"
	"os"
	"path/filepath"
	"testing"

	"github.com/Muxcore-Media/downloader-native-usenet/internal/postproc"
)

func TestExtractZip(t *testing.T) {
	dir := t.TempDir()
	zipPath := filepath.Join(dir, "release.zip")
	mk := filepath.Join(dir, "movie.mkv")
	if err := os.WriteFile(mk, []byte("fake video payload"), 0o644); err != nil {
		t.Fatal(err)
	}
	w, err := os.Create(zipPath)
	if err != nil {
		t.Fatal(err)
	}
	zw := zip.NewWriter(w)
	f, err := zw.Create("movie.mkv")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := f.Write([]byte("fake video payload")); err != nil {
		t.Fatal(err)
	}
	if err := zw.Close(); err != nil {
		t.Fatal(err)
	}
	if err := w.Close(); err != nil {
		t.Fatal(err)
	}
	_ = os.Remove(mk)

	t.Setenv("USENET_PAR2", "skip")
	t.Setenv("USENET_UNPACK", "auto")

	var statuses []string
	res, err := postproc.Process(context.Background(), dir, postproc.ConfigFromEnv(), func(s string) {
		statuses = append(statuses, s)
	})
	if err != nil {
		t.Fatal(err)
	}
	if !res.Extracted {
		t.Fatal("expected extraction")
	}
	if _, err := os.Stat(res.Storage); err != nil {
		t.Fatalf("extracted file: %v (storage=%s)", err, res.Storage)
	}
	if filepath.Base(res.Storage) != "movie.mkv" {
		t.Fatalf("expected movie.mkv, got %s", res.Storage)
	}
	if len(statuses) == 0 || statuses[0] != postproc.StatusExtracting {
		t.Fatalf("statuses: %v", statuses)
	}
}

func TestFindPar2IndexSkipsVolumes(t *testing.T) {
	dir := t.TempDir()
	for _, name := range []string{
		"release.par2",
		"release.vol01+02.par2",
		"release.vol02+02.par2",
	} {
		if err := os.WriteFile(filepath.Join(dir, name), []byte("PAR2\x00PKT"), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	t.Setenv("USENET_PAR2", "require")
	t.Setenv("USENET_UNPACK", "skip")
	_, err := postproc.Process(context.Background(), dir, postproc.ConfigFromEnv(), nil)
	if err == nil {
		t.Fatal("expected par2 verify error on dummy par2 data")
	}
}

func TestProcessNoPostprocAuto(t *testing.T) {
	dir := t.TempDir()
	mk := filepath.Join(dir, "direct.mkv")
	if err := os.WriteFile(mk, []byte("x"), 0o644); err != nil {
		t.Fatal(err)
	}
	t.Setenv("USENET_PAR2", "auto")
	t.Setenv("USENET_UNPACK", "auto")
	res, err := postproc.Process(context.Background(), dir, postproc.ConfigFromEnv(), nil)
	if err != nil {
		t.Fatal(err)
	}
	if res.Storage != mk {
		t.Fatalf("storage: %s", res.Storage)
	}
}

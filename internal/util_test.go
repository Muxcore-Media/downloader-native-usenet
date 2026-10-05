package internal

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestClassifyNZBURLAllowsPublicHTTPS(t *testing.T) {
	if err := classifyNZBURL("https://example.com/nzb/123", false); err != nil {
		t.Fatal(err)
	}
}

func TestClassifyNZBURLRejectsNonHTTP(t *testing.T) {
	if err := classifyNZBURL("file:///etc/passwd", false); err == nil {
		t.Fatal("expected scheme error")
	}
}

func TestFetchNZBRejectsLoopback(t *testing.T) {
	srv := httptest.NewServer(nil)
	t.Cleanup(srv.Close)
	_, err := fetchNZB(context.Background(), srv.Client(), srv.URL+"/x.nzb", false)
	if err == nil {
		t.Fatal("expected blocked loopback host")
	}
}

func TestFetchNZBRejectsMetadataHost(t *testing.T) {
	_, err := fetchNZB(context.Background(), nil, "http://169.254.169.254/latest/meta-data", false)
	if err == nil {
		t.Fatal("expected metadata host blocked")
	}
}

func TestFetchNZBRejectsMetadataWhenPrivateAllowed(t *testing.T) {
	_, err := fetchNZB(context.Background(), nil, "http://169.254.169.254/latest/meta-data", true)
	if err == nil {
		t.Fatal("expected metadata host blocked even when private URLs are allowed")
	}
}

func TestFetchNZBOkViaHTTPtest(t *testing.T) {
	const body = `<?xml version="1.0"?><nzb xmlns="http://www.newzbin.com/DTD/nzb/nzb-1.1.dtd"></nzb>`
	srv := httptest.NewUnstartedServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		_, _ = w.Write([]byte(body))
	}))
	srv.Start()
	t.Cleanup(srv.Close)

	got, err := fetchNZB(context.Background(), srv.Client(), srv.URL+"/ok.nzb", true)
	if err != nil {
		t.Fatal(err)
	}
	if string(got) != body {
		t.Fatalf("body mismatch")
	}
}

func TestJobDirSanitizesCategory(t *testing.T) {
	dir := jobDir(t.TempDir(), "../evil", "release")
	if strings.Contains(dir, "..") {
		t.Fatalf("category traversal leaked: %s", dir)
	}
}

package internal_test

import (
	"context"
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/tls"
	"crypto/x509"
	"crypto/x509/pkix"
	"encoding/pem"
	"math/big"
	"net"
	"os"
	"path/filepath"
	"testing"
	"time"

	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/credentials"
	"google.golang.org/grpc/credentials/insecure"
	"google.golang.org/grpc/status"
	"google.golang.org/protobuf/types/known/emptypb"

	"github.com/Muxcore-Media/downloader-native-usenet/internal"
)

func TestMain(m *testing.M) {
	// Existing tests use plaintext listeners; the mesh TLS test below re-enables TLS.
	_ = os.Setenv("MUXCORE_INSECURE_DISABLE_TLS", "true")
	os.Exit(m.Run())
}

func writeMeshPKI(t *testing.T) (certFile, keyFile, caFile string) {
	t.Helper()
	dir := t.TempDir()
	caKey, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	caTmpl := &x509.Certificate{
		SerialNumber:          big.NewInt(1),
		Subject:               pkix.Name{CommonName: "test mesh CA"},
		NotBefore:             time.Now().Add(-time.Hour),
		NotAfter:              time.Now().Add(time.Hour),
		IsCA:                  true,
		BasicConstraintsValid: true,
		KeyUsage:              x509.KeyUsageCertSign,
	}
	caDER, err := x509.CreateCertificate(rand.Reader, caTmpl, caTmpl, &caKey.PublicKey, caKey)
	if err != nil {
		t.Fatal(err)
	}
	caCert, err := x509.ParseCertificate(caDER)
	if err != nil {
		t.Fatal(err)
	}
	leafKey, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	leafTmpl := &x509.Certificate{
		SerialNumber: big.NewInt(2),
		Subject:      pkix.Name{CommonName: "module"},
		NotBefore:    time.Now().Add(-time.Hour),
		NotAfter:     time.Now().Add(time.Hour),
		KeyUsage:     x509.KeyUsageDigitalSignature,
		ExtKeyUsage:  []x509.ExtKeyUsage{x509.ExtKeyUsageServerAuth, x509.ExtKeyUsageClientAuth},
		DNSNames:     []string{"localhost"},
		IPAddresses:  []net.IP{net.ParseIP("127.0.0.1")},
	}
	leafDER, err := x509.CreateCertificate(rand.Reader, leafTmpl, caCert, &leafKey.PublicKey, caKey)
	if err != nil {
		t.Fatal(err)
	}
	keyDER, err := x509.MarshalECPrivateKey(leafKey)
	if err != nil {
		t.Fatal(err)
	}
	write := func(name, typ string, der []byte) string {
		p := filepath.Join(dir, name)
		if err := os.WriteFile(p, pem.EncodeToMemory(&pem.Block{Type: typ, Bytes: der}), 0o600); err != nil {
			t.Fatal(err)
		}
		return p
	}
	return write("cert.pem", "CERTIFICATE", leafDER), write("key.pem", "EC PRIVATE KEY", keyDER), write("ca.pem", "CERTIFICATE", caDER)
}

// TestGRPCServesMeshTLS checks the module's real gRPC listener negotiates mesh
// TLS (client certificate accepted) and refuses plaintext clients (NFR-SEC-003).
func TestGRPCServesMeshTLS(t *testing.T) {
	certFile, keyFile, caFile := writeMeshPKI(t)
	t.Setenv("MUXCORE_INSECURE_DISABLE_TLS", "")
	t.Setenv("MUXCORE_TLS_CERT", certFile)
	t.Setenv("MUXCORE_TLS_KEY", keyFile)
	t.Setenv("MUXCORE_TLS_CA", caFile)

	ctx := context.Background()
	m := internal.NewModule(internal.Config{
		Engine:   "fixture",
		DestDir:  t.TempDir(),
		GRPCAddr: "127.0.0.1:0",
		HTTPAddr: "127.0.0.1:0",
	})
	if err := m.Init(ctx); err != nil {
		t.Fatal(err)
	}
	if err := m.Start(ctx); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = m.Stop(ctx) })
	addr := m.GRPCListenAddr()
	_, port, err := net.SplitHostPort(addr)
	if err != nil {
		t.Fatal(err)
	}
	target := net.JoinHostPort("127.0.0.1", port)

	caPEM, err := os.ReadFile(caFile)
	if err != nil {
		t.Fatal(err)
	}
	pool := x509.NewCertPool()
	pool.AppendCertsFromPEM(caPEM)
	pair, err := tls.LoadX509KeyPair(certFile, keyFile)
	if err != nil {
		t.Fatal(err)
	}
	tlsConn, err := grpc.NewClient(target, grpc.WithTransportCredentials(credentials.NewTLS(&tls.Config{
		RootCAs: pool, Certificates: []tls.Certificate{pair}, ServerName: "localhost", MinVersion: tls.VersionTLS12,
	})))
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = tlsConn.Close() }()
	cctx, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()
	// An unknown method is answered Unimplemented only once the TLS handshake succeeded.
	err = tlsConn.Invoke(cctx, "/muxcore.test.Probe/Ping", &emptypb.Empty{}, &emptypb.Empty{})
	if status.Code(err) != codes.Unimplemented {
		t.Fatalf("TLS client: want Unimplemented, got %v", err)
	}

	plain, err := grpc.NewClient(target, grpc.WithTransportCredentials(insecure.NewCredentials()))
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = plain.Close() }()
	pctx, pcancel := context.WithTimeout(ctx, 5*time.Second)
	defer pcancel()
	err = plain.Invoke(pctx, "/muxcore.test.Probe/Ping", &emptypb.Empty{}, &emptypb.Empty{})
	if err == nil || status.Code(err) == codes.Unimplemented {
		t.Fatalf("plaintext client must be rejected, got %v", err)
	}
}

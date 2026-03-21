package pki

import (
	"crypto/rand"
	"crypto/rsa"
	"crypto/x509"
	"crypto/x509/pkix"
	"encoding/pem"
	"os"
	"path/filepath"
	"testing"
	"time"
)

func setupTestPKI(t *testing.T) *Service {
	t.Helper()
	dir := t.TempDir()
	svc := NewService(dir)
	if err := svc.EnsurePKI(); err != nil {
		t.Fatalf("EnsurePKI failed: %v", err)
	}
	return svc
}

func generateTestCSR(t *testing.T) ([]byte, *rsa.PrivateKey) {
	t.Helper()
	priv, err := rsa.GenerateKey(rand.Reader, 2048)
	if err != nil {
		t.Fatalf("failed to generate key: %v", err)
	}
	template := x509.CertificateRequest{
		Subject:            pkix.Name{Organization: []string{"wireops Worker"}},
		SignatureAlgorithm: x509.SHA256WithRSA,
	}
	csrBytes, err := x509.CreateCertificateRequest(rand.Reader, &template, priv)
	if err != nil {
		t.Fatalf("failed to create CSR: %v", err)
	}
	csrPEM := pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE REQUEST", Bytes: csrBytes})
	return csrPEM, priv
}

func TestEnsurePKICreatesAllFiles(t *testing.T) {
	dir := t.TempDir()
	svc := NewService(dir)

	if err := svc.EnsurePKI(); err != nil {
		t.Fatalf("EnsurePKI failed: %v", err)
	}

	for _, name := range []string{"ca.crt", "ca.key", "server.crt", "server.key"} {
		path := filepath.Join(dir, name)
		if !fileExists(path) {
			t.Errorf("expected file %s to exist", name)
		}
	}

	if svc.caCert == nil {
		t.Error("expected caCert to be loaded")
	}
	if svc.caPriv == nil {
		t.Error("expected caPriv to be loaded")
	}
}

func TestEnsurePKIIdempotent(t *testing.T) {
	dir := t.TempDir()
	svc := NewService(dir)

	if err := svc.EnsurePKI(); err != nil {
		t.Fatalf("first EnsurePKI failed: %v", err)
	}

	caCertBefore, _ := os.ReadFile(filepath.Join(dir, "ca.crt"))

	if err := svc.EnsurePKI(); err != nil {
		t.Fatalf("second EnsurePKI failed: %v", err)
	}

	caCertAfter, _ := os.ReadFile(filepath.Join(dir, "ca.crt"))
	if string(caCertBefore) != string(caCertAfter) {
		t.Error("expected CA cert to remain unchanged on second call")
	}
}

func TestSignCSRReturnsValidCert(t *testing.T) {
	svc := setupTestPKI(t)
	csrPEM, _ := generateTestCSR(t)

	result, err := svc.SignCSR(csrPEM, "worker-abc")
	if err != nil {
		t.Fatalf("SignCSR failed: %v", err)
	}

	if result.CertPEM == nil {
		t.Fatal("expected non-nil CertPEM")
	}
	if result.Serial == "" {
		t.Error("expected non-empty serial")
	}
	if result.NotAfter.IsZero() {
		t.Error("expected non-zero NotAfter")
	}

	block, _ := pem.Decode(result.CertPEM)
	if block == nil {
		t.Fatal("failed to decode cert PEM")
	}
	cert, err := x509.ParseCertificate(block.Bytes)
	if err != nil {
		t.Fatalf("failed to parse certificate: %v", err)
	}

	if cert.Subject.CommonName != "worker-abc" {
		t.Errorf("expected CN=worker-abc, got %s", cert.Subject.CommonName)
	}
	if cert.Issuer.CommonName != "wireops Root CA" {
		t.Errorf("expected issuer=wireops Root CA, got %s", cert.Issuer.CommonName)
	}
	if len(cert.ExtKeyUsage) == 0 || cert.ExtKeyUsage[0] != x509.ExtKeyUsageClientAuth {
		t.Error("expected ClientAuth ext key usage")
	}
}

func TestSignCSRUniqueSerialsPerCall(t *testing.T) {
	svc := setupTestPKI(t)
	csrPEM, _ := generateTestCSR(t)

	r1, err := svc.SignCSR(csrPEM, "w1")
	if err != nil {
		t.Fatalf("first SignCSR failed: %v", err)
	}
	r2, err := svc.SignCSR(csrPEM, "w1")
	if err != nil {
		t.Fatalf("second SignCSR failed: %v", err)
	}

	if r1.Serial == r2.Serial {
		t.Error("expected different serial numbers for successive signings")
	}
}

func TestSignCSRInvalidCSR(t *testing.T) {
	svc := setupTestPKI(t)

	_, err := svc.SignCSR([]byte("not a CSR"), "w1")
	if err == nil {
		t.Error("expected error for invalid CSR")
	}
}

func TestRenewServerCert(t *testing.T) {
	svc := setupTestPKI(t)

	certBefore, _ := os.ReadFile(filepath.Join(svc.pkiDir, "server.crt"))

	if err := svc.RenewServerCert(); err != nil {
		t.Fatalf("RenewServerCert failed: %v", err)
	}

	certAfter, _ := os.ReadFile(filepath.Join(svc.pkiDir, "server.crt"))

	if string(certBefore) == string(certAfter) {
		t.Error("expected server cert to change after renewal")
	}

	cert, err := svc.loadServerCert()
	if err != nil {
		t.Fatalf("failed to load renewed server cert: %v", err)
	}

	if cert.Subject.CommonName != "wireops Server" {
		t.Errorf("expected CN=wireops Server, got %s", cert.Subject.CommonName)
	}
	if cert.Issuer.CommonName != "wireops Root CA" {
		t.Errorf("expected issuer=wireops Root CA, got %s", cert.Issuer.CommonName)
	}

	hasDNS := false
	for _, dns := range cert.DNSNames {
		if dns == "server" {
			hasDNS = true
			break
		}
	}
	if !hasDNS {
		t.Errorf("expected SAN 'server' in renewed cert, got %v", cert.DNSNames)
	}
}

func TestGetServerCertNotAfter(t *testing.T) {
	svc := setupTestPKI(t)

	notAfter, err := svc.GetServerCertNotAfter()
	if err != nil {
		t.Fatalf("GetServerCertNotAfter failed: %v", err)
	}

	expectedRange := time.Now().AddDate(1, 0, 0)
	diff := notAfter.Sub(expectedRange)
	if diff < -time.Minute || diff > time.Minute {
		t.Errorf("expected NotAfter ~1 year from now, got %v (diff %v)", notAfter, diff)
	}
}

func TestGetCACertNotAfter(t *testing.T) {
	svc := setupTestPKI(t)

	notAfter := svc.GetCACertNotAfter()
	if notAfter.IsZero() {
		t.Fatal("expected non-zero CA NotAfter")
	}

	expectedRange := time.Now().AddDate(10, 0, 0)
	diff := notAfter.Sub(expectedRange)
	if diff < -time.Minute || diff > time.Minute {
		t.Errorf("expected CA NotAfter ~10 years from now, got %v (diff %v)", notAfter, diff)
	}
}

func TestServerCertNeedsRenewal(t *testing.T) {
	svc := setupTestPKI(t)

	if svc.ServerCertNeedsRenewal(30) {
		t.Error("fresh cert should not need renewal at 30-day threshold")
	}
	if !svc.ServerCertNeedsRenewal(366) {
		t.Error("cert expiring in ~365 days should need renewal at 366-day threshold")
	}
}

func TestCertStatus(t *testing.T) {
	tests := []struct {
		name     string
		notAfter time.Time
		want     string
	}{
		{"expired", time.Now().Add(-time.Hour), "expired"},
		{"critical_1day", time.Now().Add(24 * time.Hour), "critical"},
		{"warning_15days", time.Now().Add(15 * 24 * time.Hour), "warning"},
		{"ok_60days", time.Now().Add(60 * 24 * time.Hour), "ok"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := CertStatus(tt.notAfter)
			if got != tt.want {
				t.Errorf("CertStatus(%v) = %q, want %q", tt.notAfter, got, tt.want)
			}
		})
	}
}

func TestGetPKIDetails(t *testing.T) {
	svc := setupTestPKI(t)

	details, err := svc.GetPKIDetails()
	if err != nil {
		t.Fatalf("GetPKIDetails failed: %v", err)
	}

	if details.CA.Subject != "wireops Root CA" {
		t.Errorf("expected CA subject=wireops Root CA, got %s", details.CA.Subject)
	}
	if details.Server.Subject != "wireops Server" {
		t.Errorf("expected Server subject=wireops Server, got %s", details.Server.Subject)
	}
	if details.CA.Status != "ok" {
		t.Errorf("expected CA status=ok, got %s", details.CA.Status)
	}
	if details.Server.Status != "ok" {
		t.Errorf("expected Server status=ok, got %s", details.Server.Status)
	}
	if details.CA.Fingerprint == "" {
		t.Error("expected non-empty CA fingerprint")
	}
	if details.Server.Fingerprint == "" {
		t.Error("expected non-empty Server fingerprint")
	}
}

func TestGetCACertPEM(t *testing.T) {
	svc := setupTestPKI(t)

	pemBytes, err := svc.GetCACertPEM()
	if err != nil {
		t.Fatalf("GetCACertPEM failed: %v", err)
	}

	block, _ := pem.Decode(pemBytes)
	if block == nil {
		t.Fatal("failed to decode CA PEM")
	}
	if block.Type != "CERTIFICATE" {
		t.Errorf("expected block type CERTIFICATE, got %s", block.Type)
	}
}

func TestGetServerTLSCert(t *testing.T) {
	svc := setupTestPKI(t)

	cert, err := svc.GetServerTLSCert()
	if err != nil {
		t.Fatalf("GetServerTLSCert failed: %v", err)
	}
	if len(cert.Certificate) == 0 {
		t.Error("expected at least one certificate in the TLS pair")
	}
}

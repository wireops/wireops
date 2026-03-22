package pki

import (
	"crypto/rand"
	"crypto/rsa"
	"crypto/x509"
	"crypto/x509/pkix"
	"encoding/pem"
	"math/big"
	"os"
	"path/filepath"
	"testing"
	"time"
)

// writeTestCert creates a self-signed certificate in pkiDir/worker.crt with the given expiry.
func writeTestCert(t *testing.T, pkiDir string, notAfter time.Time) {
	t.Helper()
	priv, err := rsa.GenerateKey(rand.Reader, 2048)
	if err != nil {
		t.Fatal(err)
	}
	template := x509.Certificate{
		SerialNumber: big.NewInt(1),
		Subject:      pkix.Name{CommonName: "test-worker"},
		NotBefore:    time.Now().Add(-time.Hour),
		NotAfter:     notAfter,
	}
	certBytes, err := x509.CreateCertificate(rand.Reader, &template, &template, &priv.PublicKey, priv)
	if err != nil {
		t.Fatal(err)
	}
	certPEM := pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: certBytes})
	keyBytes, _ := x509.MarshalPKCS8PrivateKey(priv)
	keyPEM := pem.EncodeToMemory(&pem.Block{Type: "PRIVATE KEY", Bytes: keyBytes})

	_ = os.MkdirAll(pkiDir, 0700)
	_ = os.WriteFile(filepath.Join(pkiDir, "worker.crt"), certPEM, 0644)
	_ = os.WriteFile(filepath.Join(pkiDir, "worker.key"), keyPEM, 0600)
	_ = os.WriteFile(filepath.Join(pkiDir, "ca.crt"), certPEM, 0644) // reuse as dummy CA
}

func TestGetCertNotAfter(t *testing.T) {
	dir := t.TempDir()
	expected := time.Now().Add(90 * 24 * time.Hour).Truncate(time.Second)
	writeTestCert(t, dir, expected)

	got, err := GetCertNotAfter(dir)
	if err != nil {
		t.Fatalf("GetCertNotAfter failed: %v", err)
	}

	diff := got.Sub(expected)
	if diff < -time.Second || diff > time.Second {
		t.Errorf("expected NotAfter=%v, got=%v (diff=%v)", expected, got, diff)
	}
}

func TestGetCertNotAfterMissingFile(t *testing.T) {
	dir := t.TempDir()
	_, err := GetCertNotAfter(dir)
	if err == nil {
		t.Error("expected error when cert file is missing")
	}
}

func TestNeedsRenewalTrue(t *testing.T) {
	dir := t.TempDir()
	writeTestCert(t, dir, time.Now().Add(10*24*time.Hour))

	if !NeedsRenewal(dir, 30) {
		t.Error("expected NeedsRenewal=true for cert expiring in 10 days at 30-day threshold")
	}
}

func TestNeedsRenewalFalse(t *testing.T) {
	dir := t.TempDir()
	writeTestCert(t, dir, time.Now().Add(90*24*time.Hour))

	if NeedsRenewal(dir, 30) {
		t.Error("expected NeedsRenewal=false for cert expiring in 90 days at 30-day threshold")
	}
}

func TestNeedsRenewalExpired(t *testing.T) {
	dir := t.TempDir()
	writeTestCert(t, dir, time.Now().Add(-time.Hour))

	if !NeedsRenewal(dir, 30) {
		t.Error("expected NeedsRenewal=true for already-expired cert")
	}
}

func TestNeedsRenewalMissingCert(t *testing.T) {
	dir := t.TempDir()

	if !NeedsRenewal(dir, 30) {
		t.Error("expected NeedsRenewal=true when cert file is missing (fail-closed)")
	}
}

func TestHasValidCerts(t *testing.T) {
	dir := t.TempDir()

	if HasValidCerts(dir) {
		t.Error("expected HasValidCerts=false for empty directory")
	}

	writeTestCert(t, dir, time.Now().Add(365*24*time.Hour))

	if !HasValidCerts(dir) {
		t.Error("expected HasValidCerts=true after writing certs")
	}
}

func TestWriteCertificates(t *testing.T) {
	dir := t.TempDir()

	cert := []byte("-----BEGIN CERTIFICATE-----\ntest-cert\n-----END CERTIFICATE-----\n")
	key := []byte("-----BEGIN PRIVATE KEY-----\ntest-key\n-----END PRIVATE KEY-----\n")
	ca := []byte("-----BEGIN CERTIFICATE-----\ntest-ca\n-----END CERTIFICATE-----\n")

	if err := WriteCertificates(dir, cert, key, ca); err != nil {
		t.Fatalf("WriteCertificates failed: %v", err)
	}

	gotCert, _ := os.ReadFile(filepath.Join(dir, "worker.crt"))
	gotKey, _ := os.ReadFile(filepath.Join(dir, "worker.key"))
	gotCA, _ := os.ReadFile(filepath.Join(dir, "ca.crt"))

	if string(gotCert) != string(cert) {
		t.Error("worker.crt content mismatch")
	}
	if string(gotKey) != string(key) {
		t.Error("worker.key content mismatch")
	}
	if string(gotCA) != string(ca) {
		t.Error("ca.crt content mismatch")
	}

	// Verify no .tmp files remain
	for _, name := range []string{"worker.crt.tmp", "worker.key.tmp", "ca.crt.tmp"} {
		if _, err := os.Stat(filepath.Join(dir, name)); err == nil {
			t.Errorf("expected .tmp file %s to be removed", name)
		}
	}
}

func TestWriteCertificatesCreatesDir(t *testing.T) {
	dir := filepath.Join(t.TempDir(), "nested", "pki")

	cert := []byte("cert-data")
	key := []byte("key-data")
	ca := []byte("ca-data")

	if err := WriteCertificates(dir, cert, key, ca); err != nil {
		t.Fatalf("WriteCertificates failed: %v", err)
	}

	if _, err := os.Stat(filepath.Join(dir, "worker.crt")); err != nil {
		t.Error("expected worker.crt to exist in nested dir")
	}
}

func TestGenerateCSR(t *testing.T) {
	csrPEM, keyPEM, err := GenerateCSR()
	if err != nil {
		t.Fatalf("GenerateCSR failed: %v", err)
	}

	csrBlock, _ := pem.Decode(csrPEM)
	if csrBlock == nil {
		t.Fatal("failed to decode CSR PEM")
	}
	if csrBlock.Type != "CERTIFICATE REQUEST" {
		t.Errorf("expected CSR block type, got %s", csrBlock.Type)
	}

	csr, err := x509.ParseCertificateRequest(csrBlock.Bytes)
	if err != nil {
		t.Fatalf("failed to parse CSR: %v", err)
	}
	if err := csr.CheckSignature(); err != nil {
		t.Errorf("CSR signature check failed: %v", err)
	}

	keyBlock, _ := pem.Decode(keyPEM)
	if keyBlock == nil {
		t.Fatal("failed to decode key PEM")
	}
	if keyBlock.Type != "PRIVATE KEY" {
		t.Errorf("expected PRIVATE KEY block type, got %s", keyBlock.Type)
	}
}

func TestPurgeCredentials(t *testing.T) {
	dir := t.TempDir()
	writeTestCert(t, dir, time.Now().Add(365*24*time.Hour))

	if !HasValidCerts(dir) {
		t.Fatal("expected certs to exist before purge")
	}

	PurgeCredentials(dir)

	if HasValidCerts(dir) {
		t.Error("expected certs to be removed after purge")
	}
}

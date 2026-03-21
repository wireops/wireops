package pki

import (
	"crypto/rand"
	"crypto/rsa"
	"crypto/sha256"
	"crypto/tls"
	"crypto/x509"
	"crypto/x509/pkix"
	"encoding/hex"
	"encoding/pem"
	"errors"
	"fmt"
	"log"
	"math/big"
	"os"
	"path/filepath"
	"sync"
	"time"
)

type Service struct {
	pkiDir string
	caCert *x509.Certificate
	caPriv *rsa.PrivateKey
	mu     sync.Mutex
}

func NewService(pkiDir string) *Service {
	if pkiDir == "" {
		pkiDir = "./pki_data"
	}
	return &Service{
		pkiDir: pkiDir,
	}
}

// EnsurePKI checks if CA and Server certs exist, generating them if they don't.
func (s *Service) EnsurePKI() error {
	err := os.MkdirAll(s.pkiDir, 0700)
	if err != nil {
		return fmt.Errorf("failed to create PKI directory: %w", err)
	}

	caCertPath := filepath.Join(s.pkiDir, "ca.crt")
	caKeyPath := filepath.Join(s.pkiDir, "ca.key")
	serverCertPath := filepath.Join(s.pkiDir, "server.crt")
	serverKeyPath := filepath.Join(s.pkiDir, "server.key")

	if fileExists(caCertPath) && fileExists(caKeyPath) && fileExists(serverCertPath) && fileExists(serverKeyPath) {
		log.Println("[PKI] CA and Server certificates already exist. Skipping generation.")
		return s.loadCA(caCertPath, caKeyPath)
	}

	log.Println("[PKI] Generating new Root CA and Server certificates...")

	if err := s.generateCA(caCertPath, caKeyPath); err != nil {
		return err
	}

	if err := s.generateServerCert(serverCertPath, serverKeyPath); err != nil {
		return err
	}

	return nil
}

func (s *Service) loadCA(certPath, keyPath string) error {
	certPEM, err := os.ReadFile(certPath)
	if err != nil {
		return err
	}
	certBlock, _ := pem.Decode(certPEM)
	if certBlock == nil {
		return errors.New("failed to decode CA cert PEM")
	}
	cert, err := x509.ParseCertificate(certBlock.Bytes)
	if err != nil {
		return err
	}
	s.caCert = cert

	keyPEM, err := os.ReadFile(keyPath)
	if err != nil {
		return err
	}
	keyBlock, _ := pem.Decode(keyPEM)
	if keyBlock == nil {
		return errors.New("failed to decode CA key PEM")
	}
	key, err := x509.ParsePKCS8PrivateKey(keyBlock.Bytes)
	if err != nil {
		// Fallback for older PKCS1
		key, err = x509.ParsePKCS1PrivateKey(keyBlock.Bytes)
		if err != nil {
			return err
		}
	}

	rsaKey, ok := key.(*rsa.PrivateKey)
	if !ok {
		return errors.New("CA private key is not RSA")
	}
	s.caPriv = rsaKey

	return nil
}

func (s *Service) generateCA(certPath, keyPath string) error {
	priv, err := rsa.GenerateKey(rand.Reader, 4096)
	if err != nil {
		return err
	}

	serialNumberLimit := new(big.Int).Lsh(big.NewInt(1), 128)
	serialNumber, err := rand.Int(rand.Reader, serialNumberLimit)
	if err != nil {
		return err
	}

	template := x509.Certificate{
		SerialNumber: serialNumber,
		Subject: pkix.Name{
			Organization: []string{"wireops"},
			CommonName:   "wireops Root CA",
		},
		NotBefore:             time.Now(),
		NotAfter:              time.Now().AddDate(10, 0, 0), // 10 years
		IsCA:                  true,
		ExtKeyUsage:           []x509.ExtKeyUsage{x509.ExtKeyUsageClientAuth, x509.ExtKeyUsageServerAuth},
		KeyUsage:              x509.KeyUsageDigitalSignature | x509.KeyUsageCertSign,
		BasicConstraintsValid: true,
	}

	derBytes, err := x509.CreateCertificate(rand.Reader, &template, &template, &priv.PublicKey, priv)
	if err != nil {
		return err
	}

	if err := writePEM(certPath, "CERTIFICATE", derBytes); err != nil {
		return err
	}

	privBytes, err := x509.MarshalPKCS8PrivateKey(priv)
	if err != nil {
		return err
	}
	if err := writePEM(keyPath, "PRIVATE KEY", privBytes); err != nil {
		return err
	}

	cert, err := x509.ParseCertificate(derBytes)
	if err != nil {
		return err
	}

	s.caCert = cert
	s.caPriv = priv

	return nil
}

func (s *Service) generateServerCert(certPath, keyPath string) error {
	priv, err := rsa.GenerateKey(rand.Reader, 4096)
	if err != nil {
		return err
	}

	serialNumberLimit := new(big.Int).Lsh(big.NewInt(1), 128)
	serialNumber, err := rand.Int(rand.Reader, serialNumberLimit)
	if err != nil {
		return err
	}

	template := x509.Certificate{
		SerialNumber: serialNumber,
		Subject: pkix.Name{
			Organization: []string{"wireops"},
			CommonName:   "wireops Server",
		},
		NotBefore:   time.Now(),
		NotAfter:    time.Now().AddDate(1, 0, 0), // 1 year
		ExtKeyUsage: []x509.ExtKeyUsage{x509.ExtKeyUsageServerAuth},
		KeyUsage:    x509.KeyUsageDigitalSignature,
	}

	template.DNSNames = []string{"localhost", "server"}

	derBytes, err := x509.CreateCertificate(rand.Reader, &template, s.caCert, &priv.PublicKey, s.caPriv)
	if err != nil {
		return err
	}

	if err := writePEM(certPath, "CERTIFICATE", derBytes); err != nil {
		return err
	}

	privBytes, err := x509.MarshalPKCS8PrivateKey(priv)
	if err != nil {
		return err
	}
	return writePEM(keyPath, "PRIVATE KEY", privBytes)
}

// WritePEM helper
func writePEM(path, blockType string, data []byte) error {
	f, err := os.OpenFile(path, os.O_WRONLY|os.O_CREATE|os.O_TRUNC, 0600)
	if err != nil {
		return err
	}
	defer f.Close()
	return pem.Encode(f, &pem.Block{Type: blockType, Bytes: data})
}

func fileExists(filename string) bool {
	info, err := os.Stat(filename)
	if err != nil {
		return false
	}
	return !info.IsDir()
}

// SignedCertResult holds the output of a CSR signing operation.
type SignedCertResult struct {
	CertPEM    []byte
	Serial     string
	NotAfter   time.Time
}

// SignCSR takes a PEM-encoded CSR and returns a signed certificate with metadata.
func (s *Service) SignCSR(csrPEM []byte, workerID string) (*SignedCertResult, error) {
	block, _ := pem.Decode(csrPEM)
	if block == nil {
		return nil, errors.New("failed to parse CSR PEM")
	}

	csr, err := x509.ParseCertificateRequest(block.Bytes)
	if err != nil {
		return nil, err
	}
	if err := csr.CheckSignature(); err != nil {
		return nil, errors.New("invalid CSR signature")
	}

	serialNumberLimit := new(big.Int).Lsh(big.NewInt(1), 128)
	serialNumber, err := rand.Int(rand.Reader, serialNumberLimit)
	if err != nil {
		return nil, err
	}

	notAfter := time.Now().AddDate(1, 0, 0)
	template := x509.Certificate{
		SerialNumber: serialNumber,
		Subject: pkix.Name{
			Organization: []string{"wireops Worker"},
			CommonName:   workerID,
		},
		NotBefore:   time.Now(),
		NotAfter:    notAfter,
		ExtKeyUsage: []x509.ExtKeyUsage{x509.ExtKeyUsageClientAuth},
		KeyUsage:    x509.KeyUsageDigitalSignature,
	}

	certBytes, err := x509.CreateCertificate(rand.Reader, &template, s.caCert, csr.PublicKey, s.caPriv)
	if err != nil {
		return nil, err
	}

	return &SignedCertResult{
		CertPEM:  pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: certBytes}),
		Serial:   serialNumber.Text(16),
		NotAfter: notAfter,
	}, nil
}

// GetCACertPEM returns the PEM-encoded CA public certificate.
func (s *Service) GetCACertPEM() ([]byte, error) {
	if s.caCert == nil {
		return nil, errors.New("CA not initialized")
	}
	return pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: s.caCert.Raw}), nil
}

// GetServerTLSCert returns the loaded server certificate for the TLS server.
func (s *Service) GetServerTLSCert() (tls.Certificate, error) {
	serverCertPath := filepath.Join(s.pkiDir, "server.crt")
	serverKeyPath := filepath.Join(s.pkiDir, "server.key")
	return tls.LoadX509KeyPair(serverCertPath, serverKeyPath)
}

// RenewServerCert generates a new server certificate signed by the current CA.
// It writes to temporary files first, then atomically renames both into place under
// the service mutex so readers never observe a mismatched cert/key pair.
func (s *Service) RenewServerCert() error {
	if s.caCert == nil || s.caPriv == nil {
		return errors.New("CA not initialized")
	}

	tmpCertPath := filepath.Join(s.pkiDir, "server.crt.tmp")
	tmpKeyPath := filepath.Join(s.pkiDir, "server.key.tmp")

	// Clean up temps on any failure path.
	cleanup := func() {
		os.Remove(tmpCertPath)
		os.Remove(tmpKeyPath)
	}

	log.Println("[PKI] Renewing server certificate...")
	if err := s.generateServerCert(tmpCertPath, tmpKeyPath); err != nil {
		cleanup()
		return fmt.Errorf("failed to generate server cert: %w", err)
	}

	s.mu.Lock()
	defer s.mu.Unlock()

	if err := os.Rename(tmpCertPath, filepath.Join(s.pkiDir, "server.crt")); err != nil {
		cleanup()
		return fmt.Errorf("failed to install server cert: %w", err)
	}
	if err := os.Rename(tmpKeyPath, filepath.Join(s.pkiDir, "server.key")); err != nil {
		cleanup()
		return fmt.Errorf("failed to install server key: %w", err)
	}

	log.Println("[PKI] Server certificate renewed successfully.")
	return nil
}

// GetServerCertNotAfter returns the expiry time of the current server certificate.
func (s *Service) GetServerCertNotAfter() (time.Time, error) {
	cert, err := s.loadServerCert()
	if err != nil {
		return time.Time{}, err
	}
	return cert.NotAfter, nil
}

// GetCACertNotAfter returns the expiry time of the Root CA certificate.
func (s *Service) GetCACertNotAfter() time.Time {
	if s.caCert == nil {
		return time.Time{}
	}
	return s.caCert.NotAfter
}

// ServerCertNeedsRenewal reports whether the server certificate expires within
// the given number of days.
func (s *Service) ServerCertNeedsRenewal(thresholdDays int) bool {
	notAfter, err := s.GetServerCertNotAfter()
	if err != nil {
		// Treat missing or corrupt certs as needing renewal so the auto-renew
		// path can repair them.
		return true
	}
	return time.Until(notAfter) < time.Duration(thresholdDays)*24*time.Hour
}

// loadServerCert parses the server certificate from disk.
func (s *Service) loadServerCert() (*x509.Certificate, error) {
	certPath := filepath.Join(s.pkiDir, "server.crt")
	certPEM, err := os.ReadFile(certPath)
	if err != nil {
		return nil, fmt.Errorf("failed to read server cert: %w", err)
	}
	block, _ := pem.Decode(certPEM)
	if block == nil {
		return nil, errors.New("failed to decode server cert PEM")
	}
	return x509.ParseCertificate(block.Bytes)
}

type CertDetails struct {
	Issuer         string    `json:"issuer"`
	Subject        string    `json:"subject"`
	ExpirationDate time.Time `json:"expiration_date"`
	Fingerprint    string    `json:"fingerprint"`
	Status         string    `json:"status"`
}

type PKIDetails struct {
	CA     CertDetails `json:"ca"`
	Server CertDetails `json:"server"`
}

// CertStatus returns "ok", "warning", or "critical" based on days until expiry.
func CertStatus(notAfter time.Time) string {
	remaining := time.Until(notAfter)
	switch {
	case remaining < 0:
		return "expired"
	case remaining < 7*24*time.Hour:
		return "critical"
	case remaining < 30*24*time.Hour:
		return "warning"
	default:
		return "ok"
	}
}

// GetPKIDetails returns public details about the CA and Server certificates.
func (s *Service) GetPKIDetails() (*PKIDetails, error) {
	if s.caCert == nil {
		return nil, errors.New("CA not initialized")
	}

	caHash := sha256.Sum256(s.caCert.Raw)
	caFingerprint := hex.EncodeToString(caHash[:])

	caDetails := CertDetails{
		Issuer:         s.caCert.Issuer.CommonName,
		Subject:        s.caCert.Subject.CommonName,
		ExpirationDate: s.caCert.NotAfter,
		Fingerprint:    caFingerprint,
		Status:         CertStatus(s.caCert.NotAfter),
	}

	serverCert, err := s.loadServerCert()
	if err != nil {
		return nil, fmt.Errorf("failed to load server certificate: %w", err)
	}

	serverHash := sha256.Sum256(serverCert.Raw)
	serverFingerprint := hex.EncodeToString(serverHash[:])

	serverDetails := CertDetails{
		Issuer:         serverCert.Issuer.CommonName,
		Subject:        serverCert.Subject.CommonName,
		ExpirationDate: serverCert.NotAfter,
		Fingerprint:    serverFingerprint,
		Status:         CertStatus(serverCert.NotAfter),
	}

	return &PKIDetails{
		CA:     caDetails,
		Server: serverDetails,
	}, nil
}

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
	"net"
	"os"
	"path/filepath"
	"strings"
	"time"
)

type Service struct {
	pkiDir string
	caCert *x509.Certificate
	caPriv *rsa.PrivateKey
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

	s.caCert = &template
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

	sans := os.Getenv("WIREOPS_SERVER_SANS")
	if sans != "" {
		for _, san := range strings.Split(sans, ",") {
			san = strings.TrimSpace(san)
			if ip := net.ParseIP(san); ip != nil {
				template.IPAddresses = append(template.IPAddresses, ip)
			} else {
				template.DNSNames = append(template.DNSNames, san)
			}
		}
	} else {
		template.DNSNames = []string{"localhost", "server"}
	}

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

// SignCSR takes a PEM-encoded CSR and returns a PEM-encoded Certificate signed by the CA.
func (s *Service) SignCSR(csrPEM []byte, agentID string) ([]byte, error) {
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

	template := x509.Certificate{
		SerialNumber: serialNumber,
		Subject: pkix.Name{
			Organization: []string{"wireops Agent"},
			CommonName:   agentID, // Connect identity to CN
		},
		NotBefore:   time.Now(),
		NotAfter:    time.Now().AddDate(1, 0, 0), // 1 year
		ExtKeyUsage: []x509.ExtKeyUsage{x509.ExtKeyUsageClientAuth},
		KeyUsage:    x509.KeyUsageDigitalSignature,
	}

	certBytes, err := x509.CreateCertificate(rand.Reader, &template, s.caCert, csr.PublicKey, s.caPriv)
	if err != nil {
		return nil, err
	}

	return pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: certBytes}), nil
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

type CertDetails struct {
	Issuer         string    `json:"issuer"`
	Subject        string    `json:"subject"`
	ExpirationDate time.Time `json:"expiration_date"`
	Fingerprint    string    `json:"fingerprint"`
}

type PKIDetails struct {
	CA     CertDetails `json:"ca"`
	Server CertDetails `json:"server"`
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
	}

	serverCertPair, err := s.GetServerTLSCert()
	if err != nil {
		return nil, fmt.Errorf("failed to load server certificate: %w", err)
	}
	if len(serverCertPair.Certificate) == 0 {
		return nil, errors.New("server certificate contains no data")
	}

	serverCert, err := x509.ParseCertificate(serverCertPair.Certificate[0])
	if err != nil {
		return nil, fmt.Errorf("failed to parse server certificate: %w", err)
	}

	serverHash := sha256.Sum256(serverCert.Raw)
	serverFingerprint := hex.EncodeToString(serverHash[:])

	serverDetails := CertDetails{
		Issuer:         serverCert.Issuer.CommonName,
		Subject:        serverCert.Subject.CommonName,
		ExpirationDate: serverCert.NotAfter,
		Fingerprint:    serverFingerprint,
	}

	return &PKIDetails{
		CA:     caDetails,
		Server: serverDetails,
	}, nil
}

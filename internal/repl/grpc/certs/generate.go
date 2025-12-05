// Package certs provides TLS certificate generation for steep-repl.
package certs

import (
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/x509"
	"crypto/x509/pkix"
	"encoding/pem"
	"fmt"
	"math/big"
	"net"
	"os"
	"path/filepath"
	"time"
)

// Config holds certificate generation configuration.
type Config struct {
	// OutputDir is where certificates will be written
	OutputDir string
	// NodeName is used in the certificate CN
	NodeName string
	// Hosts are SANs (hostnames and IPs)
	Hosts []string
	// ValidDays is certificate validity period
	ValidDays int
}

// Result contains paths to generated certificates.
type Result struct {
	CAKey      string
	CACert     string
	ServerKey  string
	ServerCert string
	ClientKey  string
	ClientCert string
}

// Generate creates a CA and server/client certificates for mTLS.
func Generate(cfg Config) (*Result, error) {
	if cfg.ValidDays == 0 {
		cfg.ValidDays = 365
	}
	if cfg.NodeName == "" {
		cfg.NodeName = "steep-repl"
	}
	if len(cfg.Hosts) == 0 {
		cfg.Hosts = []string{"localhost", "127.0.0.1"}
	}

	// Create output directory
	if err := os.MkdirAll(cfg.OutputDir, 0700); err != nil {
		return nil, fmt.Errorf("create output dir: %w", err)
	}

	result := &Result{
		CAKey:      filepath.Join(cfg.OutputDir, "ca.key"),
		CACert:     filepath.Join(cfg.OutputDir, "ca.crt"),
		ServerKey:  filepath.Join(cfg.OutputDir, "server.key"),
		ServerCert: filepath.Join(cfg.OutputDir, "server.crt"),
		ClientKey:  filepath.Join(cfg.OutputDir, "client.key"),
		ClientCert: filepath.Join(cfg.OutputDir, "client.crt"),
	}

	// Generate CA
	caKey, caCert, err := generateCA(cfg)
	if err != nil {
		return nil, fmt.Errorf("generate CA: %w", err)
	}

	if err := writeKey(result.CAKey, caKey); err != nil {
		return nil, err
	}
	if err := writeCert(result.CACert, caCert); err != nil {
		return nil, err
	}

	// Generate server certificate
	serverKey, serverCert, err := generateCert(cfg, caKey, caCert, false)
	if err != nil {
		return nil, fmt.Errorf("generate server cert: %w", err)
	}

	if err := writeKey(result.ServerKey, serverKey); err != nil {
		return nil, err
	}
	if err := writeCert(result.ServerCert, serverCert); err != nil {
		return nil, err
	}

	// Generate client certificate
	clientKey, clientCert, err := generateCert(cfg, caKey, caCert, true)
	if err != nil {
		return nil, fmt.Errorf("generate client cert: %w", err)
	}

	if err := writeKey(result.ClientKey, clientKey); err != nil {
		return nil, err
	}
	if err := writeCert(result.ClientCert, clientCert); err != nil {
		return nil, err
	}

	return result, nil
}

func generateCA(cfg Config) (*ecdsa.PrivateKey, *x509.Certificate, error) {
	key, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		return nil, nil, err
	}

	serialNumber, err := rand.Int(rand.Reader, new(big.Int).Lsh(big.NewInt(1), 128))
	if err != nil {
		return nil, nil, err
	}

	template := &x509.Certificate{
		SerialNumber: serialNumber,
		Subject: pkix.Name{
			Organization: []string{"Steep"},
			CommonName:   "steep-repl-ca",
		},
		NotBefore:             time.Now(),
		NotAfter:              time.Now().AddDate(0, 0, cfg.ValidDays),
		KeyUsage:              x509.KeyUsageCertSign | x509.KeyUsageCRLSign,
		BasicConstraintsValid: true,
		IsCA:                  true,
		MaxPathLen:            1,
	}

	certDER, err := x509.CreateCertificate(rand.Reader, template, template, &key.PublicKey, key)
	if err != nil {
		return nil, nil, err
	}

	cert, err := x509.ParseCertificate(certDER)
	if err != nil {
		return nil, nil, err
	}

	return key, cert, nil
}

func generateCert(cfg Config, caKey *ecdsa.PrivateKey, caCert *x509.Certificate, isClient bool) (*ecdsa.PrivateKey, *x509.Certificate, error) {
	key, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		return nil, nil, err
	}

	serialNumber, err := rand.Int(rand.Reader, new(big.Int).Lsh(big.NewInt(1), 128))
	if err != nil {
		return nil, nil, err
	}

	cn := cfg.NodeName + "-server"
	extKeyUsage := []x509.ExtKeyUsage{x509.ExtKeyUsageServerAuth}
	if isClient {
		cn = cfg.NodeName + "-client"
		extKeyUsage = []x509.ExtKeyUsage{x509.ExtKeyUsageClientAuth}
	}

	template := &x509.Certificate{
		SerialNumber: serialNumber,
		Subject: pkix.Name{
			Organization: []string{"Steep"},
			CommonName:   cn,
		},
		NotBefore:   time.Now(),
		NotAfter:    time.Now().AddDate(0, 0, cfg.ValidDays),
		KeyUsage:    x509.KeyUsageDigitalSignature | x509.KeyUsageKeyEncipherment,
		ExtKeyUsage: extKeyUsage,
	}

	// Add SANs
	for _, h := range cfg.Hosts {
		if ip := net.ParseIP(h); ip != nil {
			template.IPAddresses = append(template.IPAddresses, ip)
		} else {
			template.DNSNames = append(template.DNSNames, h)
		}
	}

	certDER, err := x509.CreateCertificate(rand.Reader, template, caCert, &key.PublicKey, caKey)
	if err != nil {
		return nil, nil, err
	}

	cert, err := x509.ParseCertificate(certDER)
	if err != nil {
		return nil, nil, err
	}

	return key, cert, nil
}

func writeKey(path string, key *ecdsa.PrivateKey) error {
	keyDER, err := x509.MarshalECPrivateKey(key)
	if err != nil {
		return fmt.Errorf("marshal key: %w", err)
	}

	f, err := os.OpenFile(path, os.O_WRONLY|os.O_CREATE|os.O_TRUNC, 0600)
	if err != nil {
		return fmt.Errorf("create %s: %w", path, err)
	}
	defer f.Close()

	return pem.Encode(f, &pem.Block{Type: "EC PRIVATE KEY", Bytes: keyDER})
}

func writeCert(path string, cert *x509.Certificate) error {
	f, err := os.OpenFile(path, os.O_WRONLY|os.O_CREATE|os.O_TRUNC, 0644)
	if err != nil {
		return fmt.Errorf("create %s: %w", path, err)
	}
	defer f.Close()

	return pem.Encode(f, &pem.Block{Type: "CERTIFICATE", Bytes: cert.Raw})
}

// ConfigSnippet returns YAML config to paste into config.yaml.
func ConfigSnippet(result *Result) string {
	return fmt.Sprintf(`grpc:
  port: 5433
  tls:
    cert_file: %s
    key_file: %s
    ca_file: %s
`, result.ServerCert, result.ServerKey, result.CACert)
}

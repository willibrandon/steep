package grpc

import (
	"crypto/tls"
	"crypto/x509"
	"fmt"
	"os"

	"google.golang.org/grpc/credentials"
)

// LoadServerCredentials loads mTLS server credentials from certificate files.
// Returns insecure credentials if no TLS config is provided.
func LoadServerCredentials(certFile, keyFile, caFile string) (credentials.TransportCredentials, error) {
	// If no TLS files provided, return nil for insecure mode
	if certFile == "" || keyFile == "" {
		return nil, nil
	}

	// Load server certificate and key
	serverCert, err := tls.LoadX509KeyPair(certFile, keyFile)
	if err != nil {
		return nil, fmt.Errorf("failed to load server certificate: %w", err)
	}

	// Create TLS config
	config := &tls.Config{
		Certificates: []tls.Certificate{serverCert},
		MinVersion:   tls.VersionTLS13,
	}

	// If CA file provided, enable client certificate verification (mTLS)
	if caFile != "" {
		caCert, err := os.ReadFile(caFile)
		if err != nil {
			return nil, fmt.Errorf("failed to read CA certificate: %w", err)
		}

		caPool := x509.NewCertPool()
		if !caPool.AppendCertsFromPEM(caCert) {
			return nil, fmt.Errorf("failed to parse CA certificate")
		}

		config.ClientCAs = caPool
		config.ClientAuth = tls.RequireAndVerifyClientCert
	}

	return credentials.NewTLS(config), nil
}

// LoadClientCredentials loads mTLS client credentials from certificate files.
// Returns insecure credentials if no TLS config is provided.
func LoadClientCredentials(certFile, keyFile, caFile string) (credentials.TransportCredentials, error) {
	// If no TLS files provided, return nil for insecure mode
	if caFile == "" {
		return nil, nil
	}

	// Load CA certificate for server verification
	caCert, err := os.ReadFile(caFile)
	if err != nil {
		return nil, fmt.Errorf("failed to read CA certificate: %w", err)
	}

	caPool := x509.NewCertPool()
	if !caPool.AppendCertsFromPEM(caCert) {
		return nil, fmt.Errorf("failed to parse CA certificate")
	}

	config := &tls.Config{
		RootCAs:    caPool,
		MinVersion: tls.VersionTLS13,
	}

	// If client cert provided, load it for mTLS
	if certFile != "" && keyFile != "" {
		clientCert, err := tls.LoadX509KeyPair(certFile, keyFile)
		if err != nil {
			return nil, fmt.Errorf("failed to load client certificate: %w", err)
		}
		config.Certificates = []tls.Certificate{clientCert}
	}

	return credentials.NewTLS(config), nil
}

// InsecureCredentials returns insecure transport credentials for development/testing.
func InsecureCredentials() credentials.TransportCredentials {
	return nil // nil means use grpc.WithInsecure() on client
}

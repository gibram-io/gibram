package config

import (
	"crypto/tls"
	"crypto/x509"
	"os"
	"path/filepath"
	"testing"
	"time"
)

func TestGenerateSelfSignedCert(t *testing.T) {
	hosts := []string{"localhost", "127.0.0.1", "gibram.local"}
	validFor := 24 * time.Hour

	certPEM, keyPEM, err := GenerateSelfSignedCert(hosts, validFor)
	if err != nil {
		t.Fatalf("GenerateSelfSignedCert failed: %v", err)
	}

	if len(certPEM) == 0 {
		t.Error("certPEM should not be empty")
	}
	if len(keyPEM) == 0 {
		t.Error("keyPEM should not be empty")
	}

	// Verify we can parse the certificate
	cert, err := tls.X509KeyPair(certPEM, keyPEM)
	if err != nil {
		t.Fatalf("failed to parse generated cert: %v", err)
	}

	// Parse the X509 certificate to check its properties
	x509Cert, err := x509.ParseCertificate(cert.Certificate[0])
	if err != nil {
		t.Fatalf("failed to parse X509 certificate: %v", err)
	}

	// Check validity period
	if time.Now().Before(x509Cert.NotBefore) {
		t.Error("certificate should be valid now")
	}
	if time.Now().Add(validFor).After(x509Cert.NotAfter.Add(time.Hour)) {
		t.Error("certificate should be valid for the requested duration")
	}

	// Check that localhost is included
	foundLocalhost := false
	for _, dns := range x509Cert.DNSNames {
		if dns == "localhost" {
			foundLocalhost = true
			break
		}
	}
	if !foundLocalhost {
		t.Error("certificate should include localhost in DNS names")
	}

	// Check that custom DNS name is included
	foundCustomDNS := false
	for _, dns := range x509Cert.DNSNames {
		if dns == "gibram.local" {
			foundCustomDNS = true
			break
		}
	}
	if !foundCustomDNS {
		t.Error("certificate should include custom DNS name")
	}
}

func TestLoadOrGenerateTLSConfig_WithCertFiles(t *testing.T) {
	tmpDir, err := os.MkdirTemp("", "tls_test")
	if err != nil {
		t.Fatalf("failed to create temp dir: %v", err)
	}
	defer func() {
		if err := os.RemoveAll(tmpDir); err != nil {
			t.Logf("failed to remove temp dir: %v", err)
		}
	}()

	// Generate test certificates
	certPEM, keyPEM, err := GenerateSelfSignedCert([]string{"localhost"}, 24*time.Hour)
	if err != nil {
		t.Fatalf("failed to generate test cert: %v", err)
	}

	certPath := filepath.Join(tmpDir, "cert.pem")
	keyPath := filepath.Join(tmpDir, "key.pem")

	if err := os.WriteFile(certPath, certPEM, 0644); err != nil {
		t.Fatalf("failed to write cert: %v", err)
	}
	if err := os.WriteFile(keyPath, keyPEM, 0600); err != nil {
		t.Fatalf("failed to write key: %v", err)
	}

	cfg := &TLSConfig{
		CertFile: certPath,
		KeyFile:  keyPath,
		AutoCert: false,
	}

	tlsConfig, enabled, err := cfg.LoadOrGenerateTLSConfig(tmpDir)
	if err != nil {
		t.Fatalf("LoadOrGenerateTLSConfig failed: %v", err)
	}
	if !enabled {
		t.Error("TLS should be enabled with cert files")
	}
	if tlsConfig == nil {
		t.Fatal("tlsConfig should not be nil")
	}
	if len(tlsConfig.Certificates) == 0 {
		t.Error("tlsConfig should have certificates")
	}
}

func TestLoadOrGenerateTLSConfig_AutoCert(t *testing.T) {
	tmpDir, err := os.MkdirTemp("", "tls_autocert_test")
	if err != nil {
		t.Fatalf("failed to create temp dir: %v", err)
	}
	defer func() {
		if err := os.RemoveAll(tmpDir); err != nil {
			t.Logf("failed to remove temp dir: %v", err)
		}
	}()

	cfg := &TLSConfig{
		CertFile: "",
		KeyFile:  "",
		AutoCert: true,
	}

	// First call should generate new certificate
	tlsConfig, enabled, err := cfg.LoadOrGenerateTLSConfig(tmpDir)
	if err != nil {
		t.Fatalf("LoadOrGenerateTLSConfig failed: %v", err)
	}
	if !enabled {
		t.Error("TLS should be enabled with AutoCert")
	}
	if tlsConfig == nil {
		t.Fatal("tlsConfig should not be nil")
	}

	// Verify cert files were cached
	certPath := filepath.Join(tmpDir, "auto_cert.pem")
	keyPath := filepath.Join(tmpDir, "auto_key.pem")

	if _, err := os.Stat(certPath); os.IsNotExist(err) {
		t.Error("auto_cert.pem should be created")
	}
	if _, err := os.Stat(keyPath); os.IsNotExist(err) {
		t.Error("auto_key.pem should be created")
	}

	// Second call should use cached certificate
	tlsConfig2, enabled2, err := cfg.LoadOrGenerateTLSConfig(tmpDir)
	if err != nil {
		t.Fatalf("LoadOrGenerateTLSConfig (cached) failed: %v", err)
	}
	if !enabled2 {
		t.Error("TLS should still be enabled")
	}
	if tlsConfig2 == nil {
		t.Error("tlsConfig should not be nil")
	}
}

func TestLoadOrGenerateTLSConfig_Disabled(t *testing.T) {
	tmpDir, err := os.MkdirTemp("", "tls_disabled_test")
	if err != nil {
		t.Fatalf("failed to create temp dir: %v", err)
	}
	defer func() {
		if err := os.RemoveAll(tmpDir); err != nil {
			t.Logf("failed to remove temp dir: %v", err)
		}
	}()

	cfg := &TLSConfig{
		CertFile: "",
		KeyFile:  "",
		AutoCert: false,
	}

	tlsConfig, enabled, err := cfg.LoadOrGenerateTLSConfig(tmpDir)
	if err != nil {
		t.Fatalf("LoadOrGenerateTLSConfig failed: %v", err)
	}
	if enabled {
		t.Error("TLS should be disabled")
	}
	if tlsConfig != nil {
		t.Error("tlsConfig should be nil when TLS is disabled")
	}
}

func TestLoadOrGenerateTLSConfig_InvalidCertFile(t *testing.T) {
	tmpDir, err := os.MkdirTemp("", "tls_invalid_test")
	if err != nil {
		t.Fatalf("failed to create temp dir: %v", err)
	}
	defer func() {
		if err := os.RemoveAll(tmpDir); err != nil {
			t.Logf("failed to remove temp dir: %v", err)
		}
	}()

	cfg := &TLSConfig{
		CertFile: "/nonexistent/cert.pem",
		KeyFile:  "/nonexistent/key.pem",
		AutoCert: false,
	}

	_, _, err = cfg.LoadOrGenerateTLSConfig(tmpDir)
	if err == nil {
		t.Error("should fail with invalid cert files")
	}
}

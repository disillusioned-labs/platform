package grpc

import (
	"crypto/tls"
	"crypto/x509"
	"errors"
	"fmt"
	"os"

	"google.golang.org/grpc/credentials"
)

// NewTLSConfig builds a *tls.Config from PEM file paths, the shape the
// services' GRPCTLSConfig carries. The result still passes through
// HardenTLSConfig when handed to WithTLS, so min-version and
// InsecureSkipVerify rules stay in one place.
//
// An empty caFile keeps the system trust store. certFile and keyFile are
// all-or-nothing: give both to present a client certificate (mTLS client, or
// the server's own certificate), or neither. mutualTLS additionally requires
// caFile - on the server it is the pool client certificates are verified
// against, and a client presenting a certificate is expected to pin the
// server's CA anyway.
func NewTLSConfig(caFile, certFile, keyFile, serverName string, mutualTLS bool) (*tls.Config, error) {
	out := &tls.Config{ServerName: serverName}

	var caPool *x509.CertPool
	if caFile != "" {
		pem, err := os.ReadFile(caFile)
		if err != nil {
			return nil, fmt.Errorf("grpc: read CA file: %w", err)
		}
		caPool = x509.NewCertPool()
		if !caPool.AppendCertsFromPEM(pem) {
			return nil, fmt.Errorf("grpc: CA file %q contains no certificates", caFile)
		}
		out.RootCAs = caPool
	}

	if mutualTLS || certFile != "" || keyFile != "" {
		if certFile == "" || keyFile == "" {
			return nil, errors.New("grpc: a client/server certificate needs both cert_file and key_file")
		}
		cert, err := tls.LoadX509KeyPair(certFile, keyFile)
		if err != nil {
			return nil, fmt.Errorf("grpc: load x509 key pair: %w", err)
		}
		out.Certificates = []tls.Certificate{cert}
	}

	if mutualTLS {
		if caPool == nil {
			return nil, errors.New("grpc: mutual TLS requires ca_file to verify peer certificates")
		}
		out.ClientCAs = caPool
		out.ClientAuth = tls.RequireAndVerifyClientCert
	}

	return out, nil
}

func HardenTLSConfig(config *tls.Config) (*tls.Config, error) {
	if config == nil {
		return nil, errors.New("grpc: TLS config must not be nil")
	}

	out := config.Clone()

	if out.MinVersion == 0 {
		out.MinVersion = tls.VersionTLS12
	}

	if out.MinVersion < tls.VersionTLS12 {
		return nil, errors.New(
			"grpc: TLS versions below 1.2 are not allowed",
		)
	}

	if out.InsecureSkipVerify {
		return nil, errors.New(
			"grpc: InsecureSkipVerify is not allowed",
		)
	}

	return out, nil
}

func ServerTLSCredentials(config *tls.Config) (credentials.TransportCredentials, error) {
	hardened, err := HardenTLSConfig(config)
	if err != nil {
		return nil, err
	}

	if len(hardened.Certificates) == 0 && hardened.GetCertificate == nil {
		return nil, errors.New("grpc: server TLS requires a certificate")
	}

	return credentials.NewTLS(hardened), nil
}

func ClientTLSCredentials(config *tls.Config) (credentials.TransportCredentials, error) {
	hardened, err := HardenTLSConfig(config)
	if err != nil {
		return nil, err
	}

	return credentials.NewTLS(hardened), nil
}

package grpc

import (
	"crypto/tls"
	"errors"

	"google.golang.org/grpc/credentials"
)

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

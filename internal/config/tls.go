package config

import (
	"crypto/tls"
	"crypto/x509"
	"fmt"
	"io/ioutil"
)

// TLSConfig: deines parameters to setup TLSConfig
type TLSConfig struct {
	CertFile      string
	KeyFile       string
	CAFile        string
	ServerAddress string
	Server        bool
}

func SetupTLSConfig(cfg TLSConfig) (*tls.Config, error) {
	var err error
	tlsConfig := &tls.Config{}

	// check if cert file and key file are non-empty and load the x.509 certificate using the cert and private key pair
	// enable TLS server to present the certificate to clients for authentication.
	if cfg.CertFile != "" && cfg.KeyFile != "" {
		tlsConfig.Certificates = make([]tls.Certificate, 1)
		tlsConfig.Certificates[0], err = tls.LoadX509KeyPair(
			cfg.CertFile,
			cfg.KeyFile,
		)

		if err != nil {
			return nil, err
		}
	}

	// check if ca file is non-empty
	if cfg.CAFile != "" {
		b, err := ioutil.ReadFile(cfg.CAFile)
		if err != nil {
			return nil, err
		}

		ca := x509.NewCertPool()
		ok := ca.AppendCertsFromPEM(b)
		if !ok {
			return nil, fmt.Errorf(
				"failed to parse root certificate: %q",
				cfg.CAFile)
		}

		// configure TLS server - if server flag is true
		if cfg.Server {

			// clientCAs defines the set of root certificate authorities that servers use if required to
			// verify a client certificate by the policy in ClientAuth
			tlsConfig.ClientCAs = ca

			// enforce client cert verification
			// RequireAndVerifyClientCert indicates that a client certificate should be requested during the handshake,
			// and that at least one valid certificate is required to be sent by the client.
			tlsConfig.ClientAuth = tls.RequireAndVerifyClientCert
		} else {

			// configure TLS client
			// RootCAs defines the set of root certificate authorities that clients use when verifying server certificates.
			// If RootCAs is nil, TLS uses the host's root CA set.
			tlsConfig.RootCAs = ca
		}

		tlsConfig.ServerName = cfg.ServerAddress
	}

	return tlsConfig, nil
}

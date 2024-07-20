package srvutils

import (
	"crypto/tls"
	"crypto/x509"
	"fmt"
	"net/http"
	"os"
	"strconv"
)

type TLSConfig struct {
	CA         string  `json:"ca" yaml:"ca"`
	Cert       string  `json:"cert" yaml:"cert"`
	Key        string  `json:"key" yaml:"key"`
	ClientAuth *string `json:"clientAuth" yaml:"clientAuth"`
}

func SetupServer(handler http.Handler, port int, tlsConfig *TLSConfig) (*http.Server, error) {
	srv := &http.Server{
		Addr:    ":" + strconv.Itoa(port),
		Handler: handler,
	}

	if tlsConfig != nil && tlsConfig.Cert != "" && tlsConfig.Key != "" {
		cert, err := tls.LoadX509KeyPair(tlsConfig.Cert, tlsConfig.Key)
		if err != nil {
			return nil, fmt.Errorf("server: loadkeys: %s", err)
		}
		config := tls.Config{Certificates: []tls.Certificate{cert}}
		if tlsConfig.CA != "" {
			caCert, err := os.ReadFile(tlsConfig.CA)
			if err != nil {
				return nil, fmt.Errorf("server: read cacert: %s", err)
			}
			caCertPool := x509.NewCertPool()
			caCertPool.AppendCertsFromPEM(caCert)
			config.RootCAs = caCertPool
			config.ClientCAs = caCertPool
			if tlsConfig.ClientAuth != nil {
				switch *tlsConfig.ClientAuth {
				case "require":
					config.ClientAuth = tls.RequireAndVerifyClientCert
				case "request":
					config.ClientAuth = tls.RequestClientCert
				case "verify":
					config.ClientAuth = tls.VerifyClientCertIfGiven
				case "none":
					config.ClientAuth = tls.NoClientCert
				default:
					config.ClientAuth = tls.NoClientCert
				}
			}
		}
		srv.TLSConfig = &config
	}

	return srv, nil
}

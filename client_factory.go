package govirt

import (
	"context"
	"crypto/tls"
	"crypto/x509"
	"fmt"
	"io/ioutil"
	"net/http"
	"strings"
	"time"

	ovirtsdk4 "github.com/ovirt/go-ovirt"
)

// New creates a new copy of the enhanced oVirt client.
func New(
	url string,
	username string,
	password string,
	caFile string,
	caCert []byte,
	insecure bool,
	extraHeaders map[string]string,
	logger Logger,
) (Client, error) {
	if err := validateURL(url); err != nil {
		return nil, fmt.Errorf("invalid URL: %s (%w)", url, err)
	}
	if err := validateUsername(username); err != nil {
		return nil, fmt.Errorf("invalid username: %s (%w)", username, err)
	}
	if caFile == "" && len(caCert) == 0 && !insecure {
		return nil, fmt.Errorf("one of caFile, caCert, or insecure must be provided")
	}

	connBuilder := ovirtsdk4.NewConnectionBuilder().
		URL(url).
		Username(username).
		Password(password).
		CAFile(caFile).
		CACert(caCert).
		Insecure(insecure).
		LogFunc(logger.Logf)
	if len(extraHeaders) > 0 {
		connBuilder.Headers(extraHeaders)
	}

	conn, err := connBuilder.Build()
	if err != nil {
		return nil, fmt.Errorf("failed to create underlying oVirt connection (%w)", err)
	}

	tlsConfig, err := createTLSConfig(caFile, caCert, insecure)
	if err != nil {
		return nil, fmt.Errorf("failed to create TLS configuration (%w)", err)
	}

	httpClient := http.Client{
		Transport: &http.Transport{
			TLSClientConfig: tlsConfig,
		},
	}

	return &oVirtClient{
		conn:       conn,
		httpClient: httpClient,
		logger:     logger,
		url:        url,
	}, nil
}

func createTLSConfig(
	caFile string,
	caCert []byte,
	insecure bool,
) (*tls.Config, error) {
	tlsConfig := &tls.Config{
		// Based on Mozilla intermediate compatibility:
		// https://wiki.mozilla.org/Security/Server_Side_TLS#Intermediate_compatibility_.28recommended.29
		MinVersion: tls.VersionTLS12,
		CipherSuites: []uint16{
			tls.TLS_ECDHE_ECDSA_WITH_AES_128_GCM_SHA256,
			tls.TLS_ECDHE_RSA_WITH_AES_128_GCM_SHA256,
			tls.TLS_ECDHE_ECDSA_WITH_AES_256_GCM_SHA384,
			tls.TLS_ECDHE_RSA_WITH_AES_256_GCM_SHA384,
			tls.TLS_ECDHE_ECDSA_WITH_CHACHA20_POLY1305,
			tls.TLS_ECDHE_RSA_WITH_CHACHA20_POLY1305,
		},
		CurvePreferences: []tls.CurveID{
			tls.CurveP256, tls.CurveP384,
		},
		PreferServerCipherSuites: false,
		InsecureSkipVerify:       insecure,
	}

	certPool, err := x509.SystemCertPool()
	if err != nil {
		// This is the case on Windows where the system certificate pool is not available.
		certPool = x509.NewCertPool()
	}
	if len(caCert) != 0 {
		if ok := certPool.AppendCertsFromPEM(caCert); !ok {
			return nil, fmt.Errorf("the provided CA certificate is not a valid certificate in PEM format")
		}
	}
	if caFile != "" {
		pemData, err := ioutil.ReadFile(caFile)
		if err != nil {
			return nil, fmt.Errorf("failed to read CA certificate from file %s (%w)", caFile, err)
		}
		if ok := certPool.AppendCertsFromPEM(pemData); !ok {
			return nil, fmt.Errorf(
				"the provided CA certificate is not a valid certificate in PEM format in file %s",
				caFile,
			)
		}
	}
	return tlsConfig, nil
}

type oVirtClient struct {
	conn       *ovirtsdk4.Connection
	httpClient http.Client
	logger     Logger
	url        string
}

func (o *oVirtClient) GetSDKClient() *ovirtsdk4.Connection {
	return o.conn
}

func (o *oVirtClient) GetHTTPClient() http.Client {
	return o.httpClient
}

func (o *oVirtClient) GetURL() string {
	return o.url
}

func (o *oVirtClient) RemoveDisk(ctx context.Context, diskID string) error {
	var lastError error
	for {
		if _, err := o.conn.SystemService().DisksService().DiskService(diskID).Remove().Send(); err != nil {
			lastError = fmt.Errorf("failed to remove disk %s (%w)", diskID, err)
		} else {
			return nil
		}

		select {
		case <-time.After(5 * time.Second):
		case <-ctx.Done():
			return fmt.Errorf("timeout while tryint to remove disk %s (last error: %w)", diskID, lastError)
		}
	}
}

func validateUsername(username string) error {
	usernameParts := strings.SplitN(username, "@", 2)
	//nolint:gomnd
	if len(usernameParts) != 2 {
		return fmt.Errorf("username must contain exactly one @ sign (format should be admin@internal)")
	}
	if len(usernameParts[0]) == 0 {
		return fmt.Errorf("no user supplied before @ sign in username (format should be admin@internal)")
	}
	if len(usernameParts[1]) == 0 {
		return fmt.Errorf("no scope supplied after @ sign in username (format should be admin@internal)")
	}
	return nil
}

func validateURL(url string) error {
	if !strings.HasPrefix(url, "http://") && !strings.HasSuffix(url, "https://") {
		return fmt.Errorf("URL must start with http:// or https://")
	}
	return nil
}
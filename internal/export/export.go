package export

import (
	"bytes"
	"crypto/tls"
	"crypto/x509"
	"encoding/base64"
	"fmt"
	"io/ioutil"
	"net"
	"net/http"
	"sync"
	"time"

	"github.com/go-chi/chi/middleware"
	"github.com/go-kit/kit/log"
	"github.com/go-kit/kit/log/level"
	"github.com/pkg/errors"
)

type Endpoints struct {
	Endpoints []Endpoint `yaml:"endpoints"`
}

type Endpoint struct {
	URL string `yaml:"url"`
	// +optional
	TlsConfig *TlsConfig `yaml:"tlsConfig,omitempty"`
	// +optional
	BasicAuth *BasicAuth `yaml:"basicAuth,omitempty"`
}

type TlsConfig struct {
	CA   string `yaml:"ca"`
	Cert string `yaml:"cert"`
	Key  string `yaml:"key"`
}

type BasicAuth struct {
	User     string `yaml:"user"`
	Password string `yaml:"password"`
}

func WithExport(endpoints Endpoints) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			body, _ := ioutil.ReadAll(r.Body)
			_ = r.Body.Close()
			r.Body = ioutil.NopCloser(bytes.NewBuffer(body))

			var wg sync.WaitGroup
			logger := log.NewNopLogger()
			rlogger := log.With(logger, "request", middleware.GetReqID(r.Context()))
			for _, endpoint := range endpoints.Endpoints {
				client := &http.Client{}
				if endpoint.TlsConfig != nil {
					transport, err := createMtlsTransport(*endpoint.TlsConfig)
					if err != nil {
						level.Error(rlogger).Log("msg", "Failed to create mtls transport", "err", err, "url", endpoint.URL)
						return
					}
					client.Transport = transport
				}
				req, err := http.NewRequest(http.MethodPost, endpoint.URL, bytes.NewReader(body))
				if err != nil {
					level.Error(rlogger).Log("msg", "Failed to create the forward request", "err", err, "url", endpoint.URL)
				} else {
					if endpoint.BasicAuth != nil {
						auth := fmt.Sprintf("%s:%s", endpoint.BasicAuth.User, endpoint.BasicAuth.Password)
						authHeader := fmt.Sprintf("Basic %s", base64.StdEncoding.EncodeToString([]byte(auth)))
						req.Header.Add("Authorization", authHeader)
					}
					wg.Add(1)
					go func() {
						defer wg.Done()
						resp, err := client.Do(req)
						if err != nil {
							level.Error(rlogger).Log("msg", "Failed to send request to the server", "err", err)
						} else {
							defer resp.Body.Close()
							if resp.StatusCode >= 300 || resp.StatusCode < 200 {
								responseBody, err := ioutil.ReadAll(resp.Body)
								if err != nil {
									level.Error(rlogger).Log("msg", "Failed to read response of the forward request", "err", err, "return code", resp.Status, "url", endpoint.URL)
								} else {
									level.Error(rlogger).Log("msg", "Failed to forward metrics", "return code", resp.Status, "response", string(responseBody), "url", endpoint.URL)
								}
							} else {
								level.Debug(rlogger).Log("msg", "Metrics forwarded successfully", "url", endpoint.URL)
							}
						}
					}()
				}
			}

			wg.Add(1)
			go func() {
				defer wg.Done()
				next.ServeHTTP(w, r)
			}()

			wg.Wait()
		})
	}
}

func createMtlsTransport(cfg TlsConfig) (*http.Transport, error) {
	// Load Server CA cert
	caCert, err := ioutil.ReadFile(cfg.CA)
	if err != nil {
		return nil, errors.Wrap(err, "failed to load ca cert file")
	}
	// Load client cert/key
	cert, err := tls.LoadX509KeyPair(cfg.Cert, cfg.Key)
	if err != nil {
		return nil, errors.Wrap(err, "failed to load client cert/key file")
	}

	caCertPool := x509.NewCertPool()
	caCertPool.AppendCertsFromPEM(caCert)
	// Setup HTTPS client
	tlsConfig := &tls.Config{
		Certificates: []tls.Certificate{cert},
		RootCAs:      caCertPool,
		MinVersion:   tls.VersionTLS12,
	}
	return &http.Transport{
		Dial: (&net.Dialer{
			Timeout:   30 * time.Second,
			KeepAlive: 30 * time.Second,
		}).Dial,
		TLSHandshakeTimeout: 10 * time.Second,
		DisableKeepAlives:   true,
		TLSClientConfig:     tlsConfig,
	}, nil
}

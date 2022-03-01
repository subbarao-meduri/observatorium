package remotewrite

import (
	"bytes"
	"crypto/tls"
	"crypto/x509"
	"encoding/base64"
	"fmt"
	"io/ioutil"
	"net"
	"net/http"
	"net/url"
	"path"
	"strconv"
	"time"

	"github.com/go-chi/chi/middleware"
	"github.com/go-kit/kit/log"
	"github.com/go-kit/kit/log/level"
	"github.com/pkg/errors"
	"github.com/prometheus/client_golang/prometheus"
)

const (
	THANOS_ENDPOINT_NAME = "thanos-receiver"
)

type Endpoints struct {
	Endpoints []Endpoint `yaml:"endpoints"`
}

type Endpoint struct {
	Name string `yaml:"name"`
	URL  string `yaml:"url"`
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

func Proxy(write *url.URL, endpoints *Endpoints, logger log.Logger, r *prometheus.Registry) http.Handler {

	requests := prometheus.NewCounterVec(prometheus.CounterOpts{
		Name:        "http_proxy_requests_total",
		Help:        "Counter of proxy HTTP requests.",
		ConstLabels: prometheus.Labels{"proxy": "metricsv1-write"},
	}, []string{"method"})
	r.MustRegister(requests)

	if write != nil {
		endpoints.Endpoints = append(endpoints.Endpoints, Endpoint{
			URL:  write.String(),
			Name: THANOS_ENDPOINT_NAME,
		})
	}

	remotewriteRequests := prometheus.NewCounterVec(prometheus.CounterOpts{
		Name:        "remote_write_requests_total",
		Help:        "Counter of remote write requests.",
		ConstLabels: prometheus.Labels{"proxy": "metricsv1-remotewrite"},
	}, []string{"code", "name"})
	r.MustRegister(remotewriteRequests)

	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {

		requests.With(prometheus.Labels{"method": r.Method}).Inc()

		body, _ := ioutil.ReadAll(r.Body)
		_ = r.Body.Close()
		r.Body = ioutil.NopCloser(bytes.NewBuffer(body))

		if endpoints == nil {
			endpoints = &Endpoints{}
		}
		if write != nil {
			remotewriteUrl := url.URL{}
			remotewriteUrl.Path = path.Join(write.Path, r.URL.Path)
			remotewriteUrl.Host = write.Host
			remotewriteUrl.Scheme = write.Scheme
			endpoints.Endpoints[len(endpoints.Endpoints)-1].URL = remotewriteUrl.String()
		}

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
			req.Header = r.Header
			if err != nil {
				level.Error(rlogger).Log("msg", "Failed to create the forward request", "err", err, "url", endpoint.URL)
			} else {
				if endpoint.BasicAuth != nil {
					auth := fmt.Sprintf("%s:%s", endpoint.BasicAuth.User, endpoint.BasicAuth.Password)
					authHeader := fmt.Sprintf("Basic %s", base64.StdEncoding.EncodeToString([]byte(auth)))
					req.Header.Add("Authorization", authHeader)
				}
				ep := endpoint
				go func() {
					resp, err := client.Do(req)
					if err != nil {
						remotewriteRequests.With(prometheus.Labels{"code": "<error>", "name": ep.Name}).Inc()
						level.Error(rlogger).Log("msg", "Failed to send request to the server", "err", err)
					} else {
						defer resp.Body.Close()
						remotewriteRequests.With(prometheus.Labels{"code": strconv.Itoa(resp.StatusCode), "name": ep.Name}).Inc()
						if resp.StatusCode >= 300 || resp.StatusCode < 200 {
							responseBody, err := ioutil.ReadAll(resp.Body)
							if err != nil {
								level.Error(rlogger).Log("msg", "Failed to read response of the forward request", "err", err, "return code", resp.Status, "url", ep.URL)
							} else {
								level.Error(rlogger).Log("msg", "Failed to forward metrics", "return code", resp.Status, "response", string(responseBody), "url", ep.URL)
							}
						} else {
							level.Debug(rlogger).Log("msg", "Metrics forwarded successfully", "url", ep.URL)
						}
					}
				}()
			}
		}
	})
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

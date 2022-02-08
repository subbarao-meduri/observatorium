package export

import (
	"bytes"
	"io/ioutil"
	stdlog "log"
	"net/http"
	"net/url"
	"sync"
)

func WithExport(endpoint *url.URL) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			body, _ := ioutil.ReadAll(r.Body)
			r.Body.Close()
			r.Body = ioutil.NopCloser(bytes.NewBuffer(body))

			var wg sync.WaitGroup
			wg.Add(2)

			go func() {
				defer wg.Done()
				client := &http.Client{}
				req, err := http.NewRequest(http.MethodPost, endpoint.String(), bytes.NewReader(body))
				if err != nil {
					stdlog.Printf("Failed to create the forward request due to %v", err)
				} else {
					resp, err := client.Do(req)
					if err != nil {
						stdlog.Println("Errored when sending request to the server")
					} else {
						defer resp.Body.Close()
						responseBody, err := ioutil.ReadAll(resp.Body)
						if err != nil {
							stdlog.Printf("Failed to read response of the forward request due to %v", err)
						} else if resp.StatusCode > 300 {
							stdlog.Printf("Failed to forward metrics, status code is %s, response is %s", resp.Status, string(responseBody))
						}
					}
				}
			}()

			go func() {
				defer wg.Done()
				next.ServeHTTP(w, r)
			}()

			wg.Wait()
		})
	}
}

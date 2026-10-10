package middleware

import (
	"crypto/tls"
	"fmt"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/4thPlanet/dispatch"
)

func TestHostnameEnforcement(t *testing.T) {
	t.Run("Validation", func(t *testing.T) {
		for _, badInput := range []string{
			"",
			"https://example.com",
			"example.com:80:80",
			"user@example.com",
			"example.com?foo",
			"example.com#bar",
		} {
			testName := badInput
			if testName == "" {
				testName = "<empty hostname>"
			}
			t.Run(testName, func(t *testing.T) {
				defer func() {
					if r := recover(); r == nil {
						t.Errorf("HostnameEnforcement did not panic")
					}
				}()
				HostnameEnforcement[*mockRequest](badInput)
			})
		}
	})

	for _, test := range []struct {
		Name string
		Port *int
	}{
		{"Standard", nil},
		{"Non-Standard", new(8080)},
	} {
		hostname := "example.com"
		port := ""
		if test.Port != nil {
			port = fmt.Sprintf(":%d", *test.Port)
			hostname += port
		}
		t.Run(test.Name, func(t *testing.T) {
			handler := dispatch.NewTypedHandler(func(r *http.Request) *mockRequest {
				return &mockRequest{r: r}
			})
			handler.HandleFunc("/", func(w http.ResponseWriter, r *mockRequest) {
				w.Write(testBody)
			})
			handler.UseMiddleware(HostnameEnforcement[*mockRequest](hostname))

			for _, test := range []struct {
				Scheme   string
				Hostname string
				Method   string
				Target   string
				Code     int
			}{
				{"http", "example.org" + port, http.MethodGet, "/", http.StatusPermanentRedirect},
				{"https", "example.org" + port, http.MethodGet, "/", http.StatusPermanentRedirect},
				{"http", "example.org" + port, http.MethodPost, "/", http.StatusPermanentRedirect},
				{"http", "example.org" + port, http.MethodGet, "/foo", http.StatusPermanentRedirect},
				{"http", "example.org" + port, http.MethodGet, "/?bar=baz", http.StatusPermanentRedirect},
				{"http", "example.org" + port, http.MethodGet, "/foo?bar=baz", http.StatusPermanentRedirect},
				{"http", "127.0.0.1" + port, http.MethodGet, "/", http.StatusPermanentRedirect},
				{"http", "example.com" + port, http.MethodGet, "/", http.StatusOK},
				{"http", "eXaMpLe.com" + port, http.MethodGet, "/", http.StatusOK},
				{"http", "example.com" + port, http.MethodPost, "/", http.StatusOK},
			} {
				req := httptest.NewRequest(test.Method, test.Target, nil)
				req.Host = test.Hostname

				if test.Scheme == "https" {
					req.TLS = new(tls.ConnectionState)
				}
				testname := fmt.Sprintf("%s %s://%s%s", test.Method, test.Scheme, test.Hostname, test.Target)
				t.Run(testname, func(t *testing.T) {
					res := httptest.NewRecorder()
					handler.ServeHTTP(res, req)
					if got, want := res.Code, test.Code; got != want {
						t.Errorf("Unexpected response code. Got: %v, Want: %v", got, want)
					}
					if res.Code == http.StatusOK {
						if got, want := res.Body.String(), string(testBody); got != want {
							t.Errorf("Unexpected response body. Got: %v, Want: %v", got, want)
						}
					} else {
						if got, want := res.Header().Get("Location"), test.Scheme+"://example.com"+port+test.Target; got != want {
							t.Errorf("Unexpected Location header. Got: %v, Want: %v", got, want)
						}
					}
				})
			}

		})
	}
}

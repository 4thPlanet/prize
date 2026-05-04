package middleware

import (
	"bytes"
	"crypto/tls"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strconv"
	"testing"
	"time"

	"github.com/4thPlanet/dispatch"
)

func TestLogBuilder(t *testing.T) {
	for _, test := range []struct {
		FormatString string
		Output       string
		ModifyReq    func(*http.Request)
		ModifyWL     func(*writerLog)
	}{
		// Literals
		{"Hello", "Hello", nil, nil},
		{"%%", "%", nil, nil},
		{"Hello %% World", "Hello % World", nil, nil},

		// %a / %h — remote IP (writerLog strips port)
		{"%a", "192.0.2.1", nil, nil},
		{"%h", "192.0.2.1", nil, nil},

		// %A — local IP (set via context by the server; httptest doesn't set it, so expect "-")
		{"%A", "-", nil, nil},

		// %b — response bytes, "-" when zero
		{"%b", "500", nil, nil},
		{"%b", "-", nil, func(wl *writerLog) { wl.length = 0 }},

		// %B — response bytes, "0" when zero
		{"%B", "500", nil, nil},
		{"%B", "0", nil, func(wl *writerLog) { wl.length = 0 }},

		// %D — duration in microseconds: 2s + 25ms + 300us = 2_025_300
		{"%D", "2025300", nil, nil},

		// %H — protocol
		{"%H", "HTTP/1.1", nil, nil},

		// %l — always "-"
		{"%l", "-", nil, nil},

		// %m — method
		{"%m", "GET", nil, nil},

		// %p — port: httptest request has no port in Host, no TLS → "80"
		{"%p", "80", nil, nil},
		{"%p", "443", func(r *http.Request) {
			r.TLS = &tls.ConnectionState{}
		}, nil},
		{"%p", "8080", func(r *http.Request) {
			r.Host = "example.com:8080"
		}, nil},

		// %q — query string
		{"%q", "", nil, nil},
		{"%q", "?foo=bar", func(r *http.Request) {
			r.URL.RawQuery = "foo=bar"
		}, nil},

		// %r — request line
		{"%r", "GET /path HTTP/1.1", nil, nil},

		// %s — status code
		{"%s", "418", nil, nil},

		// %t — timestamp (Unix 1750000000 = 2025-06-15 in UTC)
		{"%t", "[15/Jun/2025:15:06:40 +0000]", nil, nil},

		// %T — duration in whole seconds
		{"%T", "2", nil, nil},

		// %u — basic auth user
		{"%u", "-", nil, nil},
		{"%u", "alice", func(r *http.Request) {
			r.SetBasicAuth("alice", "password")
		}, nil},

		// %U — path only, no query
		{"%U", "/path", nil, nil},

		// %v / %V — server name; httptest sets Host header
		{"%v", "example.com", func(r *http.Request) {
			r.Host = "example.com"
		}, nil},
		{"%V", "example.com", func(r *http.Request) {
			r.Host = "example.com"
		}, nil},
		{"%v", "example.com", func(r *http.Request) {
			r.Host = "example.com:9000"
		}, nil},

		// %X — always "-"
		{"%X", "-", nil, nil},

		// Parameterised: %{name}i — request header
		{"%{X-Forwarded-For}i", "10.0.0.1", func(r *http.Request) {
			r.Header.Set("X-Forwarded-For", "10.0.0.1")
		}, nil},
		{"%{X-Forwarded-For}i", "", nil, nil}, // absent header → empty string

		// Parameterised: %{name}o — response header
		{"%{Content-Type}o", "application/json", func(r *http.Request) {}, func(wl *writerLog) {
			wl.ResponseWriter.Header().Set("Content-Type", "application/json")
		}},

		// Parameterised: %{format}t — custom time format
		{"%{2006}t", "2025", nil, nil},
		{"%{15:04}t", "15:06", nil, nil},

		// Parameterised: %{name}C — cookie value
		{"%{session}C", "abc123", func(r *http.Request) {
			r.AddCookie(&http.Cookie{Name: "session", Value: "abc123"})
		}, nil},
		{"%{session}C", "", nil, nil}, // absent cookie → empty string

		// Parameterised: %{name}e — environment variable
		{"%{TEST_LOG_VAR}e", "hello", func(r *http.Request) {
			t.Setenv("TEST_LOG_VAR", "hello")
		}, nil},
		{"%{TEST_LOG_VAR}e", "", func(r *http.Request) {
			t.Setenv("TEST_LOG_VAR", "")
		}, nil}, // unset env var → empty string

		// Parameterised: %{local}p / %{remote}p
		{"%{local}p", "80", nil, nil},
		{"%{remote}p", "1234", func(r *http.Request) {
			r.RemoteAddr = "192.0.2.1:1234"
		}, nil},

		// Parameterised: %{pid}P — process ID (just check it's numeric, not a fixed value)
		// We test this separately below since the value is dynamic.

		// Parameterised: %{T}T / %{ms}T / %{us}T / %{s}T
		{"%{s}T", "2", nil, nil},
		{"%{ms}T", "2025", nil, nil},
		{"%{us}T", "2025300", nil, nil},

		// JSON-sensitive characters in values should be escaped
		{"%{X-Custom}i", `hello \"world\"`, func(r *http.Request) {
			r.Header.Set("X-Custom", `hello "world"`)
		}, nil},

		// Combined format
		{"%m %U%q %s", "GET /path 418", nil, nil},
		{"%m %U%q %s", "GET /path?foo=bar 418", func(r *http.Request) {
			r.URL.RawQuery = "foo=bar"
		}, nil},
	} {
		test := test
		t.Run(test.FormatString, func(t *testing.T) {
			req := httptest.NewRequest(http.MethodGet, "/path", nil)
			req.RemoteAddr = "192.0.2.1:5000"

			res := httptest.NewRecorder()
			wl := new(writerLog)
			wl.ResponseWriter = res
			wl.length = 500
			wl.code = http.StatusTeapot
			startTime := time.Unix(1750000000, 0).UTC()
			duration := (time.Second * 2) + (time.Millisecond * 25) + (time.Microsecond * 300)

			if test.ModifyReq != nil {
				test.ModifyReq(req)
			}
			if test.ModifyWL != nil {
				test.ModifyWL(wl)
			}

			got := logBuilder(test.FormatString, req, wl, startTime, duration)
			if got != test.Output {
				t.Errorf("format %q: got %q, want %q", test.FormatString, got, test.Output)
			}
		})
	}

	// %{pid}P is dynamic, so we just verify it's a non-zero integer
	t.Run("%{pid}P", func(t *testing.T) {
		req := httptest.NewRequest(http.MethodGet, "/", nil)
		res := httptest.NewRecorder()
		wl := new(writerLog)
		wl.ResponseWriter = res
		got := logBuilder("%{pid}P", req, wl, time.Now(), 0)
		if _, err := strconv.Atoi(got); err != nil {
			t.Errorf("%%{pid}P: expected numeric PID, got %q", got)
		}
		if got == "0" {
			t.Errorf("%%{pid}P: expected non-zero PID, got 0")
		}
	})
}

type mockLogger struct{ *bytes.Buffer }

func (l *mockLogger) Print(args ...any) {
	fmt.Fprint(l.Buffer, args...)
}
func (l *mockLogger) Printf(format string, args ...any) {
	fmt.Fprintf(l.Buffer, format, args...)
}

func TestLogger(t *testing.T) {
	buf := &mockLogger{Buffer: new(bytes.Buffer)}
	logger := Logger[*mockRequest]("%m %I %b %s", buf)
	req := httptest.NewRequest(http.MethodPost, "/", bytes.NewBuffer(make([]byte, 100)))
	res := httptest.NewRecorder()
	logger(res, &mockRequest{r: req}, func(w http.ResponseWriter, r *mockRequest, next dispatch.Middleware[*mockRequest]) {
		w.WriteHeader(http.StatusTeapot)
		w.Write(testBody)
	})

	// Confirm buf.Buffer contains request body size + response body size
	if got, want := buf.String(), fmt.Sprintf("%s %d %d %d", http.MethodPost, 100, len(testBody), http.StatusTeapot); got != want {
		t.Errorf("Unexpect log recorder. Got %v, Want %v", got, want)
	}
}

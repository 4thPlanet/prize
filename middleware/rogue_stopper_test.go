package middleware

import (
	"context"
	"io"
	"net/http"
	"net/http/httptest"
	"sync"
	"testing"
	"testing/synctest"
	"time"

	"github.com/4thPlanet/dispatch"
	"github.com/4thPlanet/prize/cache"
)

func TestRogueStopper(t *testing.T) {
	t.Run("Initialization", func(t *testing.T) {
		wg := sync.WaitGroup{}
		wg.Go(func() {
			defer func() {
				if r := recover(); r == nil {
					t.Errorf("RogueStopper initialization did not panic on maxBadRequests = 0.")
				}
			}()
			memory := cache.NewInMemoryCache()
			defer memory.Close()
			NewRogueStopper(
				memory,
				func(r *mockRequest) string { return "" },
				0,
				time.Second,
				time.Minute,
				io.Discard,
			)
		})
		wg.Go(func() {
			defer func() {
				if r := recover(); r == nil {
					t.Errorf("RogueStopper initialization did not panic on perDuration = 0.")
				}
			}()
			memory := cache.NewInMemoryCache()
			defer memory.Close()
			NewRogueStopper(
				memory,
				func(r *mockRequest) string { return "" },
				1,
				0,
				time.Minute,
				io.Discard,
			)
		})
		wg.Go(func() {
			defer func() {
				if r := recover(); r == nil {
					t.Errorf("RogueStopper initialization did not panic on timeout = 0.")
				}
			}()
			memory := cache.NewInMemoryCache()
			defer memory.Close()
			NewRogueStopper(
				memory,
				func(r *mockRequest) string { return "" },
				1,
				time.Second,
				0,
				io.Discard,
			)
		})

		wg.Wait()
	})

	t.Run("IgnoreDoubleCodes", func(t *testing.T) {
		handler := dispatch.NewTypedHandler(func(r *http.Request) *mockRequest {
			return &mockRequest{r: r}
		})
		handler.HandleFunc("/", func(w http.ResponseWriter, r *mockRequest) {
			w.WriteHeader(http.StatusOK)
			w.WriteHeader(http.StatusTeapot)
			w.Write(testBody)
		})
		memory := cache.NewInMemoryCache()
		defer memory.Close()

		// If a user has any 4xx response codes in a second, then time them out for a minute
		handler.UseMiddleware(NewRogueStopper(
			memory,
			func(r *mockRequest) string {
				return ""
			},
			1, time.Second, time.Minute, io.Discard,
		))

		for range 10 {
			req := httptest.NewRequest(http.MethodGet, "/", nil)
			res := httptest.NewRecorder()
			handler.ServeHTTP(res, req)

			if got, want := res.Code, http.StatusOK; got != want {
				t.Errorf("Unexpected response code. Got: %v, Want: %v", got, want)
			}
			if got, want := string(res.Body.Bytes()), string(testBody); got != want {
				t.Errorf("Unexpected response body. Got: %v, Want: %v", got, want)
			}

		}
	})

	synctest.Test(t, func(t *testing.T) {
		handler := dispatch.NewTypedHandler(func(r *http.Request) *mockRequest {
			return &mockRequest{r: r}
		})
		handler.HandleFunc("/", func(w http.ResponseWriter, r *mockRequest) {
			w.WriteHeader(http.StatusTeapot)
		})
		memory := cache.NewInMemoryCache()
		defer memory.Close()

		// If a user has any 4xx response codes in a second, then time them out for a minute
		handler.UseMiddleware(NewRogueStopper(
			memory,
			func(r *mockRequest) string {
				return r.Request().Context().Value("sender_id").(string)
			},
			1, time.Second, time.Minute, io.Discard,
		))

		for tdx, test := range []struct {
			SenderId     string
			ExpectedCode int
			WaitRequired time.Duration
		}{
			{"foo", http.StatusTeapot, 0},
			{"foo", http.StatusForbidden, 0},
			{"bar", http.StatusTeapot, 0},
			{"foo", http.StatusForbidden, time.Second * 30},
			{"bar", http.StatusForbidden, 0},
			{"foo", http.StatusTeapot, time.Second * 30},
			{"bar", http.StatusTeapot, 0},
		} {
			if test.WaitRequired > 0 {
				time.Sleep(test.WaitRequired + time.Nanosecond)
			}
			req := httptest.NewRequest(http.MethodGet, "/", nil)
			req = req.WithContext(context.WithValue(req.Context(), "sender_id", test.SenderId))
			res := httptest.NewRecorder()
			handler.ServeHTTP(res, req)
			t.Logf("Request #%d", tdx+1)

			if got, want := res.Code, test.ExpectedCode; got != want {
				t.Errorf("Unexpected response code. Got: %v, Want: %v", got, want)
			}
		}

	})
}

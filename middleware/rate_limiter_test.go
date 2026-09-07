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

func TestRateLimiter(t *testing.T) {

	t.Run("Initialization", func(t *testing.T) {
		wg := sync.WaitGroup{}
		wg.Go(func() {
			defer func() {
				if r := recover(); r == nil {
					t.Errorf("RateLimiter initialization did not panic on maxVisits = 0.")
				}
			}()
			memory := cache.NewInMemoryCache()
			defer memory.Close()
			NewRateLimiter(
				memory,
				func(r *mockRequest) string { return "" },
				0,
				time.Second,
				io.Discard,
			)
		})
		wg.Go(func() {
			defer func() {
				if r := recover(); r == nil {
					t.Errorf("RateLimiter initialization did not panic on perDuration = 0.")
				}
			}()
			memory := cache.NewInMemoryCache()
			defer memory.Close()
			NewRateLimiter(
				memory,
				func(r *mockRequest) string { return "" },
				1,
				0,
				io.Discard,
			)
		})

		wg.Wait()

	})

	synctest.Test(t, func(t *testing.T) {
		handler := dispatch.NewTypedHandler(func(r *http.Request) *mockRequest {
			return &mockRequest{r: r}
		})
		handler.HandleFunc("/", func(w http.ResponseWriter, r *mockRequest) {
			w.Write(testBody)
		})
		memory := cache.NewInMemoryCache()
		defer memory.Close()
		handler.UseMiddleware(NewRateLimiter(
			memory,
			func(r *mockRequest) string {
				return r.Request().Context().Value("sender_id").(string)
			},
			1,
			time.Second,
			io.Discard,
		))

		for tdx, test := range []struct {
			SenderId     string
			ExpectedCode int
			WaitRequired bool
		}{
			{"foo", http.StatusOK, false},
			{"foo", http.StatusTooManyRequests, false},
			{"bar", http.StatusOK, false},
			{"foo", http.StatusOK, true},
			{"bar", http.StatusOK, false},
		} {
			if test.WaitRequired {
				time.Sleep(time.Second + time.Nanosecond)
			}
			req := httptest.NewRequest(http.MethodGet, "/", nil)
			req = req.WithContext(context.WithValue(req.Context(), "sender_id", test.SenderId))
			res := httptest.NewRecorder()
			handler.ServeHTTP(res, req)
			t.Logf("Request #%d", tdx+1)

			if got, want := res.Code, test.ExpectedCode; got != want {
				t.Errorf("Unexpected response code. Got: %v, Want: %v", got, want)
			}
			if res.Code == http.StatusOK {
				if got, want := string(res.Body.Bytes()), string(testBody); got != want {
					t.Errorf("Unexpected response body. Got: %v, Want: %v", got, want)
				}
			}

		}
	})
}

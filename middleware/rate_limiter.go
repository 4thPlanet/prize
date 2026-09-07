package middleware

import (
	"fmt"
	"io"
	"net/http"
	"time"

	"github.com/4thPlanet/dispatch"
	"github.com/4thPlanet/prize/cache"
)

type RateLimiter[R dispatch.RequestAdapter] struct {
	cache     cache.Cache
	idFunc    func(R) string
	duration  time.Duration
	maxVisits int
	log       io.Writer
}

func NewRateLimiter[R dispatch.RequestAdapter](
	cacheProvider cache.Cache,
	idFunc func(R) string,
	maxVisits int,
	perDuration time.Duration,
	log io.Writer,
) *RateLimiter[R] {
	if maxVisits == 0 {
		panic("rate limiter requires non-zero maximum visits")
	}
	if perDuration == 0 {
		panic("rate limiter requires non-zero duration")
	}
	return &RateLimiter[R]{
		cache:     cacheProvider,
		idFunc:    idFunc,
		maxVisits: maxVisits,
		duration:  perDuration,
		log:       log,
	}
}

func (rl *RateLimiter[R]) Enter(w http.ResponseWriter, r R) (http.ResponseWriter, R, bool) {
	id := rl.idFunc(r)
	var recentHits int

	if err := rl.cache.Atomic(r.Request().Context(), id, func(cv any) any {
		now := time.Now()
		var hits []time.Time
		var ok bool
		if hits, ok = cv.([]time.Time); ok {
			cutoff := 0
			for _, hit := range hits {
				if hit.Add(rl.duration).Before(now) {
					cutoff++
				} else {
					break
				}
			}
			hits = hits[cutoff:]
		}

		recentHits = len(hits)
		if len(hits) < rl.maxVisits {
			hits = append(hits, now)
		} else {
			copy(hits, hits[1:])
			hits[recentHits-1] = now
		}

		return hits
	}, &rl.duration); err != nil {
		fmt.Fprintf(rl.log, "prize: RateLimiter: Failed to record visit: %v", err)
		return w, r, true
	}

	if recentHits >= rl.maxVisits {
		w.WriteHeader(http.StatusTooManyRequests)
		return w, r, false
	}

	return w, r, true
}
func (rl *RateLimiter[R]) Exit(w http.ResponseWriter, r R) {}

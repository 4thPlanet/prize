package middleware

import (
	"bufio"
	"context"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"time"

	"github.com/4thPlanet/dispatch"
	"github.com/4thPlanet/prize/cache"
)

type RogueStopper[R dispatch.RequestAdapter] struct {
	cache          cache.Cache
	idFunc         func(R) string
	duration       time.Duration
	maxBadRequests int
	timeout        time.Duration
	log            io.Writer
	ttl            time.Duration
}

func NewRogueStopper[R dispatch.RequestAdapter](
	cacheProvider cache.Cache,
	idFunc func(R) string,
	maxBadRequests int,
	perDuration time.Duration,
	timeout time.Duration,
	log io.Writer,
) *RogueStopper[R] {
	if maxBadRequests == 0 {
		panic("bad actor catcher requires non-zero maximum bad requests")
	}
	if perDuration == 0 {
		panic("bad actor catcher requires non-zero duration")
	}
	if timeout == 0 {
		panic("bad actor catcher requires non-zero timeout")
	}
	ttl := perDuration
	if timeout > ttl {
		ttl = timeout
	}
	return &RogueStopper[R]{
		cache:          cacheProvider,
		idFunc:         idFunc,
		duration:       perDuration,
		maxBadRequests: maxBadRequests,
		timeout:        timeout,
		log:            log,
		ttl:            ttl,
	}
}

type badRequestHistory struct {
	visitHistory []time.Time
	banUntil     *time.Time
}

type rogueRecorder[R dispatch.RequestAdapter] struct {
	http.ResponseWriter
	*RogueStopper[R]
	userid        string
	headerWritten bool
}

func (w *rogueRecorder[R]) WriteHeader(code int) {
	if w.headerWritten {
		fmt.Fprintf(w.log, "prize: RogueStopper: multiple response codes written on the same request.")
		return
	}
	w.ResponseWriter.WriteHeader(code)
	w.headerWritten = true
	now := time.Now()
	if code >= 400 && code < 500 {
		if err := w.cache.Atomic(context.Background(), w.userid, func(val any) any {
			history, _ := val.(badRequestHistory)
			history.visitHistory = append(history.visitHistory, now)
			cutoff := 0
			for _, visitTime := range history.visitHistory {
				if visitTime.Add(w.duration).Before(now) {
					cutoff++
				} else {
					break
				}
			}
			history.visitHistory = history.visitHistory[cutoff:]

			if len(history.visitHistory) >= w.maxBadRequests {
				history.banUntil = new(now.Add(w.timeout))
			}

			return history

		}, &w.ttl); err != nil {
			fmt.Fprintf(w.log, "prize: RogueStopper: Failed to record 4xx visit: %v", err)
		}
	}
}
func (w *rogueRecorder[R]) Flush() {
	if f, ok := w.ResponseWriter.(http.Flusher); ok {
		f.Flush()
	}
}
func (w *rogueRecorder[R]) Hijack() (net.Conn, *bufio.ReadWriter, error) {
	if hj, ok := w.ResponseWriter.(http.Hijacker); !ok {
		return nil, nil, errors.New("hijack not supported")
	} else {
		return hj.Hijack()
	}
}

func (bac *RogueStopper[R]) Enter(w http.ResponseWriter, r R) (http.ResponseWriter, R, bool) {
	id := bac.idFunc(r)
	val, err := bac.cache.Get(r.Request().Context(), id)
	var userHistory badRequestHistory
	if err == nil {
		userHistory, _ = val.(badRequestHistory)
	}
	if userHistory.banUntil != nil && userHistory.banUntil.After(time.Now()) {
		w.WriteHeader(http.StatusForbidden)
		return w, r, false
	}

	// Wrap in writer-wrapper which records 4xx response codes
	w = &rogueRecorder[R]{
		ResponseWriter: w,
		RogueStopper:   bac,
		userid:         id,
	}

	return w, r, true
}
func (bac *RogueStopper[R]) Exit(w http.ResponseWriter, r R) {}

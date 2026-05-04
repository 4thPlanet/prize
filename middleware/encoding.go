package middleware

import (
	"bufio"
	"cmp"
	"compress/flate"
	"compress/gzip"
	"errors"
	"io"
	"net"
	"net/http"
	"slices"
	"strconv"
	"strings"
	"sync"

	"github.com/4thPlanet/dispatch"
)

type encodingWriter struct {
	http.ResponseWriter
	write func([]byte) (int, error)
	flush func()
}

func (ew *encodingWriter) Write(in []byte) (int, error) {
	return ew.write(in)
}
func (ew *encodingWriter) Flush() {
	if ew.flush != nil {
		ew.flush()
	} else {
		if f, ok := ew.ResponseWriter.(http.Flusher); ok {
			f.Flush()
		}
	}
}
func (ew *encodingWriter) Hijack() (net.Conn, *bufio.ReadWriter, error) {
	if hj, ok := ew.ResponseWriter.(http.Hijacker); ok {
		return hj.Hijack()
	} else {
		return nil, nil, errors.New("Hijacker is not available.")
	}

}
func (ew *encodingWriter) reset(w http.ResponseWriter) {
	ew.ResponseWriter = w
	ew.write = nil
	ew.flush = nil
}

type ContentEncoder interface {
	Name() string
	Create(io.Writer) io.Writer
}

type acceptedEncoding struct {
	encoding string
	weight   float64
}

type WriteFlusher interface {
	io.Writer
	Flush() error
}

// This function strongly borrows from github.com/4thPlanet/dispatch/content_type.go::negotiateContentType. However it's much simpler as reflection isn't needed, and subtypes + specificity are not required for consideration.
func negotiateEncoding(acceptHeader string, providers map[string]func(w http.ResponseWriter) io.Writer) string {
	if len(acceptHeader) == 0 {
		acceptHeader = "identity"
	}
	acceptHeaderSplit := strings.Split(acceptHeader, ",")
	acceptedEncodings := make([]acceptedEncoding, 0, len(acceptHeaderSplit))
	for _, encoding := range acceptHeaderSplit {
		qualitySplit := strings.Split(strings.TrimSpace(encoding), ";q=")

		found := false
		var acceptedEncoding = acceptedEncoding{encoding: qualitySplit[0]}
		for provider := range providers {
			if qualitySplit[0] == provider {
				found = true
				break
			}
		}
		if !found {
			continue
		}
		acceptedEncoding.weight = 1.0
		if len(qualitySplit) > 1 {
			if weight, err := strconv.ParseFloat(qualitySplit[1], 64); err != nil {
				continue // Invalid weight
			} else {
				acceptedEncoding.weight = weight
			}
		}
		if acceptedEncoding.weight == 1.0 {
			// short-circuit the result, this is as good as you'll get
			return acceptedEncoding.encoding
		} else {
			acceptedEncodings = append(acceptedEncodings, acceptedEncoding)
		}

	}
	if len(acceptedEncodings) == 0 {
		return ""
	}
	highestWeighted := slices.MaxFunc(acceptedEncodings, func(a, b acceptedEncoding) int {
		return cmp.Compare(a.weight, b.weight)
	})

	return highestWeighted.encoding
}

func ContentEncoding[R dispatch.RequestAdapter](withProviders ...ContentEncoder) dispatch.Middleware[R] {
	writerPool := sync.Pool{
		New: func() any {
			return new(encodingWriter)
		},
	}

	allProviders := map[string]func(w http.ResponseWriter) io.Writer{
		"gzip": func(w http.ResponseWriter) io.Writer {
			return gzip.NewWriter(w)
		},
		"deflate": func(w http.ResponseWriter) io.Writer {
			encoding, _ := flate.NewWriter(w, flate.DefaultCompression)
			return encoding
		},
		"identity": func(w http.ResponseWriter) io.Writer {
			return w
		},
	}
	for _, provider := range withProviders {
		allProviders[provider.Name()] = func(w http.ResponseWriter) io.Writer {
			encoding := provider.Create(w)
			return encoding
		}
	}

	return func(w http.ResponseWriter, r R, next dispatch.Middleware[R]) {
		acceptedEncoding := negotiateEncoding(r.Request().Header.Get("Accept-Encoding"), allProviders)
		if acceptedEncoding == "" {
			w.WriteHeader(http.StatusNotAcceptable)
			return
		}
		wrappedWriter := writerPool.Get().(*encodingWriter)
		wrappedWriter.reset(w)
		defer writerPool.Put(wrappedWriter)
		w.Header().Set("Content-Encoding", acceptedEncoding)
		fn := allProviders[acceptedEncoding]
		encoding := fn(w)
		wrappedWriter.write = encoding.Write
		if f, ok := encoding.(http.Flusher); ok {
			wrappedWriter.flush = f.Flush
		} else if f, ok := encoding.(WriteFlusher); ok {
			wrappedWriter.flush = func() { _ = f.Flush() }
		}
		if c, ok := encoding.(io.Closer); ok && encoding != w {
			defer c.Close()
		}

		if wrappedWriter.flush != nil {
			defer wrappedWriter.flush()
		}
		next(wrappedWriter, r, next)
	}
}

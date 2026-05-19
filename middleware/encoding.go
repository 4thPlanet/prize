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
	Pool() *sync.Pool
}

type acceptedEncoding struct {
	encoding string
	weight   float64
}

type WriteFlusher interface {
	io.Writer
	Flush() error
}

type DispatchEncoder interface {
	dispatch.RequestAdapter
	Encoder() *EncodingData
}

type EncodingData struct {
	wrappedWriter *encodingWriter
	encoding      io.Writer
	pool          *sync.Pool
}

type encodingMW[R DispatchEncoder] struct {
	providers    map[string]func(w http.ResponseWriter) (io.Writer, *sync.Pool)
	encodingPool sync.Pool
}

// This function strongly borrows from github.com/4thPlanet/dispatch/content_type.go::negotiateContentType. However it's much simpler as reflection isn't needed, and subtypes + specificity are not required for consideration.
func (mw *encodingMW[R]) negotiateEncoding(acceptHeader string) string {
	if len(acceptHeader) == 0 {
		acceptHeader = "identity"
	}
	acceptHeaderSplit := strings.Split(acceptHeader, ",")
	acceptedEncodings := make([]acceptedEncoding, 0, len(acceptHeaderSplit))
	for _, encoding := range acceptHeaderSplit {
		qualitySplit := strings.Split(strings.TrimSpace(encoding), ";q=")

		found := false
		var acceptedEncoding = acceptedEncoding{encoding: qualitySplit[0]}
		for provider := range mw.providers {
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

func (mw *encodingMW[R]) Enter(w http.ResponseWriter, r R) (http.ResponseWriter, R, bool) {
	data := r.Encoder()

	acceptedEncoding := mw.negotiateEncoding(r.Request().Header.Get("Accept-Encoding"))
	if acceptedEncoding == "" {
		w.WriteHeader(http.StatusNotAcceptable)
		return w, r, false
	}
	data.wrappedWriter = mw.encodingPool.Get().(*encodingWriter)
	wrappedWriter := data.wrappedWriter
	wrappedWriter.reset(w)
	w.Header().Set("Content-Encoding", acceptedEncoding)
	fn := mw.providers[acceptedEncoding]
	encoding, pool := fn(w)
	data.pool = pool

	wrappedWriter.write = encoding.Write
	if f, ok := encoding.(http.Flusher); ok {
		wrappedWriter.flush = f.Flush
	} else if f, ok := encoding.(WriteFlusher); ok {
		wrappedWriter.flush = func() { _ = f.Flush() }
	}

	data.encoding = encoding

	return wrappedWriter, r, true
}
func (mw *encodingMW[R]) Exit(w http.ResponseWriter, r R) {
	data := r.Encoder()
	defer mw.encodingPool.Put(data.wrappedWriter)
	if data.pool != nil {
		defer data.pool.Put(data.encoding)
	}
	if c, ok := data.encoding.(io.Closer); ok && data.encoding != w {
		defer c.Close()
	}

	if data.wrappedWriter.flush != nil {
		defer data.wrappedWriter.flush()
	}
}

func ContentEncoding[R DispatchEncoder](withProviders ...ContentEncoder) dispatch.Middleware[R] {
	gzipPool := &sync.Pool{
		New: func() any {
			return new(gzip.Writer)
		},
	}
	deflatePool := &sync.Pool{
		New: func() any {
			writer, _ := flate.NewWriter(nil, flate.DefaultCompression)
			return writer
		},
	}
	allProviders := map[string]func(w http.ResponseWriter) (io.Writer, *sync.Pool){
		"gzip": func(w http.ResponseWriter) (io.Writer, *sync.Pool) {
			writer := gzipPool.Get().(*gzip.Writer)
			writer.Reset(w)
			return writer, gzipPool
		},
		"deflate": func(w http.ResponseWriter) (io.Writer, *sync.Pool) {
			writer := deflatePool.Get().(*flate.Writer)
			writer.Reset(w)
			return writer, deflatePool
		},
		"identity": func(w http.ResponseWriter) (io.Writer, *sync.Pool) {
			return w, nil
		},
	}
	for _, provider := range withProviders {
		allProviders[provider.Name()] = func(w http.ResponseWriter) (io.Writer, *sync.Pool) {
			encoding := provider.Create(w)
			return encoding, provider.Pool()
		}
	}

	mw := &encodingMW[R]{
		providers: allProviders,
		encodingPool: sync.Pool{
			New: func() any {
				return new(encodingWriter)
			},
		},
	}
	return mw
}

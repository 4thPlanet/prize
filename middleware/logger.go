package middleware

import (
	"bufio"
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"os"
	"regexp"
	"runtime"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"github.com/4thPlanet/dispatch"
)

type writerLog struct {
	http.ResponseWriter
	length int
	code   int
}

func (wl *writerLog) reset(w http.ResponseWriter) {
	wl.ResponseWriter = w
	wl.length = 0
	wl.code = http.StatusOK
}

func (wl *writerLog) Write(out []byte) (int, error) {
	n, err := wl.ResponseWriter.Write(out)
	wl.length += n
	return n, err
}
func (wl *writerLog) WriteHeader(code int) {
	wl.code = code
	wl.ResponseWriter.WriteHeader(code)
}
func (wl *writerLog) Hijack() (net.Conn, *bufio.ReadWriter, error) {
	if hj, ok := wl.ResponseWriter.(http.Hijacker); !ok {
		return nil, nil, errors.New("hijack not supported")
	} else {
		return hj.Hijack()
	}
}
func (wl *writerLog) Flush() {
	if f, ok := wl.ResponseWriter.(http.Flusher); ok {
		f.Flush()
	}
}

type bodySizeReader struct {
	length   atomic.Uint32
	isClosed atomic.Bool
	io.ReadCloser
}

func (bsr *bodySizeReader) reset(rc io.ReadCloser) {
	bsr.length.Store(0)
	bsr.isClosed.Store(false)
	bsr.ReadCloser = rc
}
func (bsr *bodySizeReader) Read(buf []byte) (int, error) {
	if !bsr.isClosed.Load() {
		n, err := bsr.ReadCloser.Read(buf)
		bsr.length.Add(uint32(n))
		return n, err
	}
	return 0, io.ErrClosedPipe
}
func (bsr *bodySizeReader) Close() error {
	bsr.isClosed.Store(true)
	return nil
}

// Creates a log entry using apache-style directives. Any value derived from such a directive will be json-encoded.
// An unrecognized directive will be rendered literally (e.g., %z will be rendered as "%z" and not an empty string)
// See https://httpd.apache.org/docs/2.4/mod/mod_log_config.html#formats for supported directives
var paramDirectiveRegex = regexp.MustCompile(`^\{(\S+)\}(\w)`)
var directiveMap map[byte]func(*http.Request, *writerLog, time.Time, time.Duration) string

func init() {
	directiveMap = map[byte]func(*http.Request, *writerLog, time.Time, time.Duration) string{
		'%': func(r *http.Request, wl *writerLog, t time.Time, _ time.Duration) string { return "%" },
		'a': func(r *http.Request, wl *writerLog, t time.Time, _ time.Duration) string {
			addr := r.RemoteAddr
			if ip, _, err := net.SplitHostPort(addr); err == nil {
				return ip
			}
			return addr
		},
		'A': func(r *http.Request, wl *writerLog, t time.Time, _ time.Duration) string {
			if addr, ok := r.Context().Value(http.LocalAddrContextKey).(net.Addr); ok {
				if ip, _, err := net.SplitHostPort(addr.String()); err == nil {
					return ip
				}
				return addr.String()
			}
			return "-"
		},
		'b': func(r *http.Request, wl *writerLog, t time.Time, _ time.Duration) string {
			if wl.length == 0 {
				return "-"
			}
			return strconv.FormatInt(int64(wl.length), 10)
		},
		'B': func(r *http.Request, wl *writerLog, t time.Time, _ time.Duration) string {
			return strconv.FormatInt(int64(wl.length), 10)
		},
		'D': func(r *http.Request, wl *writerLog, t time.Time, requestDuration time.Duration) string {
			return strconv.FormatInt(requestDuration.Microseconds(), 10)
		},
		'H': func(r *http.Request, wl *writerLog, t time.Time, _ time.Duration) string { return r.Proto },
		'I': func(r *http.Request, wl *writerLog, t time.Time, _ time.Duration) string {
			return strconv.FormatUint(uint64(r.Body.(*bodySizeReader).length.Load()), 10)
		},
		'l': func(r *http.Request, wl *writerLog, t time.Time, _ time.Duration) string { return "-" },
		'm': func(r *http.Request, wl *writerLog, t time.Time, _ time.Duration) string { return r.Method },
		'p': func(r *http.Request, wl *writerLog, t time.Time, _ time.Duration) string {
			host := r.Host
			if _, port, err := net.SplitHostPort(host); err == nil && port != "" {
				return port
			}
			if r.TLS != nil {
				return "443"
			}
			return "80"
		},
		'q': func(r *http.Request, wl *writerLog, t time.Time, _ time.Duration) string {
			if r.URL.RawQuery > "" {
				return "?" + r.URL.RawQuery
			}
			return ""
		},
		'r': func(r *http.Request, wl *writerLog, t time.Time, _ time.Duration) string {
			return r.Method + " " + r.RequestURI + " " + r.Proto
		},
		's': func(r *http.Request, wl *writerLog, t time.Time, _ time.Duration) string {
			return strconv.FormatInt(int64(wl.code), 10)
		},
		't': func(r *http.Request, wl *writerLog, t time.Time, _ time.Duration) string {
			return t.Format("[02/Jan/2006:15:04:05 -0700]")
		},
		'T': func(r *http.Request, wl *writerLog, t time.Time, requestDuration time.Duration) string {
			return strconv.FormatInt(int64(requestDuration.Seconds()), 10)
		},
		'u': func(r *http.Request, wl *writerLog, t time.Time, _ time.Duration) string {
			if user, _, ok := r.BasicAuth(); ok && user != "" {
				return user
			}
			return "-"
		},
		'U': func(r *http.Request, wl *writerLog, t time.Time, _ time.Duration) string { return r.URL.Path },
		'v': func(r *http.Request, wl *writerLog, t time.Time, _ time.Duration) string {
			if host := r.Host; host != "" {
				h, _, err := net.SplitHostPort(host)
				if err == nil {
					return h
				}
				return host
			}
			if name, err := os.Hostname(); err == nil {
				return name
			}
			return "-"
		},
		'X': func(r *http.Request, wl *writerLog, t time.Time, _ time.Duration) string { return "-" },
	}
	directiveMap['h'] = directiveMap['a']
	directiveMap['V'] = directiveMap['v']
}

func logBuilder(format string, r *http.Request, w *writerLog, requestTime time.Time, requestDuration time.Duration) string {

	var sb strings.Builder

	for cdx := 0; cdx < len(format); cdx++ {
		c := format[cdx]
		if c != '%' || cdx+1 == len(format) {
			sb.WriteByte(c)
			continue
		}

		if fn, isset := directiveMap[format[cdx+1]]; isset {
			value, _ := json.Marshal(fn(r, w, requestTime, requestDuration))
			sb.WriteString(string(value[1 : len(value)-1]))
			cdx++
			continue
		}

		paramDirectiveMatch := paramDirectiveRegex.FindStringSubmatchIndex(format[cdx+1:])
		if paramDirectiveMatch != nil {
			param := format[paramDirectiveMatch[2]+cdx+1 : paramDirectiveMatch[3]+cdx+1]
			directive := format[paramDirectiveMatch[4]+cdx+1]
			var value string
			switch directive {
			case 'i':
				value = r.Header.Get(param)
			case 'o':
				value = w.ResponseWriter.Header().Get(param)
			case 't':
				value = requestTime.Format(param)
			case 'C':
				cookie, err := r.Cookie(param)
				if err == nil {
					value = cookie.Value
				}
			case 'e':
				value = os.Getenv(param)
			case 'p':
				switch param {
				case "canonical":
					// used when a reverse proxy is pointed to the server
					// TODO: work out whether this should be worked through headers or a config, or what..
					// for now just use local as a fallback
					value = directiveMap['p'](r, w, requestTime, requestDuration)
				case "local":
					value = directiveMap['p'](r, w, requestTime, requestDuration)
				case "remote":
					if _, port, err := net.SplitHostPort(r.RemoteAddr); err == nil {
						value = port
					}
				default:
					value = "-"
				}
			case 'P':
				// process ID or thread ID of the server serving the request
				switch param {
				case "pid":
					value = strconv.FormatInt(int64(os.Getpid()), 10)
				case "tid", "hextid":
					// best we can do for thread id is the goroutine id...you really shouldn't use this...
					var buf [64]byte
					runtime.Stack(buf[:], false)
					if !bytes.HasPrefix(buf[:], []byte("goroutine ")) {
						value = "0"
					} else {
						id := int64(0)
						for _, digit := range buf[10:] {
							if digit < '0' || digit > '9' {
								break
							}
							id = id*10 + int64(digit-'0')
						}
						if param == "tid" {
							value = strconv.FormatInt(id, 10)
						} else {
							value = strconv.FormatInt(id, 16)
						}
					}
				default:
					value = "-"
				}
			case 'T':
				switch param {
				case "ms":
					value = strconv.FormatInt(requestDuration.Milliseconds(), 10)
				case "us":
					value = directiveMap['D'](r, w, requestTime, requestDuration)
				case "s":
					value = directiveMap['T'](r, w, requestTime, requestDuration)
				default:

				}
			default:
				value = format[paramDirectiveMatch[0]+cdx : paramDirectiveMatch[1]+cdx]
			}
			encoded, _ := json.Marshal(value)
			sb.WriteString(string(encoded[1 : len(encoded)-1]))
			cdx += paramDirectiveMatch[1]
		}

	}

	return sb.String()
}

type LoggerData struct {
	start time.Time
	wl    *writerLog
	bsr   *bodySizeReader
}

type loggerMW[R DispatchLogger] struct {
	format     string
	logger     io.Writer
	writerPool sync.Pool
	bsrPool    sync.Pool
}

func (mw *loggerMW[R]) Enter(w http.ResponseWriter, r R) (http.ResponseWriter, R, bool) {
	data := r.Log()
	data.start = time.Now()
	data.wl = mw.writerPool.Get().(*writerLog)
	data.bsr = mw.bsrPool.Get().(*bodySizeReader)

	data.wl.reset(w)
	data.bsr.reset(r.Request().Body)

	r.Request().Body = data.bsr
	return data.wl, r, true
}
func (mw *loggerMW[R]) Exit(w http.ResponseWriter, r R) {
	data := r.Log()

	defer mw.writerPool.Put(data.wl)
	defer mw.bsrPool.Put(data.bsr)
	defer data.bsr.ReadCloser.Close()

	duration := time.Since(data.start)
	data.bsr.isClosed.Store(false)
	if _, err := io.Copy(io.Discard, data.bsr); err != nil {
		fmt.Fprintf(mw.logger, "Error reading remainder of request body: %v", err)
	}

	fmt.Fprint(mw.logger, logBuilder(mw.format, r.Request(), data.wl, data.start, duration))

}

type DispatchLogger interface {
	dispatch.RequestAdapter
	Log() *LoggerData
}

func Logger[R DispatchLogger](format string, logger io.Writer) dispatch.Middleware[R] {
	mw := &loggerMW[R]{
		format: format,
		logger: logger,
		writerPool: sync.Pool{
			New: func() any {
				return new(writerLog)
			},
		},
		bsrPool: sync.Pool{
			New: func() any {
				return new(bodySizeReader)
			},
		},
	}
	return mw
}

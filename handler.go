package prize

import (
	"io"
	"log"
	"net/http"

	"github.com/4thPlanet/dispatch"
	"github.com/4thPlanet/prize/middleware"
)

type TypedHandler[S middleware.Session, T any] struct {
	*dispatch.TypedHandler[*HandlerData[S, T]]
	logFormat    string
	logger       io.Writer
	errorHandler middleware.ErrorHandler[*HandlerData[S, T]]
	ctn          *dispatch.ContentTypeNegotiator
	encoders     []middleware.ContentEncoder
	sessionStore middleware.SessionStore[S]
}

type HandlerData[S middleware.Session, T any] struct {
	r            *http.Request
	Data         T
	session      S
	logData      *middleware.LoggerData
	encodingData *middleware.EncodingData
}

func (d *HandlerData[S, T]) Request() *http.Request {
	return d.r
}
func (d *HandlerData[S, T]) GetSession() S {
	return d.session
}
func (d *HandlerData[S, T]) SetSession(session S) {
	d.session = session
}
func (d *HandlerData[S, T]) Log() *middleware.LoggerData {
	return d.logData
}

func (d *HandlerData[S, T]) Encoder() *middleware.EncodingData {
	return d.encodingData
}

func NewTypedHandler[S middleware.Session, T any](fn func(*http.Request) T) *TypedHandler[S, T] {
	mux := new(TypedHandler[S, T])
	mux.TypedHandler = dispatch.NewTypedHandler(func(r *http.Request) *HandlerData[S, T] {
		data := new(HandlerData[S, T])
		data.r = r
		data.logData = new(middleware.LoggerData)
		data.encodingData = new(middleware.EncodingData)
		data.Data = fn(r)
		return data
	})
	mux.logFormat = "%h %l %u %t \"%r\" %s %b\n"
	mux.logger = log.Default().Writer()
	mux.errorHandler = middleware.Errors[*HandlerData[S, T], int]()
	mux.sessionStore = middleware.NewDefaultSessionStore[S]("sid")
	return mux
}

func (mux *TypedHandler[S, T]) UseLog(format string, l io.Writer) {
	mux.logFormat = format
	mux.logger = l
}

func (mux *TypedHandler[S, T]) UseErrorHandler(handler middleware.ErrorHandler[*HandlerData[S, T]], ctn *dispatch.ContentTypeNegotiator) {
	mux.errorHandler = handler
	mux.ctn = ctn
}

func (mux *TypedHandler[S, T]) UseContentEncoders(encs ...middleware.ContentEncoder) {
	mux.encoders = encs
}

func (mux *TypedHandler[S, T]) UseSessionStore(store middleware.SessionStore[S]) {
	mux.sessionStore = store
}

func (mux *TypedHandler[S, T]) UseMiddleware(mws ...dispatch.Middleware[*HandlerData[S, T]]) {
	// prepend to mws Logger, ErrorHandling, Session, ContentEncoding

	wrappers := []dispatch.Middleware[*HandlerData[S, T]]{
		middleware.Logger[*HandlerData[S, T]](mux.logFormat, mux.logger),
		//		middleware.Errors[*HandlerData[S, T], int](mux.ctn, mux.logger),
		mux.errorHandler.Self()(mux.ctn, mux.logger),
		middleware.SessionMW[S, *HandlerData[S, T]](mux.sessionStore, mux.logger),
		middleware.ContentEncoding[*HandlerData[S, T]](mux.encoders...),
	}

	mux.TypedHandler.UseMiddleware(append(wrappers, mws...)...)

}

package middleware

import (
	"fmt"
	"io"
	"net/http"
	"runtime/debug"

	"github.com/4thPlanet/dispatch"
)

type ErrorHandler[R dispatch.RequestAdapter] interface {
	isErrorHandler() errorHandlerMarker
	Self() func(ctn *dispatch.ContentTypeNegotiator, logger io.Writer) dispatch.Middleware[R]
}

type errorHandler[R dispatch.RequestAdapter, E ~int] func(ctn *dispatch.ContentTypeNegotiator, logger io.Writer) dispatch.Middleware[R]
type errorHandlerMarker struct{}

func (err errorHandler[R, E]) isErrorHandler() errorHandlerMarker {
	return errorHandlerMarker{}
}
func (err errorHandler[R, E]) Self() func(ctn *dispatch.ContentTypeNegotiator, logger io.Writer) dispatch.Middleware[R] {
	return err
}

type errorMW[R dispatch.RequestAdapter, E ~int] struct {
	logger  io.Writer
	handler func(http.ResponseWriter, R)
}

func (mw *errorMW[R, E]) Enter(w http.ResponseWriter, r R) (http.ResponseWriter, R, bool) {
	return w, r, true
}
func (mw *errorMW[R, E]) Exit(w http.ResponseWriter, r R) {
	if re := recover(); re != nil {
		fmt.Fprintf(mw.logger, "Recovering from panic! %v", re)
		fmt.Fprintf(mw.logger, "Stack: %s", debug.Stack())
		if mw.handler == nil {
			w.WriteHeader(http.StatusInternalServerError)
		} else {
			mw.handler(w, r)
		}
	}
}

// Factory function to create an error handler middleware
func Errors[R dispatch.RequestAdapter, E ~int]() errorHandler[R, E] {

	return func(ctn *dispatch.ContentTypeNegotiator, logger io.Writer) dispatch.Middleware[R] {
		var errorContentTypeHandler dispatch.ContentTypeHandler[R, *E] = func(r R) (*E, error) {
			return new(E(http.StatusInternalServerError)), nil
		}

		mw := &errorMW[R, E]{
			logger: logger,
		}
		if ctn != nil {
			mw.handler = errorContentTypeHandler.AsTypedHandler(ctn, logger)
		}
		return mw
	}

}

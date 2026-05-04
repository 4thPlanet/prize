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

// Factory function to create an error handler middleware
func Errors[R dispatch.RequestAdapter, E ~int]() errorHandler[R, E] {

	return func(ctn *dispatch.ContentTypeNegotiator, logger io.Writer) dispatch.Middleware[R] {
		var errorContentTypeHandler dispatch.ContentTypeHandler[R, *E] = func(r R) (*E, error) {
			return new(E(http.StatusInternalServerError)), nil
		}

		errorHandler := errorContentTypeHandler.AsTypedHandler(ctn, logger)

		handlePanic := func(w http.ResponseWriter, r R) {
			if re := recover(); re != nil {

				fmt.Fprintf(logger, "Recovering from panic! %v", re)
				fmt.Fprintf(logger, "Stack: %s", debug.Stack())
				if ctn == nil {
					w.WriteHeader(http.StatusInternalServerError)
				} else {
					errorHandler(w, r)
				}
			}
		}

		return func(w http.ResponseWriter, r R, next dispatch.Middleware[R]) {
			defer handlePanic(w, r)
			next(w, r, next)
		}
	}

}

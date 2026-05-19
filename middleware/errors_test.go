package middleware

import (
	"io"
	"net/http"
	"net/http/httptest"
	"reflect"
	"testing"

	"github.com/4thPlanet/dispatch"
)

type ErrorPage int

func (page *ErrorPage) Text(w http.ResponseWriter) error {
	code := int(*page)
	w.WriteHeader(code)
	w.Write([]byte(http.StatusText(code)))
	return nil
}

type TextOutputer interface {
	Text(w http.ResponseWriter) error
}

func TestErrors(t *testing.T) {

	ctn := dispatch.NewContentTypeNegotiator()
	dispatch.RegisterImplementationToNegotiator[TextOutputer](ctn, "text/plain")

	for _, test := range []struct {
		Name     string
		Handler  dispatch.Middleware[*mockRequest]
		Panics   bool
		Response []byte
	}{
		{"basic", Errors[*mockRequest, int]()(nil, io.Discard), false, testBody},
		{"basic-panic", Errors[*mockRequest, int]()(nil, io.Discard), true, nil},
		{"custom", Errors[*mockRequest, ErrorPage]()(ctn, io.Discard), false, testBody},
		{"custom-panic", Errors[*mockRequest, ErrorPage]()(ctn, io.Discard), true, []byte(http.StatusText(http.StatusInternalServerError))},
	} {
		t.Run(test.Name, func(t *testing.T) {
			handler := dispatch.NewTypedHandler(func(r *http.Request) *mockRequest {
				return &mockRequest{r: r}
			})
			handler.HandleFunc("/", func(w http.ResponseWriter, r *mockRequest) {
				if test.Panics {
					var n *int
					*n = *n + 5
				} else {
					w.Write(testBody)
				}

			})
			handler.UseMiddleware(test.Handler)
			req := httptest.NewRequest(http.MethodGet, "/", nil)
			res := httptest.NewRecorder()
			handler.ServeHTTP(res, req)

			expectedCode := http.StatusOK
			if test.Panics {
				expectedCode = http.StatusInternalServerError
			}
			if got, want := res.Code, expectedCode; got != want {
				t.Errorf("Unexpected response code. Got: %v, Want: %v", got, want)
			}

			if got, want := res.Body.Bytes(), test.Response; !reflect.DeepEqual(got, want) {
				t.Errorf("Unexpected response body. Got: %s, Want: %s", got, want)
			}
		})
	}

}

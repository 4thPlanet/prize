package middleware

import (
	"bytes"
	"compress/flate"
	"compress/gzip"
	"encoding/base64"
	"io"
	"net/http"
	"net/http/httptest"
	"reflect"
	"testing"

	"github.com/4thPlanet/dispatch"
)

type CustomEncoder struct{}

func (enc CustomEncoder) Name() string { return "Custom Encoder" }

type b64 struct {
	io.Writer
}

func (w *b64) Write(data []byte) (int, error) {
	encoder := base64.NewEncoder(base64.StdEncoding, w.Writer)
	defer encoder.Close()
	return encoder.Write(data)
}
func (enc CustomEncoder) Create(in io.Writer) io.Writer {
	out := new(b64)
	out.Writer = in
	return out
}

func TestEncoding(t *testing.T) {

	for _, test := range []struct {
		Name              string
		WithCustomEncoder bool
	}{
		{"baseEncoder", false}, {"withCustomEncoder", true},
	} {
		t.Run(test.Name, func(t *testing.T) {
			var encoder dispatch.Middleware[*mockRequest]
			if test.WithCustomEncoder {
				encoder = ContentEncoding[*mockRequest](CustomEncoder{})
			} else {
				encoder = ContentEncoding[*mockRequest]()
			}
			customEncoderResponseCode := http.StatusNotAcceptable
			if test.WithCustomEncoder {
				customEncoderResponseCode = http.StatusOK
			}
			for _, test := range []struct {
				AcceptEncoding   string
				ExpectedEncoding string
				Code             int
				Decoder          func(*bytes.Buffer) ([]byte, error)
			}{
				{"", "identity", http.StatusOK, nil},
				{"identity", "identity", http.StatusOK, nil},
				{"gzip", "gzip", http.StatusOK, func(body *bytes.Buffer) ([]byte, error) {
					if r, err := gzip.NewReader(body); err != nil {
						return nil, err
					} else {
						return io.ReadAll(r)
					}

				}},
				{"deflate", "deflate", http.StatusOK, func(body *bytes.Buffer) ([]byte, error) {
					return io.ReadAll(flate.NewReader(body))
				}},
				{"gzip;q=0.8,deflate;q=0.9", "deflate", http.StatusOK, func(body *bytes.Buffer) ([]byte, error) {
					return io.ReadAll(flate.NewReader(body))
				}},
				{"gzip;q=0.8,identity;q=0.9", "identity", http.StatusOK, nil},
				{"Custom Encoder", "Custom Encoder", customEncoderResponseCode, func(body *bytes.Buffer) ([]byte, error) {
					// read 4 bytes at a time for one letter - our custom encoder does not handle flushing at all
					out := make([]byte, body.Len()/4)
					decode := [4]byte{}
					for odx := range out {
						body.Read(decode[:])
						if _, err := base64.StdEncoding.Decode(out[odx:], decode[:]); err != nil {
							return nil, err
						}
					}
					return out, nil
				}},
			} {
				t.Run(test.AcceptEncoding, func(t *testing.T) {
					req := httptest.NewRequest(http.MethodGet, "/", nil)
					req.Header.Set("Accept-Encoding", test.AcceptEncoding)
					res := httptest.NewRecorder()
					encoder(res, &mockRequest{r: req}, func(w http.ResponseWriter, r *mockRequest, _ dispatch.Middleware[*mockRequest]) {
						w.Header().Set("Transfer-Encoding", "chunked")
						for _, b := range testBody {
							w.Write([]byte{b})
							w.(http.Flusher).Flush()
						}
					})

					if got, want := res.Code, test.Code; got != want {
						t.Errorf("Unexpected response code. Got: %v, Want: %v", got, want)
					}
					if res.Code == http.StatusOK {
						if got, want := res.Header().Get("Content-Encoding"), test.ExpectedEncoding; got != want {
							t.Errorf("Unexpected Content-Encoding header. Got: %v, Want: %v", got, want)
						}

						var body []byte
						var err error
						if test.Decoder != nil {
							body, err = test.Decoder(res.Body)
							if err != nil {
								t.Errorf("Error decoding response body: %v", err)
							}
						} else {
							body, err = io.ReadAll(res.Body)
							if err != nil {
								t.Errorf("Error reading response body: %v", err)
							}
						}

						if got, want := body, testBody; !reflect.DeepEqual(got, want) {
							t.Errorf("Unexpected body after decoding. Got: %s, Want: %s", got, want)
						}

					}

				})
			}
		})
	}

}

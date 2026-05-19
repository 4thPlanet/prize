package middleware

import (
	"io"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/4thPlanet/dispatch"
)

const testSessionCookieName = "sid"

func TestDefaultSessionStore(t *testing.T) {

	store := NewDefaultSessionStore[*testSession](testSessionCookieName)
	// req1: brand new session
	req1 := httptest.NewRequest(http.MethodGet, "/", nil)
	res := httptest.NewRecorder()
	session := store.Load(req1)
	if got, want := session.Num, 0; got != want {
		t.Errorf("Unexpected initial Num value. Got %v, Want %v", got, want)
	}
	session.Num = 10
	if err := store.StoreSession(session); err != nil {
		t.Fatalf("Unable to store session: %v", err)
	}
	store.WriteCookie(res, session)
	cookies := res.Result().Cookies()
	if got, want := len(cookies), 1; got != want {
		t.Fatalf("Unexpected number of cookies returned. Got %v, Want %v", got, want)
	}
	if got, want := cookies[0].Name, testSessionCookieName; got != want {
		t.Errorf("Unexpected session cookie name. Got %v, Want %v", got, want)
	}
	if got, want := cookies[0].Value, session.Id(); got != want {
		t.Errorf("Unexpected session cookie value. Got %v, Want %v", got, want)
	}

	// req2: Use cookie from req1
	req2 := httptest.NewRequest(http.MethodGet, "/", nil)
	req2.AddCookie(cookies[0])
	session2 := store.Load(req2)
	if got, want := session2.Num, session.Num; got != want {
		t.Errorf("Unexpected session Num returned. Got %v, Want %v", got, want)
	}
}
func TestSessionMW(t *testing.T) {
	sessionRecorder := SessionMW[*testSession, *mockRequest](NewDefaultSessionStore[*testSession]("sid"), io.Discard)
	var num int
	handler := dispatch.NewTypedHandler(func(r *http.Request) *mockRequest {
		return &mockRequest{
			r: r,
		}
	})
	handler.HandleFunc("/", func(w http.ResponseWriter, r *mockRequest) {
		w.Write(testBody)
		r.S.Num++
		if got, want := r.S.Num, num+1; got != want {
			t.Errorf("Unexpected session Num. Got %v, Want %v", got, want)
		}
	})
	handler.UseMiddleware(sessionRecorder)

	var cookie *http.Cookie
	for num = range 5 {
		t.Logf("Num = %d", num)
		req := httptest.NewRequest(http.MethodGet, "/", nil)
		if cookie != nil {
			req.AddCookie(cookie)
		}
		res := httptest.NewRecorder()
		handler.ServeHTTP(res, req)
		cookies := res.Result().Cookies()
		if got, want := len(cookies), 1; got != want {
			t.Fatalf("Unexpected number of cookies returned. Got %v, Want %v", got, want)
		}
		if cookie != nil {
			if got, want := cookies[0].Name, cookie.Name; got != want {
				t.Errorf("Unexpected cookie name returned. Got %v, Want %v", got, want)
			}
			if got, want := cookies[0].Value, cookie.Value; got != want {
				t.Errorf("Unexpected cookie value returned. Got %v, Want %v", got, want)
			}
		} else {
			cookie = cookies[0]
		}
	}
}

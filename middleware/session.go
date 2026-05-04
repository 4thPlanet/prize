package middleware

import (
	"crypto/rand"
	"encoding/base64"
	"fmt"
	"io"
	"net/http"
	"sync"

	"github.com/4thPlanet/dispatch"
)

type SessionAdapter[S Session] interface {
	dispatch.RequestAdapter
	LoadSession(S)
}

type Session interface {
	Id() string
}

type SessionStore[S Session] interface {
	GetSession(*http.Request, func() S) S
	StoreSession(S) error
	WriteCookie(http.ResponseWriter, S)
}

type DefaultSessionStore[S Session] struct{ sync.Map }

func (store *DefaultSessionStore[S]) GetSession(r *http.Request, init func() S) S {
	sessionCookie, err := r.Cookie("session_id")
	var sessionId string
	if err != nil {
		var buf [12]byte
		rand.Read(buf[:])
		var b64 [16]byte
		base64.StdEncoding.Encode(b64[:], buf[:])
		sessionId = string(b64[:])
	} else {
		sessionId = sessionCookie.Value
	}

	// Does session exist in cache?
	if session, ok := store.Map.Load(sessionId); ok {
		return session.(S)
	} else {
		return init()
	}
}
func (store *DefaultSessionStore[S]) StoreSession(s S) error {
	store.Map.Store(s.Id(), s)
	return nil
}
func (store *DefaultSessionStore[S]) WriteCookie(w http.ResponseWriter, s S) {
	c := http.Cookie{
		Name:  "session_id",
		Value: s.Id(),
	}
	http.SetCookie(w, &c)
}

func SessionMW[S Session, R SessionAdapter[S]](store SessionStore[S], init func() S, log io.Writer) dispatch.Middleware[R] {
	return func(w http.ResponseWriter, r R, next dispatch.Middleware[R]) {
		session := store.GetSession(r.Request(), init)
		r.LoadSession(session)
		next(w, r, next)
		if err := store.StoreSession(session); err != nil {
			fmt.Fprintf(log, "Error storing session: %v", err)
		}
		store.WriteCookie(w, session)
	}
}

package middleware

import (
	"crypto/rand"
	"fmt"
	"io"
	"net/http"
	"reflect"
	"sync"

	"github.com/4thPlanet/dispatch"
)

type Session interface {
	Id() string
}

type SessionStore[S Session] interface {
	Load(*http.Request) S
	StoreSession(S) error
	WriteCookie(http.ResponseWriter, S)
}

type DefaultSessionStore[S Session] struct {
	CookieName  string
	sessionType reflect.Type
	sessionKind reflect.Kind
	ptrBuilder  func() reflect.Value
	sync.Map
}

func NewDefaultSessionStore[S Session](cookieName string) *DefaultSessionStore[S] {
	store := new(DefaultSessionStore[S])
	store.CookieName = cookieName
	store.sessionType = reflect.TypeFor[S]()
	store.sessionKind = store.sessionType.Kind()
	if store.sessionKind == reflect.Pointer {
		store.ptrBuilder = zeroValueBuilder(store.sessionType)
	}

	return store
}

func zeroValueBuilder(r reflect.Type) func() reflect.Value {
	switch r.Kind() {
	case reflect.Pointer:
		// create an init for the elem
		elemFn := zeroValueBuilder(r.Elem())
		return func() reflect.Value {
			ptr := reflect.New(r.Elem())
			ptr.Elem().Set(elemFn())
			return ptr
		}
	case reflect.Map:
		return func() reflect.Value {
			return reflect.MakeMap(r)
		}
	case reflect.Slice:
		return func() reflect.Value {
			return reflect.MakeSlice(r, 0, 0)
		}
	default:
		return func() reflect.Value {
			return reflect.Zero(r)
		}
	}
}

// creates a safe zero-value for sessions. reflection has been optimized out as much as possible, but it's still needed for some parts
func (store *DefaultSessionStore[S]) zeroValue() S {
	switch store.sessionKind {
	case reflect.Pointer:
		return store.ptrBuilder().Interface().(S)
	case reflect.Map:
		return reflect.MakeMap(store.sessionType).Interface().(S)
	case reflect.Slice:
		return reflect.MakeSlice(store.sessionType, 0, 0).Interface().(S)
	default:
		var z S
		return z
	}
}

func (store *DefaultSessionStore[S]) Load(r *http.Request) S {
	cookie, _ := r.Cookie(store.CookieName)
	var sessionId string

	if cookie != nil {
		// read session id from session
		sessionId = cookie.Value
	} else {
		// fresh session
		sessionId = rand.Text()
	}
	var session = store.zeroValue()

	// Does session exist in cache?
	if stored, ok := store.Map.Load(sessionId); ok {
		return stored.(S)
	}
	return session
}
func (store *DefaultSessionStore[S]) StoreSession(s S) error {
	store.Map.Store(s.Id(), s)
	return nil
}
func (store *DefaultSessionStore[S]) WriteCookie(w http.ResponseWriter, s S) {
	c := http.Cookie{
		Name:  store.CookieName,
		Value: s.Id(),
	}
	http.SetCookie(w, &c)
}

type SessionAdapter[S Session] interface {
	dispatch.RequestAdapter
	GetSession() S
	SetSession(S)
}

type sessionMW[S Session, R SessionAdapter[S]] struct {
	log   io.Writer
	store SessionStore[S]
}

func (mw *sessionMW[S, R]) Enter(w http.ResponseWriter, r R) (http.ResponseWriter, R, bool) {
	session := mw.store.Load(r.Request())
	r.SetSession(session)
	mw.store.WriteCookie(w, session)
	return w, r, true
}
func (mw *sessionMW[S, R]) Exit(w http.ResponseWriter, r R) {
	session := r.GetSession()
	if err := mw.store.StoreSession(session); err != nil {
		fmt.Fprintf(mw.log, "Error storing session: %v", err)
	}
}

func SessionMW[S Session, R SessionAdapter[S]](store SessionStore[S], log io.Writer) dispatch.Middleware[R] {
	return &sessionMW[S, R]{
		log:   log,
		store: store,
	}
}

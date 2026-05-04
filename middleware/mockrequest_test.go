package middleware

import (
	"crypto/rand"
	"net/http"
)

// types and global vars used by multiple middleware unit tests

type mockRequest struct {
	r *http.Request
	S *testSession
}

func (r *mockRequest) Request() *http.Request {
	return r.r
}
func (r *mockRequest) LoadSession(s *testSession) {
	r.S = s
}

type testSession struct {
	id  string
	Num int
}

func (s *testSession) Id() string {
	if s.id == "" {
		s.id = rand.Text()
	}
	return s.id
}

var initFunc = func() *testSession { return new(testSession) }

var testBody = []byte("Hello, World! This is test content for encoding.")

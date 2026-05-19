package middleware

import (
	"crypto/rand"
	"net/http"
)

// types and global vars used by multiple middleware unit tests

type mockRequest struct {
	r            *http.Request
	S            *testSession
	logData      *LoggerData
	encodingData *EncodingData
}

func (r *mockRequest) Request() *http.Request {
	return r.r
}
func (r *mockRequest) GetSession() *testSession {
	return r.S
}
func (r *mockRequest) SetSession(s *testSession) {
	r.S = s
}
func (r *mockRequest) Log() *LoggerData {
	if r.logData == nil {
		r.logData = new(LoggerData)
	}
	return r.logData
}
func (r *mockRequest) Encoder() *EncodingData {
	if r.encodingData == nil {
		r.encodingData = new(EncodingData)
	}
	return r.encodingData
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

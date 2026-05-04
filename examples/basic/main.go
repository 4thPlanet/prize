package main

import (
	"context"
	"crypto/rand"
	"encoding/base64"
	"encoding/csv"
	"encoding/json"
	"html"
	"log"
	"net"
	"net/http"
	"os"
	"os/signal"
	"slices"
	"strconv"
	"syscall"
	"time"

	"github.com/4thPlanet/dispatch"
	"github.com/4thPlanet/prize"
	"github.com/4thPlanet/prize/middleware"
)

type Session map[string]any

func (s Session) Id() string {
	if id, isset := s["id"]; isset {
		return id.(string)
	}

	var buf [12]byte
	rand.Read(buf[:])
	s["id"] = base64.StdEncoding.EncodeToString(buf[:])
	return s["id"].(string)
}
func (s Session) Load(r *http.Request) {}
func (s Session) Save(r *http.Request) {}

type Request struct {
	r    *http.Request
	User *User
}

func (r *Request) Request() *http.Request {
	return r.r
}

type User struct {
	Username  string
	Password  string `json:"-"`
	Birthdate time.Time
}

type Users []User

func (u Users) Html(w http.ResponseWriter) error {
	if _, err := w.Write([]byte(`<!DOCTYPE html>
<html>
	<head>
		<title>Users</title>
	</head>
	<body>
		<table>
			<thead>
				<tr>
					<th>Username</th>
					<th>Birthdate</th>
				</tr>
			</thead>
			<tbody>
	`)); err != nil {
		return err
	}
	for _, user := range u {
		w.Write([]byte(`
				<tr>
					<td>` + html.EscapeString(user.Username) + `</td>
					<td>` + user.Birthdate.Format(time.RFC3339) + `</td>
				</tr>
		`))
	}
	w.Write([]byte(`
			</tbody>
		</table>
	</body>
</html>`))

	return nil
}
func (u Users) Json(w http.ResponseWriter) error {
	b, err := json.Marshal(u)
	if err != nil {
		return err
	}
	_, err = w.Write(b)
	return err
}
func (u Users) Csv(w http.ResponseWriter) error {
	csv := csv.NewWriter(w)
	defer csv.Flush()
	if err := csv.Write([]string{"Username", "Birthdate"}); err != nil {
		return err
	}
	for _, user := range u {
		if err := csv.Write([]string{user.Username, user.Birthdate.Format(time.RFC3339)}); err != nil {
			return err
		}
	}
	return nil
}

type CsvOutputer interface {
	Csv(http.ResponseWriter) error
}
type JsonOutputer interface {
	Json(http.ResponseWriter) error
}
type HtmlOutputer interface {
	Html(http.ResponseWriter) error
}

type ErrorPage int

func (err *ErrorPage) Html(w http.ResponseWriter) error {
	code := int(*err)
	w.WriteHeader(code)
	w.Write([]byte(`<!DOCTYPE html>
<html>
	<head></head>
	<body>
		<h1>` + strconv.Itoa(code) + `</h1>
		<p>` + http.StatusText(code) + `</p>
	</body>
</html>
	`))
	return nil
}

func must(err error) {
	if err != nil {
		panic("unexpected error: " + err.Error())
	}
}

func main() {

	var ctn = dispatch.NewContentTypeNegotiator()
	must(dispatch.RegisterImplementationToNegotiator[CsvOutputer](ctn, "text/csv"))
	must(dispatch.RegisterImplementationToNegotiator[HtmlOutputer](ctn, "text/html"))
	must(dispatch.RegisterImplementationToNegotiator[JsonOutputer](ctn, "application/json"))

	server := dispatch.NewServer()

	mux := prize.NewTypedHandler[Session](func() Session {
		return make(Session)
	}, func(r *http.Request) *Request {
		req := new(Request)
		req.r = r
		return req
	})
	mux.UseErrorHandler(middleware.Errors[*prize.HandlerData[Session, *Request], ErrorPage](), ctn)

	var users = make(Users, 1)
	users[0] = User{
		Username:  "admin",
		Password:  "superpass",
		Birthdate: time.Unix(0, 0),
	}
	mux.UseMiddleware(
		func(w http.ResponseWriter, r *prize.HandlerData[Session, *Request], next dispatch.Middleware[*prize.HandlerData[Session, *Request]]) {
			// BasicAuth
			username, password, ok := r.Request().BasicAuth()
			if ok {
				// Does this username exist?
				if udx := slices.IndexFunc(users, func(user User) bool {
					return user.Username == username && user.Password == password
				}); udx > -1 {
					r.Data.User = &users[udx]
				} else {
					w.WriteHeader(http.StatusUnauthorized)
					return
				}

			}
			next(w, r, next)
		},
	)

	var userListFunc dispatch.ContentTypeHandler[*prize.HandlerData[Session, *Request], Users] = func(r *prize.HandlerData[Session, *Request]) (Users, error) {
		return users, nil
	}
	mux.HandleFunc("GET /{$}", userListFunc.AsTypedHandler(ctn, log.Default().Writer()))
	mux.HandleFunc("POST /{$}", func(w http.ResponseWriter, r *prize.HandlerData[Session, *Request]) {
		// Confirm user logged in first, then add to list of users
		if r.Data.User == nil {
			w.WriteHeader(http.StatusUnauthorized)
			return
		}
		// parse the post body
		defer r.Request().Body.Close()
		r.Request().ParseForm()
		values := r.Request().PostForm
		username := values.Get("username")
		password := values.Get("password")
		dob := values.Get("birthdate")
		birthdate, err := time.Parse(time.RFC3339, dob)
		if username == "" || password == "" || dob == "" || err != nil {
			w.WriteHeader(http.StatusBadRequest)
			return
		}

		newuser := User{
			Username:  username,
			Password:  password,
			Birthdate: birthdate,
		}

		users = append(users, newuser)
	})
	mux.HandleFunc("/panic", func(w http.ResponseWriter, r *prize.HandlerData[Session, *Request]) {
		// This handler intentionally panics
		panic("triggered panic")
	})

	server.Handle("/", mux)

	listener, err := net.Listen("tcp", "localhost:8080")
	if err != nil {
		log.Fatalf("Unable to listen on socket: %v", err)
	}

	c := make(chan os.Signal, 1)
	signal.Notify(c, syscall.SIGINT, syscall.SIGTERM)
	go func() {
		<-c
		ctx, cancel := context.WithTimeout(context.Background(), time.Second)
		defer cancel()
		if err := server.Shutdown(ctx); err != nil {
			log.Printf("Error performing graceful shutdown: %v", err)
		}
		listener.Close()
		os.Exit(0)
	}()
	if err := server.Serve(listener); err != http.ErrServerClosed {
		log.Fatalf("Error serving: %v", err)
	}
	select {}

}

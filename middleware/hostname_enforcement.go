package middleware

import (
	"net/http"
	"net/url"
	"strings"

	"github.com/4thPlanet/dispatch"
)

func HostnameEnforcement[R dispatch.RequestAdapter](hostname string) dispatch.Middleware[R] {
	const panicRequiresMsg = "hostname enforcement requires a bare host[:port], no scheme, path, fragment, query, or user info"
	if hostname == "" {
		panic("hostname is empty")
	}
	if strings.Contains(hostname, "/") {
		panic(panicRequiresMsg)
	}
	if u, err := url.Parse("http://" + hostname); err != nil {
		panic("invalid hostname: " + err.Error())
	} else if u.Host != hostname {
		panic(panicRequiresMsg)
	}

	return hostnameEnforcement[R](hostname)
}

type hostnameEnforcement[R dispatch.RequestAdapter] string

func (h hostnameEnforcement[R]) Enter(w http.ResponseWriter, r R) (http.ResponseWriter, R, bool) {
	req := r.Request()
	want := string(h)

	if !strings.EqualFold(req.Host, want) {
		target := *req.URL
		target.Host = want
		if req.TLS != nil {
			target.Scheme = "https"
		} else {
			target.Scheme = "http"
		}
		http.Redirect(w, req, target.String(), http.StatusPermanentRedirect)
		return w, r, false
	}
	return w, r, true
}
func (h hostnameEnforcement[R]) Exit(w http.ResponseWriter, r R) {}

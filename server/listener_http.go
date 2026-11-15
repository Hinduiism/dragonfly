package server

import (
	"crypto/tls"
	"errors"
	"log/slog"
	"net"
	"net/http"
	"time"

	"github.com/soheilhy/cmux"
)

// serveNetherNetHTTP serves signaling over HTTP or HTTPS. With TLS enabled,
// permitted plaintext HTTP requests redirect to the matching HTTPS URL.
// Wrapping the TLS sublistener, rather than hiding TLS inside a generic Conn,
// lets net/http perform the handshake and populate Request.TLS normally.
func serveNetherNetHTTP(srv *http.Server, tcp net.Listener, tlsConfig *tls.Config, allowHTTP bool, log *slog.Logger) {
	serve := func(l net.Listener) {
		go func() {
			if err := srv.Serve(l); !netherNetHTTPClosed(err) {
				log.Error("NetherNet HTTP listener closed unexpectedly.", "error", err)
			}
		}()
	}
	if tlsConfig == nil {
		serve(tcp)
		return
	}
	if !allowHTTP {
		serve(tls.NewListener(tcp, tlsConfig))
		return
	}

	handler := srv.Handler
	if handler == nil {
		handler = http.DefaultServeMux
	}
	srv.Handler = http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.TLS == nil {
			target := *r.URL
			target.Scheme = "https"
			target.Host = r.Host
			// A 308 preserves the method and SDP body when the client follows it.
			w.Header().Set("Connection", "close")
			http.Redirect(w, r, target.String(), http.StatusPermanentRedirect)
			return
		}
		handler.ServeHTTP(w, r)
	})

	mux := cmux.New(tcp)
	// A silent connection must neither block other clients nor remain open
	// indefinitely while the mux waits to identify its protocol.
	timeout := srv.ReadHeaderTimeout
	if timeout <= 0 {
		timeout = 5 * time.Second
	}
	mux.SetReadTimeout(timeout)
	secure := tls.NewListener(mux.Match(cmux.TLS()), tlsConfig)
	plain := mux.Match(cmux.HTTP1Fast())
	serve(secure)
	serve(plain)
	go func() {
		if err := mux.Serve(); !netherNetHTTPClosed(err) {
			log.Error("NetherNet signaling multiplexer closed unexpectedly.", "error", err)
		}
	}()
}

func netherNetHTTPClosed(err error) bool {
	return err == nil || errors.Is(err, http.ErrServerClosed) || errors.Is(err, net.ErrClosed) ||
		errors.Is(err, cmux.ErrListenerClosed) || errors.Is(err, cmux.ErrServerClosed)
}

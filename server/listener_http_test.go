package server

import (
	"crypto/tls"
	"crypto/x509"
	"io"
	"log"
	"log/slog"
	"net"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

func TestNetherNetSignalingTransports(t *testing.T) {
	certificateServer := httptest.NewTLSServer(http.NotFoundHandler())
	certificate := certificateServer.TLS.Certificates[0]
	roots := x509.NewCertPool()
	roots.AddCert(certificateServer.Certificate())
	certificateServer.Close()

	for _, tc := range []struct {
		name      string
		tls       bool
		allowHTTP bool
	}{
		{name: "HTTP only"},
		{name: "HTTPS only", tls: true},
		{name: "HTTP redirects to HTTPS", tls: true, allowHTTP: true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			tcp, err := net.Listen("tcp", "127.0.0.1:0")
			if err != nil {
				t.Fatal(err)
			}
			t.Cleanup(func() { _ = tcp.Close() })
			var config *tls.Config
			if tc.tls {
				config = &tls.Config{Certificates: []tls.Certificate{certificate}, MinVersion: tls.VersionTLS12}
			}
			const query = "version=1.26.60&id=123&name=Bedrock%20test&path=%2Fjoin"
			server := &http.Server{
				ReadHeaderTimeout: 500 * time.Millisecond,
				ErrorLog:          log.New(io.Discard, "", 0),
				Handler: http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
					validRoute := r.Method == http.MethodGet && r.URL.Path == "/v1/join" ||
						r.Method == http.MethodPost && r.URL.Path == "/v1/join/123"
					if !validRoute || r.URL.RawQuery != query {
						http.Error(w, "wrong signaling request", http.StatusBadRequest)
						return
					}
					w.Header().Set("X-Server-Identity", "same-operator")
					if r.TLS != nil {
						w.Header().Set("X-Secure", "true")
					}
					_, _ = io.Copy(w, r.Body)
				}),
			}
			t.Cleanup(func() { _ = server.Close() })
			serveNetherNetHTTP(server, tcp, config, tc.allowHTTP, slog.New(slog.NewTextHandler(io.Discard, nil)))

			// A client that sends nothing must not stall classification of other
			// connections, and must eventually be dropped by the header deadline.
			idle, err := net.DialTimeout("tcp", tcp.Addr().String(), time.Second)
			if err != nil {
				t.Fatal(err)
			}
			defer idle.Close()
			transport := &http.Transport{TLSClientConfig: &tls.Config{RootCAs: roots, MinVersion: tls.VersionTLS12}}
			defer transport.CloseIdleConnections()
			client := &http.Client{Transport: transport, Timeout: 3 * time.Second}
			for _, request := range []struct {
				method, path, body string
			}{
				{method: http.MethodGet, path: "/v1/join"},
				{method: http.MethodPost, path: "/v1/join/%31%32%33", body: "v=0\r\na=identity:unchanged\r\n"},
			} {
				if tc.tls && tc.allowHTTP {
					noRedirect := *client
					noRedirect.CheckRedirect = func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }
					req, err := http.NewRequest(request.method, "http://"+tcp.Addr().String()+request.path+"?"+query, strings.NewReader(request.body))
					if err != nil {
						t.Fatal(err)
					}
					response, err := noRedirect.Do(req)
					if err != nil {
						t.Fatal(err)
					}
					response.Body.Close()
					want := "https://" + tcp.Addr().String() + request.path + "?" + query
					if response.StatusCode != http.StatusPermanentRedirect || response.Header.Get("Location") != want {
						t.Fatalf("%s redirect: status=%d location=%q, want 308 to %q", request.method, response.StatusCode, response.Header.Get("Location"), want)
					}
				}
				for _, scheme := range []string{"http", "https"} {
					if scheme == "https" && !tc.tls {
						continue
					}
					req, err := http.NewRequest(request.method, scheme+"://"+tcp.Addr().String()+request.path+"?"+query, strings.NewReader(request.body))
					if err != nil {
						t.Fatal(err)
					}
					if request.method == http.MethodPost {
						req.Header.Set("Content-Type", "application/sdp")
					}
					response, err := client.Do(req)
					if err != nil {
						t.Fatalf("%s %s signaling: %v", scheme, request.method, err)
					}
					got, err := io.ReadAll(response.Body)
					response.Body.Close()
					if err != nil {
						t.Fatal(err)
					}
					if scheme == "http" && tc.tls && !tc.allowHTTP {
						if response.StatusCode != http.StatusBadRequest {
							t.Fatalf("HTTPS-only listener accepted HTTP: %d", response.StatusCode)
						}
						continue
					}
					if response.StatusCode != http.StatusOK || string(got) != request.body || response.Header.Get("X-Server-Identity") != "same-operator" {
						t.Fatalf("%s %s request did not reach shared handler intact: %d %q", scheme, request.method, response.StatusCode, got)
					}
					if secure := response.Header.Get("X-Secure") == "true"; secure != tc.tls {
						t.Fatalf("%s handler received incorrect TLS state", scheme)
					}
				}
			}
			_ = idle.SetReadDeadline(time.Now().Add(2 * time.Second))
			if _, err := idle.Read(make([]byte, 1)); err == nil {
				t.Fatal("silent connection remained open")
			} else if timeout, ok := err.(net.Error); ok && timeout.Timeout() {
				t.Fatal("server did not expire silent connection")
			}
			if err := server.Close(); !netherNetHTTPClosed(err) {
				t.Fatalf("close signaling server: %v", err)
			}
			if conn, err := net.DialTimeout("tcp", tcp.Addr().String(), time.Second); err == nil {
				conn.Close()
				t.Fatal("signaling socket remained open after shutdown")
			}
		})
	}
}

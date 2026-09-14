package main

import (
	"context"
	"io"
	"net"
	"net/http"
	"strings"
	"time"

	"nhooyr.io/websocket"
)

// remoteProxy is a loopback WebSocket reverse proxy to the configured relay's
// /client and /client-sessions endpoints.
//
// Why it exists: on some networks the desktop WebView cannot open a TLS
// WebSocket directly to the relay — the WKWebView handshake is fingerprint-RST
// before the ServerHello ("An SSL error has occurred"). Go's TLS stack (the
// same one the uplink connects with successfully) is not filtered. So the
// frontend points remote-session attaches at ws://127.0.0.1:<addr>/client and
// this proxy relays the bytes to the real relay over Go's connection.
//
// It is a dumb byte pipe: frames flow through untouched, so the OUT chunks stay
// E2EE-sealed end-to-end (the frontend decrypts them) and the relay still sees
// a normal /client session. Auth and session selection ride inside the stream
// (the Bearer token is attached here from config; ATTACH carries the session
// id), so the proxy needs no per-request parameters.
type remoteProxy struct {
	cfgStore *configStore
	addr     string
	httpSrv  *http.Server
}

const remoteProxyReadLimit = 17 * 1024 * 1024 // matches uplink: room for PASTE_IMAGE

// startRemoteProxy binds a loopback listener and serves the /client proxy. The
// listener address is stable for the process lifetime; the relay URL/token are
// read from config on each connection so relay re-logins are picked up live.
func startRemoteProxy(cfgStore *configStore) (*remoteProxy, error) {
	if cfgStore == nil {
		return nil, nil
	}
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		return nil, err
	}
	p := &remoteProxy{cfgStore: cfgStore, addr: ln.Addr().String()}
	mux := http.NewServeMux()
	mux.HandleFunc("/client", p.handleClient)
	mux.HandleFunc("/client-sessions", p.handleClientSessions)
	// /relay-http/ forwards ordinary REST calls (e.g. /admin/api/users) to the
	// relay over Go's TLS stack. See handleHTTPProxy for why the WebView can't
	// issue these directly.
	mux.HandleFunc("/relay-http/", p.handleHTTPProxy)
	p.httpSrv = &http.Server{Handler: mux, ReadHeaderTimeout: 10 * time.Second}
	go func() {
		if err := p.httpSrv.Serve(ln); err != nil && err != http.ErrServerClosed {
			logWarn("remote-proxy", "proxy stopped serving: %v", err)
		}
	}()
	return p, nil
}

// wsURL is the ws:// base the frontend uses as the remote endpoint. Empty when
// the proxy failed to start.
func (p *remoteProxy) wsURL() string {
	if p == nil || p.addr == "" {
		return ""
	}
	return "ws://" + p.addr
}

// httpURL is the http:// base the frontend uses as apiFetch's baseURL, so REST
// calls to the relay ride through handleHTTPProxy instead of a direct WebView
// fetch. The frontend appends "/relay-http" + path (see setup in wails.ts).
// Empty when the proxy failed to start.
func (p *remoteProxy) httpURL() string {
	if p == nil || p.addr == "" {
		return ""
	}
	return "http://" + p.addr
}

func (p *remoteProxy) Stop() {
	if p == nil || p.httpSrv == nil {
		return
	}
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	_ = p.httpSrv.Shutdown(ctx)
}

func (p *remoteProxy) handleClient(w http.ResponseWriter, r *http.Request) {
	p.handleWSProxy(w, r, "/client")
}

func (p *remoteProxy) handleClientSessions(w http.ResponseWriter, r *http.Request) {
	p.handleWSProxy(w, r, "/client-sessions")
}

// handleHTTPProxy forwards a REST request under /relay-http/ to the configured
// relay over Go's TLS stack, injecting the stored Bearer token.
//
// Why it exists: on the same networks that fingerprint-RST the WebView's WS
// handshake (see handleWSProxy), the WebView's fetch() to the relay is also
// filtered — and in the desktop build apiFetch has no relay baseURL to begin
// with (relay config lives Go-side, not in localStorage), so admin/REST calls
// otherwise resolve to the wails:// origin and never reach the relay. Pointing
// apiFetch's baseURL at this loopback endpoint routes every REST call through
// Go, exactly like ListRemoteSessions/FetchRelayMe already do.
//
// The relay path is whatever follows the "/relay-http" prefix, so
// GET /relay-http/admin/api/users?limit=10 becomes GET <relay>/admin/api/users?limit=10.
// Status code, body and Content-Type are passed through unchanged so apiFetch's
// 401 -> /login.html bounce and JSON parsing keep working.
func (p *remoteProxy) handleHTTPProxy(w http.ResponseWriter, r *http.Request) {
	cfg := p.cfgStore.Get()
	if cfg.RelayURL == "" || cfg.RelaySessionToken == "" {
		http.Error(w, "no relay configured", http.StatusServiceUnavailable)
		return
	}

	relayPath := strings.TrimPrefix(r.URL.Path, "/relay-http")
	if relayPath == "" || !strings.HasPrefix(relayPath, "/") {
		relayPath = "/" + relayPath
	}
	target := strings.TrimRight(relayHTTPBase(cfg.RelayURL), "/") + relayPath
	if r.URL.RawQuery != "" {
		target += "?" + r.URL.RawQuery
	}

	ctx, cancel := context.WithTimeout(r.Context(), 30*time.Second)
	defer cancel()
	outReq, err := http.NewRequestWithContext(ctx, r.Method, target, r.Body)
	if err != nil {
		http.Error(w, "bad proxy request", http.StatusBadGateway)
		return
	}
	// Carry the caller's Content-Type/Accept but replace auth with the stored
	// session token — the WebView never has it, so any inbound Authorization is
	// meaningless here.
	if ct := r.Header.Get("Content-Type"); ct != "" {
		outReq.Header.Set("Content-Type", ct)
	}
	if ac := r.Header.Get("Accept"); ac != "" {
		outReq.Header.Set("Accept", ac)
	}
	outReq.Header.Set("Authorization", "Bearer "+cfg.RelaySessionToken)

	resp, err := relayHTTPClient(cfg.AllowInsecureRelay, 30*time.Second).Do(outReq)
	if err != nil {
		logWarn("remote-proxy", "http proxy %s %s: %v", r.Method, relayPath, err)
		http.Error(w, "relay request failed", http.StatusBadGateway)
		return
	}
	defer resp.Body.Close()

	if ct := resp.Header.Get("Content-Type"); ct != "" {
		w.Header().Set("Content-Type", ct)
	}
	w.WriteHeader(resp.StatusCode)
	_, _ = io.Copy(w, resp.Body)
}

func (p *remoteProxy) handleWSProxy(w http.ResponseWriter, r *http.Request, relayPath string) {
	cfg := p.cfgStore.Get()
	if cfg.RelayURL == "" || cfg.RelaySessionToken == "" {
		http.Error(w, "no relay configured", http.StatusServiceUnavailable)
		return
	}

	// Echo the client's requested atterm-token subprotocol so the browser WS
	// handshake completes; the real credential for the relay dial comes from
	// config below, not from this subprotocol.
	acceptOpts := &websocket.AcceptOptions{InsecureSkipVerify: true} // loopback only
	if sp := r.Header.Get("Sec-WebSocket-Protocol"); sp != "" {
		if first := strings.TrimSpace(strings.Split(sp, ",")[0]); first != "" {
			acceptOpts.Subprotocols = []string{first}
		}
	}
	local, err := websocket.Accept(w, r, acceptOpts)
	if err != nil {
		return
	}
	local.SetReadLimit(remoteProxyReadLimit)
	defer local.Close(websocket.StatusInternalError, "")

	ctx, cancel := context.WithCancel(r.Context())
	defer cancel()

	dialOpts := &websocket.DialOptions{
		HTTPHeader: http.Header{"Authorization": {"Bearer " + cfg.RelaySessionToken}},
	}
	if cfg.AllowInsecureRelay {
		// Mirrors uplink.runOnce: relayHTTPClient forces HTTP/1.1 (required for
		// the WS upgrade) and skips verification for a self-signed relay.
		dialOpts.HTTPClient = relayHTTPClient(true, 0)
	}
	dialCtx, cancelDial := context.WithTimeout(ctx, 10*time.Second)
	relayURL := uplinkDialURL(cfg.RelayHomeInstanceURL, cfg.RelayURL)
	remote, _, err := websocket.Dial(dialCtx, strings.TrimRight(relayURL, "/")+relayPath, dialOpts)
	cancelDial()
	if err != nil {
		logWarn("remote-proxy", "dial relay %s: %v", relayPath, err)
		local.Close(websocket.StatusTryAgainLater, "relay dial failed")
		return
	}
	remote.SetReadLimit(remoteProxyReadLimit)
	defer remote.Close(websocket.StatusInternalError, "")

	// Pipe both directions; the first error tears the other down via cancel.
	errc := make(chan error, 2)
	go pipeWS(ctx, remote, local, errc)
	go pipeWS(ctx, local, remote, errc)
	<-errc
}

// pipeWS copies whole WebSocket messages from src to dst until either errors.
func pipeWS(ctx context.Context, dst, src *websocket.Conn, errc chan<- error) {
	for {
		typ, data, err := src.Read(ctx)
		if err != nil {
			errc <- err
			return
		}
		if err := dst.Write(ctx, typ, data); err != nil {
			errc <- err
			return
		}
	}
}

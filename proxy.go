package goproxy

import (
	"bufio"
	"context"
	"fmt"
	"io"
	"net"
	"net/http"
	"os"
	"regexp"
)

type emptyLogger struct{}

var NopLogger = emptyLogger{}

func (_ emptyLogger) Log(keyvals ...interface{}) error { return nil }

// borrowed from github.com/go-kit/kit log
type Logger interface {
	Log(keyvals ...interface{}) error
}

type WriterLogger struct{ Writer io.Writer }

var StderrLogger = WriterLogger{os.Stderr}

func (w WriterLogger) Log(keyvals ...interface{}) error {
	_, err := fmt.Fprintln(w.Writer, keyvals...)
	return err
}

var ErrorLogger = Loggers{Error: StderrLogger, Debug: NopLogger}
var VerboseLogger = Loggers{Error: StderrLogger, Debug: NopLogger}

type Loggers struct {
	Error Logger
	Debug Logger
}

// The basic proxy type. Implements http.Handler.
type ProxyHttpServer struct {
	// session variable must be aligned in i386
	// see http://golang.org/src/pkg/sync/atomic/doc.go#L41
	sess int64
	// setting Verbose to true will log information on each request sent to the proxy
	Verbose         bool
	Loggers         Loggers
	NonproxyHandler http.Handler
	reqHandlers     []ReqHandler
	respHandlers    []RespHandler
	httpsHandlers   []HttpsHandler
	Tr              *http.Transport
	// ConnectDial will be used to create TCP connections for CONNECT requests
	// if nil Tr.Dial will be used
	ConnectDial func(network string, addr string) (net.Conn, error)
}

var hasPort = regexp.MustCompile(`:\d+$`)

func copyHeaders(dst, src http.Header) {
	for k, _ := range dst {
		dst.Del(k)
	}
	for k, vs := range src {
		for _, v := range vs {
			dst.Add(k, v)
		}
	}
}

func isEof(r *bufio.Reader) bool {
	_, err := r.Peek(1)
	if err == io.EOF {
		return true
	}
	return false
}

func (proxy *ProxyHttpServer) filterRequest(r *http.Request, ctx context.Context) (req *http.Request, resp *http.Response) {
	req = r
	for _, h := range proxy.reqHandlers {
		req, resp, ctx = h.Handle(r, ctx)
		// non-nil resp means the handler decided to skip sending the request
		// and return canned response instead.
		if resp != nil {
			break
		}
	}
	return
}
func (proxy *ProxyHttpServer) filterResponse(respOrig *http.Response, ctx context.Context) (resp *http.Response) {
	resp = respOrig
	for _, h := range proxy.respHandlers {
		ctx = CtxWithResp(ctx, resp)
		resp, ctx = h.Handle(resp, ctx)
	}
	return
}

func removeProxyHeaders(ctx context.Context, r *http.Request) {
	r.RequestURI = "" // this must be reset when serving a request with the client
	// If no Accept-Encoding header exists, Transport will add the headers it can accept
	// and would wrap the response body with the relevant reader.
	r.Header.Del("Accept-Encoding")
	// curl can add that, see
	// https://jdebp.eu./FGA/web-proxy-connection-header.html
	r.Header.Del("Proxy-Connection")
	r.Header.Del("Proxy-Authenticate")
	r.Header.Del("Proxy-Authorization")
	// Connection, Authenticate and Authorization are single hop Header:
	// http://www.w3.org/Protocols/rfc2616/rfc2616.txt
	// 14.10 Connection
	//   The Connection general-header field allows the sender to specify
	//   options that are desired for that particular connection and MUST NOT
	//   be communicated by proxies over further connections.
	r.Header.Del("Connection")
}

// Standard net/http function. Shouldn't be used directly, http.Serve will use it.
func (proxy *ProxyHttpServer) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	//r.Header["X-Forwarded-For"] = w.RemoteAddr()
	if r.Method == "CONNECT" {
		proxy.handleHttps(w, r)
	} else {
		ctx := proxy.newCtx(r)

		var err error
		proxy.Loggers.Debug.Log("event", "request", "path", r.URL.Path, "host", r.Host, "method", r.Method, "url", r.URL.String())
		if !r.URL.IsAbs() {
			proxy.NonproxyHandler.ServeHTTP(w, r)
			return
		}
		r, resp := proxy.filterRequest(r, ctx)

		if resp == nil {
			removeProxyHeaders(ctx, r)
			rt := CtxRoundTripper(ctx)
			resp, err = rt.RoundTrip(r, ctx)
			if err != nil {
				ctx = CtxWithError(ctx, err)
				resp = proxy.filterResponse(nil, ctx)
				if resp == nil {
					proxy.Loggers.Error.Log("event", "read response", "error", err.Error())
					http.Error(w, err.Error(), 500)
					return
				}
			}
			proxy.Loggers.Debug.Log("event", "response", "status", resp.Status)
		}
		origBody := resp.Body
		resp = proxy.filterResponse(resp, ctx)
		defer origBody.Close()
		proxy.Loggers.Debug.Log("event", "before copy response", "status", resp.Status)
		// http.ResponseWriter will take care of filling the correct response length
		// Setting it now, might impose wrong value, contradicting the actual new
		// body the user returned.
		// We keep the original body to remove the header only if things changed.
		// This will prevent problems with HEAD requests where there's no body, yet,
		// the Content-Length header should be set.
		if origBody != resp.Body {
			resp.Header.Del("Content-Length")
		}
		copyHeaders(w.Header(), resp.Header)
		w.WriteHeader(resp.StatusCode)
		nr, err := io.Copy(w, resp.Body)
		if err := resp.Body.Close(); err != nil {
			proxy.Loggers.Error.Log("event", "copy response close", "error", err.Error())
		}
		proxy.Loggers.Debug.Log("event", "copy response", "nbytes", nr, "error", err)
	}
}

// New proxy server, logs to StdErr by default
func New() *ProxyHttpServer {
	proxy := ProxyHttpServer{
		Loggers:       ErrorLogger,
		reqHandlers:   []ReqHandler{},
		respHandlers:  []RespHandler{},
		httpsHandlers: []HttpsHandler{},
		NonproxyHandler: http.HandlerFunc(func(w http.ResponseWriter, req *http.Request) {
			http.Error(w, "This is a proxy server. Does not respond to non-proxy requests.", 500)
		}),
		Tr: &http.Transport{TLSClientConfig: tlsClientSkipVerify,
			Proxy: http.ProxyFromEnvironment},
	}
	proxy.ConnectDial = dialerFromEnv(&proxy)
	return &proxy
}

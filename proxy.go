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

// NopLogger is an empty logger that does nothing
var NopLogger = emptyLogger{}

func (emptyLogger) Log(keyvals ...interface{}) error { return nil }

// Logger is the interface for logging
// borrowed from github.com/go-kit/kit log
type Logger interface {
	Log(keyvals ...interface{}) error
}

// WriterLogger has the Log interface and a file to write to
type WriterLogger struct {
	Writer io.Writer
}

// StderrLogger writes to the Stderr stream
var StderrLogger = WriterLogger{os.Stderr}

// Log takes a series of keywords separated by , and outputs them to the file
func (w WriterLogger) Log(keyvals ...interface{}) error {
	_, err := fmt.Fprintln(w.Writer, keyvals...)
	return err
}

// ErrorLogger is used when only errors are output to Stderr and all debug messages go to /dev/null
var ErrorLogger = Loggers{Error: StderrLogger, Debug: NopLogger}

// VerboseLogger outputs debug messages and Error messages to Stderr
var VerboseLogger = Loggers{Error: StderrLogger, Debug: StderrLogger}

// Loggers is the collection of the error and debug logger routines
type Loggers struct {
	Error Logger
	Debug Logger
}

// ProxyHttpServer is the basic proxy type. Implements http.Handler.
type ProxyHttpServer struct {
	// session variable must be aligned in i386
	// see http://golang.org/src/pkg/sync/atomic/doc.go#L41
	sess            int64
	Loggers         Loggers
	NonproxyHandler http.Handler
	reqHandlers     []ReqHandler
	respHandlers    []RespHandler
	httpsHandlers   []HTTPSHandler
	Tr              *http.Transport
	// ConnectDial will be used to create TCP connections for CONNECT requests
	// if nil Tr.Dial will be used
	ConnectDial func(ctx context.Context, network string, addr string) (net.Conn, error)
}

var hasPort = regexp.MustCompile(`:\d+$`)

func copyHeaders(dst, src http.Header) {
	for k := range dst {
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

// Verbose to true will log information on each request sent to the proxy
func (proxy *ProxyHttpServer) Verbose(verbose bool) {
	if verbose {
		proxy.Loggers = VerboseLogger
	} else {
		proxy.Loggers = ErrorLogger
	}
}

func (proxy *ProxyHttpServer) filterRequest(r *http.Request) (req *http.Request, resp *http.Response) {
	req = r
	for _, h := range proxy.reqHandlers {
		req, resp = h.Handle(req)
		// non-nil resp means the handler decided to skip sending the request
		// and return canned response instead.
		if resp != nil {
			break
		}
	}
	return
}
func (proxy *ProxyHttpServer) filterResponse(req *http.Request, resp *http.Response) (*http.Request, *http.Response) {
	for _, h := range proxy.respHandlers {
		req, resp = h.Handle(req, resp)
	}
	return req, resp
}

func removeProxyHeaders(r *http.Request) {
	r.RequestURI = "" // this must be reset when serving a request with the client
	CtxDebugLog(r, "event", "sending-request", "method", r.Method, "url", r.URL.String())
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
		r = proxy.requestWithContext(r)

		var err error
		proxy.Loggers.Debug.Log("event", "request", "path", r.URL.Path, "host", r.Host, "method", r.Method, "url", r.URL.String())
		if !r.URL.IsAbs() {
			proxy.NonproxyHandler.ServeHTTP(w, r)
			return
		}
		r, resp := proxy.filterRequest(r)

		if resp == nil {
			removeProxyHeaders(r)
			rt := CtxRoundTripper(r)
			resp, err = rt.RoundTrip(r)
			if err != nil {
				r = SetCtxError(r, err)
				r, resp = proxy.filterResponse(r, nil)
				if resp == nil {
					proxy.Loggers.Error.Log("event", "read response", "error", err.Error())
					http.Error(w, err.Error(), 500)
					return
				}
			}
			proxy.Loggers.Debug.Log("event", "response", "status", resp.Status)
		}
		origBody := resp.Body
		r, resp = proxy.filterResponse(r, resp)
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
		httpsHandlers: []HTTPSHandler{},
		NonproxyHandler: http.HandlerFunc(func(w http.ResponseWriter, req *http.Request) {
			http.Error(w, "This is a proxy server. Does not respond to non-proxy requests.", 500)
		}),
		Tr: &http.Transport{TLSClientConfig: tlsClientSkipVerify,
			Proxy: http.ProxyFromEnvironment},
	}
	proxy.ConnectDial = dialerFromEnv(&proxy)
	return &proxy
}

package goproxy

import (
	"context"
	"net/http"
)

// ProxyCtx is the Proxy context, contains useful information about every request. It is stored in the
// context of the request.
//
type ProxyCtx struct {
	// Will contain the client request from the proxy
	Req *http.Request
	// Will contain the remote server's response (if available. nil if the request wasn't send yet)
	Resp *http.Response
	// The RoundTripper context
	RoundTripper http.RoundTripper
	// will contain the recent error that occurred while trying to send receive or parse traffic
	Error error
	// A handle for the user to keep data in the context, from the call of ReqHandler to the
	// call of RespHandler
	UserData interface{}
	// Will connect a request to a response
	Session int64
	// The Proxy context that started it all
	proxy *ProxyHttpServer
}

// The key that the context is found by on the request
type ctxKey int

const (
	ctxKeyProxy ctxKey = iota
)

// CtxDebugLog Outputs a message to the debug log
func CtxDebugLog(r *http.Request, keyvals ...interface{}) {
	proxyCtx, ok := r.Context().Value(ctxKeyProxy).(*ProxyCtx)
	if ok {
		proxyCtx.proxy.Loggers.Debug.Log(keyvals...)
	}
}

// CtxErrorLog Outputs a message to the error log
func CtxErrorLog(r *http.Request, keyvals ...interface{}) {
	proxyCtx, ok := r.Context().Value(ctxKeyProxy).(*ProxyCtx)
	if ok {
		proxyCtx.proxy.Loggers.Debug.Log(keyvals...)
	} else {
		StderrLogger.Log(keyvals...)
	}
}

// GetAnyProxyCtx returns a ProxyCtx structure associated with the request.
// If none is found, it return an empty context.
func GetAnyProxyCtx(r *http.Request) *ProxyCtx {
	proxyCtx, ok := r.Context().Value(ctxKeyProxy).(*ProxyCtx)
	if !ok {
		proxyCtx = &ProxyCtx{}
	}
	return proxyCtx
}

// GetProxyCtx returns a ProxyCtx structure associated with the request.  Note that if
// there is no such structure, it allocates a new one and stores it with the request
// As such the request may be updated and you need to store the new one.
func GetProxyCtx(r *http.Request) (*http.Request, *ProxyCtx) {
	proxyCtx, ok := r.Context().Value(ctxKeyProxy).(*ProxyCtx)
	if !ok {
		// No context, so allocate a new one and store it
		proxyCtx = &ProxyCtx{}
		ctx := context.WithValue(r.Context(), ctxKeyProxy, proxyCtx)
		r = r.WithContext(ctx)
	}
	return r, proxyCtx
}

// requestWithContext updates associated the Proxy context with the request
func (proxy *ProxyHttpServer) requestWithContext(r *http.Request) *http.Request {
	return SetCtxProxy(r, proxy)
}

// SetCtxProxy sets the associated ProxyHttpServer for a request
func SetCtxProxy(r *http.Request, proxy *ProxyHttpServer) *http.Request {
	r, proxyCtx := GetProxyCtx(r)
	proxyCtx.proxy = proxy
	return r
}

// CtxProxy retrieves the associated Proxy for the request.  Note that if
// the proxy is not found, this will panic.
func CtxProxy(r *http.Request) *ProxyHttpServer {
	proxyCtx := GetAnyProxyCtx(r)
	if proxyCtx.proxy == nil {
		panic("required value in context missing")
	}
	return proxyCtx.proxy
}

// SetCtxRequest sets the associated Request for the request
func SetCtxRequest(r *http.Request, req *http.Request) *http.Request {
	r, proxyCtx := GetProxyCtx(r)
	proxyCtx.Req = req
	return r
}

// CtxRequest retrieves the associated Request for the request
func CtxRequest(r *http.Request) *http.Request {
	proxyCtx := GetAnyProxyCtx(r)
	return proxyCtx.Req
}

// SetCtxResponse sets the associated Response for a request
func SetCtxResponse(r *http.Request, resp *http.Response) *http.Request {
	r, proxyCtx := GetProxyCtx(r)
	proxyCtx.Resp = resp
	return r
}

// CtxResponse retrieves the associated Response for the request.
func CtxResponse(r *http.Request) *http.Response {
	proxyCtx := GetAnyProxyCtx(r)
	return proxyCtx.Resp
}

// SetCtxRoundTripper sets the associated RoundTripper for a request
func SetCtxRoundTripper(r *http.Request, rt http.RoundTripper) *http.Request {
	r, proxyCtx := GetProxyCtx(r)
	proxyCtx.RoundTripper = rt
	return r
}

// CtxRoundTripper retrieves the associated RoundTripper for the request.
func CtxRoundTripper(r *http.Request) http.RoundTripper {
	proxyCtx := GetAnyProxyCtx(r)
	if proxyCtx.RoundTripper == nil {
		return proxyCtx.proxy.Tr
	}
	return proxyCtx.RoundTripper
}

// SetCtxError sets the prevailing error with this request so it can be retrieved later
func SetCtxError(r *http.Request, Error error) *http.Request {
	r, proxyCtx := GetProxyCtx(r)
	proxyCtx.Error = Error
	return r
}

// CtxError gets any prevailing error that has been associated with this request
func CtxError(r *http.Request) (Error error) {
	proxyCtx := GetAnyProxyCtx(r)
	return proxyCtx.Error
}

// SetCtxUserData sets the associated UserData for a request
func SetCtxUserData(r *http.Request, UserData interface{}) *http.Request {
	r, proxyCtx := GetProxyCtx(r)
	proxyCtx.UserData = UserData
	return r
}

// CtxUserData retrieves the associated Userdata for the request.
func CtxUserData(r *http.Request) (UserData interface{}) {
	proxyCtx := GetAnyProxyCtx(r)
	return proxyCtx.UserData
}

// SetCtxSession sets the associated Session for a request
func SetCtxSession(r *http.Request, Session int64) *http.Request {
	r, proxyCtx := GetProxyCtx(r)
	proxyCtx.Session = Session
	return r
}

// CtxSession retrieves the associated Session for the request.
func CtxSession(r *http.Request) (Session int64) {
	proxyCtx := GetAnyProxyCtx(r)
	return proxyCtx.Session
}

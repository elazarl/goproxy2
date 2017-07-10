package goproxy

import (
	"context"
	"net/http"
)

// ReqHandler will "tamper" with the request coming to the proxy server
// If Handle returns req,nil the proxy will send the returned request
// to the destination server. If it returns nil,resp the proxy will
// skip sending any requests, and will simply return the response `resp`
// to the client.
type ReqHandler interface {
	Handle(req *http.Request, ctx context.Context) (*http.Request, *http.Response, context.Context)
}

// A wrapper that would convert a function to a ReqHandler interface type
type FuncReqHandler func(req *http.Request, ctx context.Context) (*http.Request, *http.Response, context.Context)

// FuncReqHandler.Handle(req,ctx) <=> FuncReqHandler(req,ctx)
func (f FuncReqHandler) Handle(req *http.Request, ctx context.Context) (*http.Request, *http.Response, context.Context) {
	return f(req, ctx)
}

// after the proxy have sent the request to the destination server, it will
// "filter" the response through the RespHandlers it has.
// The proxy server will send to the client the response returned by the RespHandler.
// In case of error, resp will be nil, and ctx.RoundTrip.Error will contain the error
type RespHandler interface {
	Handle(resp *http.Response, ctx context.Context) (*http.Response, context.Context)
}

// A wrapper that would convert a function to a RespHandler interface type
type FuncRespHandler func(resp *http.Response, ctx context.Context) (*http.Response, context.Context)

// FuncRespHandler.Handle(req,ctx) <=> FuncRespHandler(req,ctx)
func (f FuncRespHandler) Handle(resp *http.Response, ctx context.Context) (*http.Response, context.Context) {
	return f(resp, ctx)
}

// When a client send a CONNECT request to a host, the request is filtered through
// all the HttpsHandlers the proxy has, and if one returns true, the connection is
// sniffed using Man in the Middle attack.
// That is, the proxy will create a TLS connection with the client, another TLS
// connection with the destination the client wished to connect to, and would
// send back and forth all messages from the server to the client and vice versa.
// The request and responses sent in this Man In the Middle channel are filtered
// through the usual flow (request and response filtered through the ReqHandlers
// and RespHandlers)
type HttpsHandler interface {
	HandleConnect(req string, ctx context.Context) (*ConnectAction, string)
}

// A wrapper that would convert a function to a HttpsHandler interface type
type FuncHttpsHandler func(host string, ctx context.Context) (*ConnectAction, string)

// FuncHttpsHandler should implement the RespHandler interface
func (f FuncHttpsHandler) HandleConnect(host string, ctx context.Context) (*ConnectAction, string) {
	return f(host, ctx)
}

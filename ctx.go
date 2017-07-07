package goproxy

import (
	"context"
	"net/http"
)

type ctxKey int

const (
	ctxKeyReq          ctxKey = iota
	ctxKeyResp                = iota
	ctxKeyRoundTripper        = iota
	ctxKeyError               = iota
	ctxKeyProxy               = iota
)

func (proxy *ProxyHttpServer) newCtx(r *http.Request) context.Context {
	ctx := context.WithValue(context.Background(), ctxKeyProxy, proxy)
	ctx = context.WithValue(ctx, ctxKeyReq, r)
	return ctx
}

func CtxWithResp(ctx context.Context, r *http.Response) context.Context {
	return context.WithValue(ctx, ctxKeyResp, r)
}

func CtxResp(ctx context.Context) *http.Response {
	v, ok := ctx.Value(ctxKeyResp).(*http.Response)
	if !ok {
		panic("required value in context missing")
	}
	return v
}

func CtxWithReq(ctx context.Context, r *http.Request) context.Context {
	return context.WithValue(ctx, ctxKeyReq, r)
}

func CtxReq(ctx context.Context) *http.Request {
	v, ok := ctx.Value(ctxKeyReq).(*http.Request)
	if !ok {
		panic("required value in context missing")
	}
	return v
}
func CtxWithRoundTripper(ctx context.Context, rt RoundTripper) context.Context {
	return context.WithValue(ctx, ctxKeyRoundTripper, rt)
}
func CtxRoundTripper(ctx context.Context) RoundTripper {
	v, ok := ctx.Value(ctxKeyRoundTripper).(RoundTripper)
	if !ok {
		panic("required value in context missing")
	}
	return v
}

func CtxWithError(ctx context.Context, err error) context.Context {
	return context.WithValue(ctx, ctxKeyError, err)
}

func CtxError(ctx context.Context) error {
	v, ok := ctx.Value(ctxKeyError).(error)
	if !ok {
		panic("required value in context missing")
	}
	return v
}
func ctxProxy(ctx context.Context) *ProxyHttpServer {
	proxy, ok := ctx.Value(ctxKeyProxy).(*ProxyHttpServer)
	if !ok {
		panic("required value in context missing")
	}
	return proxy
}

type RoundTripper interface {
	RoundTrip(req *http.Request, ctx context.Context) (*http.Response, error)
}

type RoundTripperFunc func(req *http.Request, ctx context.Context) (*http.Response, error)

func (f RoundTripperFunc) RoundTrip(req *http.Request, ctx context.Context) (*http.Response, error) {
	return f(req, ctx)
}

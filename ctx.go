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

func (proxy *ProxyHttpServer) requestWithContext(r *http.Request) *http.Request {
	ctx := context.WithValue(r.Context(), ctxKeyProxy, proxy)
	return r.WithContext(ctx)
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
func CtxWithRoundTripper(ctx context.Context, rt http.RoundTripper) context.Context {
	return context.WithValue(ctx, ctxKeyRoundTripper, rt)
}

func CtxRoundTripper(ctx context.Context) http.RoundTripper {
	v, ok := ctx.Value(ctxKeyRoundTripper).(http.RoundTripper)
	if !ok {
		proxy := ctxProxy(ctx)
		return proxy.Tr
	}
	return v
}

func CtxWithError(ctx context.Context, err error) context.Context {
	return context.WithValue(ctx, ctxKeyError, err)
}

func CtxError(ctx context.Context) error {
	v, ok := ctx.Value(ctxKeyError).(error)
	if !ok {
		return nil
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

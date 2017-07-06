package goproxy

import (
	"bufio"
	"crypto/tls"
	"errors"
	"io"
	"io/ioutil"
	"net"
	"net/http"
	"net/url"
	"os"
	"regexp"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
)

type ConnectActionLiteral int

const (
	ConnectAccept = iota
	ConnectReject
	ConnectMitm
	ConnectHijack
	ConnectHTTPMitm
	ConnectProxyAuthHijack
)

var (
	OkConnect       = &ConnectAction{Action: ConnectAccept, TLSConfig: TLSConfigFromCA(&GoproxyCa)}
	MitmConnect     = &ConnectAction{Action: ConnectMitm, TLSConfig: TLSConfigFromCA(&GoproxyCa)}
	HTTPMitmConnect = &ConnectAction{Action: ConnectHTTPMitm, TLSConfig: TLSConfigFromCA(&GoproxyCa)}
	RejectConnect   = &ConnectAction{Action: ConnectReject, TLSConfig: TLSConfigFromCA(&GoproxyCa)}
	httpsRegexp     = regexp.MustCompile(`^https:\/\/`)
)

type ConnectAction struct {
	Action    ConnectActionLiteral
	Hijack    func(req *http.Request, client net.Conn, ctx *ProxyCtx)
	TLSConfig func(host string, ctx *ProxyCtx) (*tls.Config, error)
}

func stripPort(s string) string {
	ix := strings.IndexRune(s, ':')
	if ix == -1 {
		return s
	}
	return s[:ix]
}

func (proxy *ProxyHttpServer) dial(network, addr string) (c net.Conn, err error) {
	if proxy.Tr.Dial != nil {
		return proxy.Tr.Dial(network, addr)
	}
	return net.Dial(network, addr)
}

func (proxy *ProxyHttpServer) connectDial(network, addr string) (c net.Conn, err error) {
	if proxy.ConnectDial == nil {
		return proxy.dial(network, addr)
	}
	return proxy.ConnectDial(network, addr)
}

func (proxy *ProxyHttpServer) handleHttps(w http.ResponseWriter, r *http.Request) {
	ctx := &ProxyCtx{Req: r, Session: atomic.AddInt64(&proxy.sess, 1), proxy: proxy}

	hij, ok := w.(http.Hijacker)
	if !ok {
		panic("httpserver does not support hijacking")
	}

	proxyClient, _, e := hij.Hijack()
	if e != nil {
		panic("Cannot hijack connection " + e.Error())
	}

	proxy.Loggers.Debug.Log("event", "connect handlers", "nhandlers", len(proxy.httpsHandlers))
	todo, host := OkConnect, r.URL.Host
	for i, h := range proxy.httpsHandlers {
		newtodo, newhost := h.HandleConnect(host, ctx)

		// If found a result, break the loop immediately
		if newtodo != nil {
			todo, host = newtodo, newhost
			proxy.Loggers.Debug.Log("event", "connect handler result", "nhandler", i, "host", host, "action", todo)
			break
		}
	}
	switch todo.Action {
	case ConnectAccept:
		if !hasPort.MatchString(host) {
			host += ":80"
		}
		targetSiteCon, err := proxy.connectDial("tcp", host)
		if err != nil {
			proxy.Loggers.Error.Log("event", "accept connect error", "host", host, "error", err.Error())
			proxy.httpError(proxyClient, ctx, err)
			return
		}
		proxy.Loggers.Debug.Log("event", "accept connect", "host", host)
		proxyClient.Write([]byte("HTTP/1.0 200 OK\r\n\r\n"))

		targetTCP, targetOK := targetSiteCon.(*net.TCPConn)
		proxyClientTCP, clientOK := proxyClient.(*net.TCPConn)
		if targetOK && clientOK {
			go proxy.copyAndClose(ctx, targetTCP, proxyClientTCP)
			go proxy.copyAndClose(ctx, proxyClientTCP, targetTCP)
		} else {
			go func() {
				var wg sync.WaitGroup
				wg.Add(2)
				go proxy.copyOrWarn(ctx, targetSiteCon, proxyClient, &wg)
				go proxy.copyOrWarn(ctx, proxyClient, targetSiteCon, &wg)
				wg.Wait()
				proxyClient.Close()
				targetSiteCon.Close()

			}()
		}

	case ConnectHijack:
		proxy.Loggers.Debug.Log("event", "hijack connect", "host", host)
		proxyClient.Write([]byte("HTTP/1.0 200 OK\r\n\r\n"))
		todo.Hijack(r, proxyClient, ctx)
	case ConnectHTTPMitm:
		proxy.Loggers.Debug.Log("event", "connect HTTP MITM", "host", host)
		proxyClient.Write([]byte("HTTP/1.0 200 OK\r\n\r\n"))
		targetSiteCon, err := proxy.connectDial("tcp", host)
		if err != nil {
			proxy.Loggers.Error.Log("event", "mitm error dial", "host", host, "error", err.Error())
			return
		}
		for {
			client := bufio.NewReader(proxyClient)
			remote := bufio.NewReader(targetSiteCon)
			req, err := http.ReadRequest(client)
			if err != nil && err != io.EOF {
				proxy.Loggers.Error.Log("event", "HTTP MITM ReadRequest", "error", err.Error())
			}
			if err != nil {
				return
			}
			req, resp := proxy.filterRequest(req, ctx)
			if resp == nil {
				if err := req.Write(targetSiteCon); err != nil {
					proxy.httpError(proxyClient, ctx, err)
					return
				}
				resp, err = http.ReadResponse(remote, req)
				if err != nil {
					proxy.httpError(proxyClient, ctx, err)
					return
				}
				defer resp.Body.Close()
			}
			resp = proxy.filterResponse(resp, ctx)
			if err := resp.Write(proxyClient); err != nil {
				proxy.httpError(proxyClient, ctx, err)
				return
			}
		}
	case ConnectMitm:
		proxy.Loggers.Debug.Log("event", "connect TLS MITM", "host", host)
		proxyClient.Write([]byte("HTTP/1.0 200 OK\r\n\r\n"))
		// this goes in a separate goroutine, so that the net/http server won't think we're
		// still handling the request even after hijacking the connection. Those HTTP CONNECT
		// request can take forever, and the server will be stuck when "closed".
		// TODO: Allow Server.Close() mechanism to shut down this connection as nicely as possible
		tlsConfig := defaultTLSConfig
		if todo.TLSConfig != nil {
			var err error
			tlsConfig, err = todo.TLSConfig(host, ctx)
			if err != nil {
				proxy.httpError(proxyClient, ctx, err)
				return
			}
		}
		go func() {
			//TODO: cache connections to the remote website
			rawClientTls := tls.Server(proxyClient, tlsConfig)
			if err := rawClientTls.Handshake(); err != nil {
				proxy.Loggers.Error.Log("event", "TLS MITM Handshake", "error", err.Error())
				return
			}
			defer rawClientTls.Close()
			clientTlsReader := bufio.NewReader(rawClientTls)
			for !isEof(clientTlsReader) {
				req, err := http.ReadRequest(clientTlsReader)
				var ctx = &ProxyCtx{Req: req, Session: atomic.AddInt64(&proxy.sess, 1), proxy: proxy}
				if err != nil && err != io.EOF {
					return
				}
				if err != nil {
					proxy.Loggers.Error.Log("event", "HTTP MITM ReadRequest", "host", r.Host, "error", err.Error())
					return
				}
				req.RemoteAddr = r.RemoteAddr // since we're converting the request, need to carry over the original connecting IP as well
				proxy.Loggers.Debug.Log("event", "TLS MITM req", "host", r.Host)

				if !httpsRegexp.MatchString(req.URL.String()) {
					req.URL, err = url.Parse("https://" + r.Host + req.URL.String())
				}

				// Bug fix which goproxy fails to provide request
				// information URL in the context when does HTTPS MITM
				ctx.Req = req

				req, resp := proxy.filterRequest(req, ctx)
				if resp == nil {
					if err != nil {
						proxy.Loggers.Error.Log("event", "HTTP MITM request URL", "url", "https://"+r.Host+req.URL.Path, "error", err.Error())
						return
					}
					removeProxyHeaders(req)
					resp, err = ctx.RoundTrip(req)
					if err != nil {
						proxy.Loggers.Error.Log("event", "HTTP MITM RoundTrip", "error", err.Error())
						return
					}
					proxy.Loggers.Debug.Log("event", "TLS MITM resp", "host", r.Host, "status", resp.Status)
				}
				resp = proxy.filterResponse(resp, ctx)
				defer resp.Body.Close()

				text := resp.Status
				statusCode := strconv.Itoa(resp.StatusCode) + " "
				if strings.HasPrefix(text, statusCode) {
					text = text[len(statusCode):]
				}
				// always use 1.1 to support chunked encoding
				if _, err := io.WriteString(rawClientTls, "HTTP/1.1"+" "+statusCode+text+"\r\n"); err != nil {
					proxy.Loggers.Error.Log("event", "HTTP MITM write response", "error", err.Error())
					return
				}
				// Since we don't know the length of resp, return chunked encoded response
				// TODO: use a more reasonable scheme
				resp.Header.Del("Content-Length")
				resp.Header.Set("Transfer-Encoding", "chunked")
				// Force connection close otherwise chrome will keep CONNECT tunnel open forever
				resp.Header.Set("Connection", "close")
				if err := resp.Header.Write(rawClientTls); err != nil {
					proxy.Loggers.Error.Log("event", "HTTP MITM response write header", "error", err.Error())
					return
				}
				if _, err = io.WriteString(rawClientTls, "\r\n"); err != nil {
					proxy.Loggers.Error.Log("event", "HTTP MITM response write \\r\\n", "error", err.Error())
					return
				}
				chunked := newChunkedWriter(rawClientTls)
				if _, err := io.Copy(chunked, resp.Body); err != nil {
					proxy.Loggers.Error.Log("event", "HTTP MITM response write body", "error", err.Error())
					return
				}
				if err := chunked.Close(); err != nil {
					proxy.Loggers.Error.Log("event", "HTTP MITM response close chunked", "error", err.Error())
					return
				}
				if _, err = io.WriteString(rawClientTls, "\r\n"); err != nil {
					proxy.Loggers.Error.Log("event", "HTTP MITM response write body", "error", err.Error())
					return
				}
			}
			proxy.Loggers.Debug.Log("event", "TLS MITM EOF")
		}()
	case ConnectProxyAuthHijack:
		proxyClient.Write([]byte("HTTP/1.1 407 Proxy Authentication Required\r\n"))
		todo.Hijack(r, proxyClient, ctx)
	case ConnectReject:
		if ctx.Resp != nil {
			if err := ctx.Resp.Write(proxyClient); err != nil {
				proxy.Loggers.Error.Log("event", "HTTP CONNECT reject write", "error", err.Error())
			}
		}
		proxyClient.Close()
	}
}

func (proxy *ProxyHttpServer) httpError(w io.WriteCloser, ctx *ProxyCtx, err error) {
	if _, err := io.WriteString(w, "HTTP/1.1 502 Bad Gateway\r\n\r\n"); err != nil {
		proxy.Loggers.Error.Log("event", "HTTP Error write", "error", err.Error())
	}
	if err := w.Close(); err != nil {
		proxy.Loggers.Error.Log("event", "HTTP Error close", "error", err.Error())
	}
}

func (proxy *ProxyHttpServer) copyOrWarn(ctx *ProxyCtx, dst io.Writer, src io.Reader, wg *sync.WaitGroup) {
	if _, err := io.Copy(dst, src); err != nil {
		proxy.Loggers.Error.Log("event", "io.Copy", "error", err.Error())
	}
	wg.Done()
}

func (proxy *ProxyHttpServer) copyAndClose(ctx *ProxyCtx, dst, src *net.TCPConn) {
	if _, err := io.Copy(dst, src); err != nil {
		proxy.Loggers.Error.Log("event", "io.Copy&Close", "error", err.Error())
	}

	dst.CloseWrite()
	src.CloseRead()
}

func dialerFromEnv(proxy *ProxyHttpServer) func(network, addr string) (net.Conn, error) {
	https_proxy := os.Getenv("HTTPS_PROXY")
	if https_proxy == "" {
		https_proxy = os.Getenv("https_proxy")
	}
	if https_proxy == "" {
		return nil
	}
	return proxy.NewConnectDialToProxy(https_proxy)
}

func (proxy *ProxyHttpServer) NewConnectDialToProxy(https_proxy string) func(network, addr string) (net.Conn, error) {
	u, err := url.Parse(https_proxy)
	if err != nil {
		return nil
	}
	if u.Scheme == "" || u.Scheme == "http" {
		if strings.IndexRune(u.Host, ':') == -1 {
			u.Host += ":80"
		}
		return func(network, addr string) (net.Conn, error) {
			connectReq := &http.Request{
				Method: "CONNECT",
				URL:    &url.URL{Opaque: addr},
				Host:   addr,
				Header: make(http.Header),
			}
			c, err := proxy.dial(network, u.Host)
			if err != nil {
				return nil, err
			}
			connectReq.Write(c)
			// Read response.
			// Okay to use and discard buffered reader here, because
			// TLS server will not speak until spoken to.
			br := bufio.NewReader(c)
			resp, err := http.ReadResponse(br, connectReq)
			if err != nil {
				c.Close()
				return nil, err
			}
			defer resp.Body.Close()
			if resp.StatusCode != 200 {
				resp, err := ioutil.ReadAll(resp.Body)
				if err != nil {
					return nil, err
				}
				c.Close()
				return nil, errors.New("proxy refused connection" + string(resp))
			}
			return c, nil
		}
	}
	if u.Scheme == "https" {
		if strings.IndexRune(u.Host, ':') == -1 {
			u.Host += ":443"
		}
		return func(network, addr string) (net.Conn, error) {
			c, err := proxy.dial(network, u.Host)
			if err != nil {
				return nil, err
			}
			c = tls.Client(c, proxy.Tr.TLSClientConfig)
			connectReq := &http.Request{
				Method: "CONNECT",
				URL:    &url.URL{Opaque: addr},
				Host:   addr,
				Header: make(http.Header),
			}
			connectReq.Write(c)
			// Read response.
			// Okay to use and discard buffered reader here, because
			// TLS server will not speak until spoken to.
			br := bufio.NewReader(c)
			resp, err := http.ReadResponse(br, connectReq)
			if err != nil {
				c.Close()
				return nil, err
			}
			defer resp.Body.Close()
			if resp.StatusCode != 200 {
				body, err := ioutil.ReadAll(io.LimitReader(resp.Body, 500))
				if err != nil {
					return nil, err
				}
				c.Close()
				return nil, errors.New("proxy refused connection" + string(body))
			}
			return c, nil
		}
	}
	return nil
}

func TLSConfigFromCA(ca *tls.Certificate) func(host string, ctx *ProxyCtx) (*tls.Config, error) {
	return func(host string, ctx *ProxyCtx) (*tls.Config, error) {
		config := *defaultTLSConfig
		cert, err := signHost(*ca, []string{stripPort(host)})
		if err != nil {
			return nil, err
		}
		config.Certificates = append(config.Certificates, cert)
		return &config, nil
	}
}

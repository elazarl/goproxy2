package main

import (
	"context"
	"fmt"
	"io"
	"log"
	. "net/http"
	"time"

	"github.com/elazarl/goproxy2"
	"github.com/elazarl/goproxy2/ext/html"
)

type Count struct {
	Id    string
	Count int64
}
type CountReadCloser struct {
	Id string
	R  io.ReadCloser
	ch chan<- Count
	nr int64
}

func (c *CountReadCloser) Read(b []byte) (n int, err error) {
	n, err = c.R.Read(b)
	c.nr += int64(n)
	return
}
func (c CountReadCloser) Close() error {
	c.ch <- Count{c.Id, c.nr}
	return c.R.Close()
}

func main() {
	proxy := goproxy.New()
	timer := make(chan bool)
	ch := make(chan Count, 10)
	go func() {
		for {
			time.Sleep(20 * time.Second)
			timer <- true
		}
	}()
	go func() {
		m := make(map[string]int64)
		for {
			select {
			case c := <-ch:
				m[c.Id] = m[c.Id] + c.Count
			case <-timer:
				fmt.Printf("statistics\n")
				for k, v := range m {
					fmt.Printf("%s -> %d\n", k, v)
				}
			}
		}
	}()

	// IsWebRelatedText filters on html/javascript/css resources
	proxy.OnResponse(goproxy_html.IsWebRelatedText).DoFunc(func(resp *Response, ctx context.Context) *Response {
		resp.Body = &CountReadCloser{CtxReq(ctx).URL.String(), resp.Body, ch, 0}
		return resp
	})
	fmt.Printf("listening on :8080\n")
	log.Fatal(ListenAndServe(":8080", proxy))
}

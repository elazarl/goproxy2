package main

import (
	"context"
	"flag"
	"log"
	"net/http"

	"github.com/toebes/goproxy2"
)

func main() {
	verbose := flag.Bool("v", false, "should every proxy request be logged to stdout")
	addr := flag.String("addr", ":8080", "proxy listen address")
	flag.Parse()
	proxy := goproxy.New()
	proxy.OnRequest().HandleConnect(goproxy.AlwaysMitm)
	proxy.OnRequest().DoFunc(func(req *http.Request, ctx context.Context) (*http.Request, *http.Response) {
		if req.URL.Scheme == "https" {
			req.URL.Scheme = "http"
		}
		return req, nil
	})
	proxy.Verbose(*verbose)
	log.Fatal(http.ListenAndServe(*addr, proxy))
}

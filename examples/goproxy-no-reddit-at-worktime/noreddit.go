package main

import (
	"context"
	"log"
	"net/http"
	"time"

	"github.com/elazarl/goproxy2"
)

func main() {
	proxy := goproxy.New()
	proxy.OnRequest(goproxy.DstHostIs("www.reddit.com")).DoFunc(
		func(r *http.Request, ctx context.Context) (*http.Request, *http.Response) {
			h, _, _ := time.Now().Clock()
			if h >= 8 && h <= 17 {
				return r, goproxy.NewResponse(r,
					goproxy.ContentTypeText, http.StatusForbidden,
					"Don't waste your time!")
			} else {
				ctx.Warnf("clock: %d, you can waste your time...", h)
			}
			return r, nil
		})
	log.Fatalln(http.ListenAndServe(":8080", proxy))
}

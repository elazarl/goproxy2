package main

import (
	"flag"
	"log"
	"net/http"

	"github.com/toebes/goproxy2"
)

func main() {
	verbose := flag.Bool("v", false, "should every proxy request be logged to stdout")
	addr := flag.String("addr", ":8080", "proxy listen address")
	flag.Parse()
	setCA(caCert, caKey)
	proxy := goproxy.New()
	proxy.OnRequest().HandleConnect(goproxy.AlwaysMitm)
	proxy.Verbose(*verbose)
	log.Fatal(http.ListenAndServe(*addr, proxy))
}

package main

import (
	"flag"
	"log"
	"net/http"

	"github.com/toebes/goproxy2"
)

func main() {
	verbose := flag.Bool("v", true, "should every proxy request be logged to stdout")
	addr := flag.String("addr", ":8080", "proxy listen address")
	flag.Parse()
	proxy := goproxy.New()
	proxy.Verbose(*verbose)
	log.Fatal(http.ListenAndServe(*addr, proxy))
}

package main

import (
	"context"
	"image"
	"log"
	"net/http"

	"github.com/toebes/goproxy2"
	"github.com/toebes/goproxy2/ext/image"
)

func main() {
	proxy := goproxy.New()
	proxy.OnResponse().Do(goproxy_image.HandleImage(func(img image.Image, ctx context.Context) image.Image {
		dx, dy := img.Bounds().Dx(), img.Bounds().Dy()

		nimg := image.NewRGBA(img.Bounds())
		for i := 0; i < dx; i++ {
			for j := 0; j <= dy; j++ {
				nimg.Set(i, j, img.At(i, dy-j-1))
			}
		}
		return nimg
	}))
	proxy.Verbose = true
	log.Fatal(http.ListenAndServe(":8080", proxy))
}

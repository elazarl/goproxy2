package goproxy_image

import (
	"bytes"
	. "github.com/elazarl/goproxy2"
	"github.com/elazarl/goproxy2/regretable"
	"image"
	_ "image/gif"
	"image/jpeg"
	"image/png"
	"io/ioutil"
	"net/http"
)

var RespIsImage = ContentTypeIs("image/gif",
	"image/jpeg",
	"image/pjpeg",
	"application/octet-stream",
	"image/png")

// "image/tiff" tiff support is in external package, and rarely used, so we omitted it

func HandleImage(f func(img image.Image, ctx *ProxyCtx) image.Image) RespHandler {
	return FuncRespHandler(func(resp *http.Response, ctx *ProxyCtx) *http.Response {
		if !RespIsImage.HandleResp(resp, ctx) {
			return resp
		}
		if resp.StatusCode != 200 {
			// we might get 304 - not modified response without data
			return resp
		}
		contentType := resp.Header.Get("Content-Type")

		const kb = 1024
		regret := regretable.NewRegretableReaderCloserSize(resp.Body, 16*kb)
		resp.Body = regret
		img, imgType, err := image.Decode(resp.Body)
		if err != nil {
			regret.Regret()
			return resp
		}
		result := f(img, ctx)
		buf := bytes.NewBuffer([]byte{})
		switch contentType {
		// No gif image encoder in go - convert to png
		case "image/gif", "image/png":
			if err := png.Encode(buf, result); err != nil {
				return resp
			}
			resp.Header.Set("Content-Type", "image/png")
		case "image/jpeg", "image/pjpeg":
			if err := jpeg.Encode(buf, result, nil); err != nil {
				return resp
			}
		case "application/octet-stream":
			switch imgType {
			case "jpeg":
				if err := jpeg.Encode(buf, result, nil); err != nil {
					return resp
				}
			case "png", "gif":
				if err := png.Encode(buf, result); err != nil {
					return resp
				}
			}
		default:
			panic("unhandlable type" + contentType)
		}
		resp.Body = ioutil.NopCloser(buf)
		return resp
	})
}

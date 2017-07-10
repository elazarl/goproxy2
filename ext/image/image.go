package goproxy_image

import (
	"bytes"
	"context"
	"image"
	_ "image/gif"
	"image/jpeg"
	"image/png"
	"io/ioutil"
	"net/http"

	. "github.com/elazarl/goproxy2"
	"github.com/elazarl/goproxy2/regretable"
)

var RespIsImage = ContentTypeIs("image/gif",
	"image/jpeg",
	"image/pjpeg",
	"application/octet-stream",
	"image/png")

// "image/tiff" tiff support is in external package, and rarely used, so we omitted it

func HandleImage(f func(img image.Image, ctx context.Context) image.Image) RespHandler {
	return FuncRespHandler(func(resp *http.Response, ctx context.Context) (*http.Response, context.Context) {
		if !RespIsImage.HandleResp(resp, ctx) {
			return resp, ctx
		}
		if resp.StatusCode != 200 {
			// we might get 304 - not modified response without data
			return resp, ctx
		}
		contentType := resp.Header.Get("Content-Type")

		const kb = 1024
		regret := regretable.NewRegretableReaderCloserSize(resp.Body, 16*kb)
		resp.Body = regret
		img, imgType, err := image.Decode(resp.Body)
		if err != nil {
			regret.Regret()
			return resp, ctx
		}
		result := f(img, ctx)
		buf := bytes.NewBuffer([]byte{})
		switch contentType {
		// No gif image encoder in go - convert to png
		case "image/gif", "image/png":
			if err := png.Encode(buf, result); err != nil {
				return resp, ctx
			}
			resp.Header.Set("Content-Type", "image/png")
		case "image/jpeg", "image/pjpeg":
			if err := jpeg.Encode(buf, result, nil); err != nil {
				return resp, ctx
			}
		case "application/octet-stream":
			switch imgType {
			case "jpeg":
				if err := jpeg.Encode(buf, result, nil); err != nil {
					return resp, ctx
				}
			case "png", "gif":
				if err := png.Encode(buf, result); err != nil {
					return resp, ctx
				}
			}
		default:
			panic("unhandlable type" + contentType)
		}
		resp.Body = ioutil.NopCloser(buf)
		return resp, ctx
	})
}

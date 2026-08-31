package images

import (
	"image"
	"io"

	"github.com/chai2010/webp"
)

func encodeWebP(w io.Writer, img image.Image, quality int, lossless bool) (string, error) {
	err := webp.Encode(w, img, &webp.Options{
		Lossless: lossless,
		Quality:  float32(quality),
	})
	return "image/webp", err
}

func webpIntermediateExtension() string {
	return "webp"
}

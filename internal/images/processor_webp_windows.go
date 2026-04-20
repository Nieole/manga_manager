//go:build windows

package images

import (
	"image"
	"image/png"
	"io"
)

func encodeWebP(w io.Writer, img image.Image, quality int, lossless bool) (string, error) {
	if err := png.Encode(w, img); err != nil {
		return "", err
	}
	return "image/png", nil
}

func webpIntermediateExtension() string {
	return "png"
}

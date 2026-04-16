package main

import (
	"bytes"
	"fmt"
	"image/jpeg"

	"github.com/disintegration/imaging"
)

// MakeThumbnail resizes a JPEG image to the given width, preserving aspect ratio.
func MakeThumbnail(data []byte, width, quality int) ([]byte, error) {
	src, err := imaging.Decode(bytes.NewReader(data))
	if err != nil {
		return nil, fmt.Errorf("decode image: %w", err)
	}

	thumb := imaging.Resize(src, width, 0, imaging.Lanczos)

	var buf bytes.Buffer
	if err := jpeg.Encode(&buf, thumb, &jpeg.Options{Quality: quality}); err != nil {
		return nil, fmt.Errorf("encode thumbnail: %w", err)
	}

	return buf.Bytes(), nil
}

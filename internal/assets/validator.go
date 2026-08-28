package assets

import (
	"bytes"
	"errors"
	"fmt"
	"image"
	"image/jpeg"
	"image/png"
	"net/http"
	"strings"
)

const (
	maxLogoBytes     = 512 * 1024
	minLogoDimension = 32
	maxLogoDimension = 1024
)

type LogoInfo struct {
	MIME   string
	Width  int
	Height int
}

func ValidateLogo(data []byte, declaredMIME string) (LogoInfo, error) {
	if len(data) == 0 || len(data) > maxLogoBytes {
		return LogoInfo{}, errors.New("logo must be between 1 byte and 512 KiB")
	}
	declaredMIME = strings.ToLower(strings.TrimSpace(strings.SplitN(declaredMIME, ";", 2)[0]))
	sniffed := http.DetectContentType(data)
	if declaredMIME != sniffed || (sniffed != "image/png" && sniffed != "image/jpeg") {
		return LogoInfo{}, errors.New("logo MIME must match a PNG or JPEG payload")
	}
	config, format, err := image.DecodeConfig(bytes.NewReader(data))
	if err != nil || (format != "png" && format != "jpeg") {
		return LogoInfo{}, errors.New("logo image cannot be decoded")
	}
	if config.Width < minLogoDimension || config.Height < minLogoDimension ||
		config.Width > maxLogoDimension || config.Height > maxLogoDimension {
		return LogoInfo{}, fmt.Errorf("logo dimensions must be between %d and %d pixels", minLogoDimension, maxLogoDimension)
	}
	return LogoInfo{MIME: sniffed, Width: config.Width, Height: config.Height}, nil
}

func encodeCanonical(data []byte, mime string) ([]byte, error) {
	img, _, err := image.Decode(bytes.NewReader(data))
	if err != nil {
		return nil, err
	}
	var output bytes.Buffer
	switch mime {
	case "image/png":
		err = png.Encode(&output, img)
	case "image/jpeg":
		err = jpeg.Encode(&output, img, &jpeg.Options{Quality: 90})
	default:
		err = errors.New("unsupported image MIME")
	}
	if err != nil {
		return nil, err
	}
	return output.Bytes(), nil
}

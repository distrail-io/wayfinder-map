// Package images fetches the images in issue bodies and decodes them. It is
// the only code that opens a connection itself rather than through gh, so
// it fetches only from GitHub's image hosts, with no credentials, and
// within limits on time, size and pixels.
package images

import (
	"bytes"
	"errors"
	"fmt"
	"image"
	_ "image/gif" // the first frame
	_ "image/jpeg"
	_ "image/png"
	"io"
	"mime"
	"net/http"
	"net/url"
	"slices"
	"strings"
	"time"

	"golang.org/x/image/draw"
	_ "golang.org/x/image/webp"
)

// Hosts are the only hosts images are fetched from. GitHub signs the URLs
// of a private repo's images on private-user-images, so they load without
// a login; camo proxies images from elsewhere.
var Hosts = []string{
	"private-user-images.githubusercontent.com",
	"user-images.githubusercontent.com",
	"camo.githubusercontent.com",
}

const (
	// Timeout bounds a fetch, from connecting to reading the last byte.
	Timeout = 10 * time.Second
	// MaxBytes is the largest image fetched or decoded.
	MaxBytes = 10 << 20
	// MaxSide is the most pixels an image may span either way.
	MaxSide = 4096
)

// Allowed reports whether u is an https URL on one of the Hosts, on the
// default port and without user info.
func Allowed(u string) bool {
	p, err := url.Parse(u)
	return err == nil && p.Scheme == "https" && p.User == nil && p.Port() == "" && slices.Contains(Hosts, p.Host)
}

// Asset names the image u points at: its host and path, without the query
// that a signed URL's short-lived signature is in. It stays the same when
// GitHub signs the URL afresh.
func Asset(u string) string {
	p, err := url.Parse(u)
	if err != nil {
		return u
	}
	return p.Host + p.Path
}

// Client fetches images. Its zero value uses the default transport.
type Client struct{ Transport http.RoundTripper }

// Fetch gets the image at u, which must be Allowed, as must every redirect.
// It sends no credentials: no token, no cookies. The reply must be an
// image no larger than MaxBytes.
func (c Client) Fetch(u string) ([]byte, error) {
	if !Allowed(u) {
		return nil, errors.New("not an image host")
	}
	client := &http.Client{Transport: c.Transport, Timeout: Timeout,
		CheckRedirect: func(r *http.Request, via []*http.Request) error {
			if len(via) >= 3 || !Allowed(r.URL.String()) {
				return errors.New("redirected away")
			}
			return nil
		}}
	resp, err := client.Get(u)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("image: %s", resp.Status)
	}
	if typ, _, _ := mime.ParseMediaType(resp.Header.Get("Content-Type")); !strings.HasPrefix(typ, "image/") {
		return nil, fmt.Errorf("not an image: %q", typ)
	}
	if resp.ContentLength > MaxBytes {
		return nil, errors.New("image too large")
	}
	data, err := io.ReadAll(io.LimitReader(resp.Body, MaxBytes+1))
	if err != nil {
		return nil, err
	}
	if len(data) > MaxBytes {
		return nil, errors.New("image too large")
	}
	return data, nil
}

// Decode decodes a PNG, JPEG, GIF (its first frame) or WebP no larger than
// MaxBytes. Its size is read from its header first, and an image wider or
// taller than MaxSide isn't decoded.
func Decode(data []byte) (image.Image, error) {
	if len(data) > MaxBytes {
		return nil, errors.New("image too large")
	}
	cfg, _, err := image.DecodeConfig(bytes.NewReader(data))
	if err != nil {
		return nil, err
	}
	if cfg.Width <= 0 || cfg.Height <= 0 || cfg.Width > MaxSide || cfg.Height > MaxSide {
		return nil, fmt.Errorf("image is %d×%d", cfg.Width, cfg.Height)
	}
	img, _, err := image.Decode(bytes.NewReader(data))
	return img, err
}

// Scale draws img as large as fits in a w×h box, keeping its shape, at the
// box's top left. The rest of the box is transparent.
func Scale(img image.Image, w, h int) image.Image {
	b := img.Bounds()
	s := min(float64(w)/float64(b.Dx()), float64(h)/float64(b.Dy()))
	dw, dh := max(1, int(float64(b.Dx())*s)), max(1, int(float64(b.Dy())*s))
	out := image.NewRGBA(image.Rect(0, 0, w, h))
	draw.CatmullRom.Scale(out, image.Rect(0, 0, min(dw, w), min(dh, h)), img, b, draw.Over, nil)
	return out
}

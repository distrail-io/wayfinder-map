package images_test

import (
	"bytes"
	"encoding/base64"
	"image"
	"image/color"
	"image/gif"
	"image/jpeg"
	"image/png"
	"io"
	"net/http"
	"testing"

	"github.com/distrail-io/wayfinder-map/internal/images"
)

func TestOnlyGitHubsImageHostsAreAllowed(t *testing.T) {
	for url, want := range map[string]bool{
		"https://private-user-images.githubusercontent.com/1/a.png?jwt=x": true,
		"https://user-images.githubusercontent.com/1/a.png":               true,
		"https://camo.githubusercontent.com/abc/def":                      true,
		"http://camo.githubusercontent.com/abc":                           false, // not https
		"https://github.com/user-attachments/assets/1":                    false,
		"https://example.com/a.png":                                       false,
		"https://camo.githubusercontent.com.evil.com/a":                   false,
		"https://evil.com/camo.githubusercontent.com/a":                   false,
		"https://user@camo.githubusercontent.com/a":                       false,
		"https://camo.githubusercontent.com:8443/a":                       false,
		"//camo.githubusercontent.com/a":                                  false,
		"":                                                                false,
	} {
		if got := images.Allowed(url); got != want {
			t.Errorf("Allowed(%q) = %v, want %v", url, got, want)
		}
	}
}

// fakeTransport answers every request with its response, and records them.
type fakeTransport struct {
	reqs []*http.Request
	resp func(*http.Request) *http.Response
}

func (f *fakeTransport) RoundTrip(r *http.Request) (*http.Response, error) {
	f.reqs = append(f.reqs, r)
	return f.resp(r), nil
}

func reply(status int, typ string, body []byte) func(*http.Request) *http.Response {
	return func(r *http.Request) *http.Response {
		h := http.Header{}
		if typ != "" {
			h.Set("Content-Type", typ)
		}
		return &http.Response{StatusCode: status, Header: h, Body: io.NopCloser(bytes.NewReader(body)),
			ContentLength: -1, Request: r}
	}
}

func pngOf(w, h int) []byte {
	var b bytes.Buffer
	png.Encode(&b, image.NewRGBA(image.Rect(0, 0, w, h)))
	return b.Bytes()
}

const url = "https://private-user-images.githubusercontent.com/1/a.png?jwt=x"

// The fetch carries no credentials: no token, no cookies, no login.
func TestFetchSendsNoCredentials(t *testing.T) {
	ft := &fakeTransport{resp: reply(200, "image/png", pngOf(2, 2))}
	data, err := images.Client{Transport: ft}.Fetch(url)
	if err != nil || !bytes.Equal(data, pngOf(2, 2)) {
		t.Fatalf("Fetch = %d bytes, %v", len(data), err)
	}
	if len(ft.reqs) != 1 {
		t.Fatalf("%d requests, want 1", len(ft.reqs))
	}
	r := ft.reqs[0]
	if r.Method != "GET" || r.URL.String() != url {
		t.Errorf("request %s %s", r.Method, r.URL)
	}
	for _, h := range []string{"Authorization", "Cookie", "Proxy-Authorization"} {
		if r.Header.Get(h) != "" {
			t.Errorf("the request carries %s", h)
		}
	}
	if r.URL.User != nil {
		t.Errorf("the request carries user info")
	}
}

func TestFetchRefuses(t *testing.T) {
	big := bytes.Repeat([]byte{0}, images.MaxBytes+1)
	for _, c := range []struct {
		name string
		url  string
		resp func(*http.Request) *http.Response
	}{
		{"another host", "https://example.com/a.png", reply(200, "image/png", pngOf(2, 2))},
		{"a page, not an image", url, reply(200, "text/html; charset=utf-8", []byte("<html>sign in</html>"))},
		{"no type", url, reply(200, "", pngOf(2, 2))},
		{"an error", url, reply(404, "image/png", nil)},
		{"over the size cap", url, reply(200, "image/png", big)},
		{"a redirect to another host", url, func(r *http.Request) *http.Response {
			if r.URL.Host == "example.com" {
				return reply(200, "image/png", pngOf(2, 2))(r)
			}
			resp := reply(302, "", nil)(r)
			resp.Header.Set("Location", "https://example.com/a.png")
			return resp
		}},
	} {
		t.Run(c.name, func(t *testing.T) {
			ft := &fakeTransport{resp: c.resp}
			if data, err := (images.Client{Transport: ft}).Fetch(c.url); err == nil {
				t.Errorf("fetched %d bytes", len(data))
			}
			for _, r := range ft.reqs {
				if !images.Allowed(r.URL.String()) {
					t.Errorf("requested %s", r.URL)
				}
			}
		})
	}
}

// The size cap holds even when the server says how long the body is.
func TestFetchStopsAtADeclaredSizeOverTheCap(t *testing.T) {
	read := 0
	ft := &fakeTransport{resp: func(r *http.Request) *http.Response {
		resp := reply(200, "image/png", nil)(r)
		resp.ContentLength = images.MaxBytes + 1
		resp.Body = io.NopCloser(readFunc(func(p []byte) (int, error) { read += len(p); return len(p), nil }))
		return resp
	}}
	if _, err := (images.Client{Transport: ft}).Fetch(url); err == nil || read > 0 {
		t.Errorf("Fetch read %d bytes, err %v", read, err)
	}
}

type readFunc func([]byte) (int, error)

func (f readFunc) Read(p []byte) (int, error) { return f(p) }

func TestDecodeReadsPNGJPEGGIFAndWebP(t *testing.T) {
	img := image.NewRGBA(image.Rect(0, 0, 3, 2))
	var j, g bytes.Buffer
	jpeg.Encode(&j, img, nil)
	gif.Encode(&g, img, nil)
	// A 1×1 lossless WebP.
	webp, _ := base64.StdEncoding.DecodeString("UklGRhoAAABXRUJQVlA4TA0AAAAvAAAAEAcQERGIiP4HAA==")
	for name, c := range map[string]struct {
		data []byte
		w, h int
	}{"png": {pngOf(3, 2), 3, 2}, "jpeg": {j.Bytes(), 3, 2}, "gif": {g.Bytes(), 3, 2}, "webp": {webp, 1, 1}} {
		got, err := images.Decode(c.data)
		if err != nil {
			t.Errorf("%s: %v", name, err)
			continue
		}
		if b := got.Bounds(); b.Dx() != c.w || b.Dy() != c.h {
			t.Errorf("%s decoded to %v", name, b)
		}
	}
}

// An image larger than MaxSide is refused from its header, before it is
// decoded; so is anything that isn't one of the formats.
func TestDecodeRefuses(t *testing.T) {
	for name, data := range map[string][]byte{
		"too wide":  pngOf(images.MaxSide+1, 1),
		"too tall":  pngOf(1, images.MaxSide+1),
		"svg":       []byte(`<svg xmlns="http://www.w3.org/2000/svg" width="10" height="10"></svg>`),
		"html":      []byte("<html>sign in</html>"),
		"truncated": pngOf(4, 4)[:40],
		"too big":   append(pngOf(2, 2), make([]byte, images.MaxBytes)...),
	} {
		if _, err := images.Decode(data); err == nil {
			t.Errorf("decoded %s", name)
		}
	}
}

// Scale fits an image into a box, keeping its shape, at the top left.
func TestScaleFitsTheBoxKeepingTheShape(t *testing.T) {
	src := image.NewRGBA(image.Rect(0, 0, 200, 100))
	for y := range 100 {
		for x := range 200 {
			src.Set(x, y, color.RGBA{255, 0, 0, 255})
		}
	}
	got := images.Scale(src, 50, 50)
	if b := got.Bounds(); b.Dx() != 50 || b.Dy() != 50 {
		t.Fatalf("scaled to %v, want 50×50", b)
	}
	if _, _, _, a := got.At(25, 10).RGBA(); a == 0 {
		t.Errorf("the image doesn't fill the top of the box")
	}
	if _, _, _, a := got.At(25, 40).RGBA(); a != 0 {
		t.Errorf("the image spills below its shape")
	}
}


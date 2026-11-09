package kitty_test

import (
	"bytes"
	"encoding/base64"
	"image"
	"image/color"
	"image/png"
	"strings"
	"testing"
	"testing/iotest"

	"github.com/charmbracelet/x/ansi"

	"github.com/distrail-io/wayfinder-map/internal/kitty"
)

const (
	clear   = "\x1b_Ga=d,d=A,q=2\x1b\\"
	home    = "\x1b7\x1b[1;1H"
	restore = "\x1b8"
)

func TestPlaceSmallImage(t *testing.T) {
	got := kitty.Place([]byte("abc"), 80, 20)
	want := clear + home + "\x1b_Ga=T,f=100,q=2,C=1,c=80,r=20,m=0;YWJj\x1b\\" + restore
	if got != want {
		t.Errorf("Place =\n%q\nwant\n%q", got, want)
	}
}

func TestPlaceChunksAt4096(t *testing.T) {
	// 3072 zero bytes encode to exactly 4096 'A's: one chunk.
	got := kitty.Place(make([]byte, 3072), 10, 5)
	want := clear + home + "\x1b_Ga=T,f=100,q=2,C=1,c=10,r=5,m=0;" + strings.Repeat("A", 4096) + "\x1b\\" + restore
	if got != want {
		t.Errorf("4096 base64 bytes were not sent as one chunk")
	}
	// Three more bytes spill four characters into a second chunk.
	got = kitty.Place(make([]byte, 3075), 10, 5)
	want = clear + home +
		"\x1b_Ga=T,f=100,q=2,C=1,c=10,r=5,m=1;" + strings.Repeat("A", 4096) + "\x1b\\" +
		"\x1b_Gm=0;AAAA\x1b\\" + restore
	if got != want {
		t.Errorf("4100 base64 bytes were not split 4096 + 4")
	}
}

func TestClear(t *testing.T) {
	if kitty.Clear != clear {
		t.Errorf("Clear = %q, want %q", kitty.Clear, clear)
	}
}

func TestQueryAsksForGraphicsThenDeviceAttributes(t *testing.T) {
	// A 1×1 RGB query that stores nothing, then DA1, which every terminal
	// answers, so a terminal that ignores the query is found out at once.
	want := "\x1b_Gi=31,s=1,v=1,a=q,t=d,f=24;AAAA\x1b\\" + "\x1b[c"
	if kitty.Query != want {
		t.Errorf("Query = %q, want %q", kitty.Query, want)
	}
}

func TestSupportedReadsTheRepliesToQuery(t *testing.T) {
	for _, c := range []struct {
		name, reply string
		want        bool
	}{
		{"kitty answers OK", "\x1b_Gi=31;OK\x1b\\\x1b[?62;22c", true},
		{"only device attributes", "\x1b[?62;22;52c", false},
		{"an error", "\x1b_Gi=31;ENOTSUPPORTED:no\x1b\\\x1b[?62c", false},
		{"no reply before the timeout", "", false},
		{"cut off before device attributes", "\x1b_Gi=31;OK\x1b\\", true},
	} {
		t.Run(c.name, func(t *testing.T) {
			if got := kitty.Supported(iotest.OneByteReader(strings.NewReader(c.reply))); got != c.want {
				t.Errorf("Supported(%q) = %v, want %v", c.reply, got, c.want)
			}
		})
	}
}

// Whatever follows the replies, such as a key pressed meanwhile, is left
// for the app to read.
func TestSupportedStopsAtDeviceAttributes(t *testing.T) {
	r := strings.NewReader("\x1b_Gi=31;OK\x1b\\\x1b[?62;22cq")
	if !kitty.Supported(r) {
		t.Error("not supported")
	}
	if r.Len() != 1 {
		t.Errorf("%d bytes left unread, want the 1 after the replies", r.Len())
	}
}

// Placeholder IDs keep their low byte, the cells' 256-colour foreground, at
// 16 or above, clear of the colours a terminal theme remaps, and spill into
// the top byte, which a third diacritic carries.
func TestIDsForPlaceholders(t *testing.T) {
	for _, c := range []struct{ n, want int }{
		{0, 1<<24 | 16},
		{1, 1<<24 | 17},
		{239, 1<<24 | 255},
		{240, 2<<24 | 16},
	} {
		if got := kitty.ID(c.n); got != c.want {
			t.Errorf("ID(%d) = %#x, want %#x", c.n, got, c.want)
		}
	}
}

// Every cell carries its row, column and top-byte diacritics, so any cell
// the renderer repaints alone still shows its piece of the image.
func TestPlaceholdersShowAnImageCellByCell(t *testing.T) {
	// The kitty docs' example: U+0305 is 0, U+030D is 1 and U+030E is 2.
	const p = "\U0010EEEE"
	got := kitty.Placeholders(2<<24|42, 2, 2)
	want := []string{
		"\x1b[38;5;42m" + p + "̅̅̎" + p + "̅̍̎" + "\x1b[39m",
		"\x1b[38;5;42m" + p + "̍̅̎" + p + "̍̍̎" + "\x1b[39m",
	}
	if len(got) != len(want) {
		t.Fatalf("Placeholders = %q, want %q", got, want)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Errorf("row %d = %q, want %q", i, got[i], want[i])
		}
	}
	// A cell takes one column, diacritics and all.
	if w := ansi.StringWidth(kitty.Placeholders(kitty.ID(0), 7, 1)[0]); w != 7 {
		t.Errorf("7 cells are %d columns wide", w)
	}
}

func TestTransmitSendsAnImageForPlaceholders(t *testing.T) {
	img := image.NewRGBA(image.Rect(0, 0, 3, 2))
	img.Set(1, 1, color.RGBA{0xff, 0, 0, 0xff})
	got := kitty.Transmit(1<<24|16, img, 30, 4)
	// A PNG, quietly, as the ID, with a virtual placement over 30×4 cells.
	head := "\x1b_Gf=100,q=2,i=16777232,U=1,c=30,r=4,a=T;"
	if !strings.HasPrefix(got, head) || !strings.HasSuffix(got, "\x1b\\") {
		t.Fatalf("Transmit = %q, want it to start %q", got, head)
	}
	raw, err := base64.StdEncoding.DecodeString(strings.TrimSuffix(strings.TrimPrefix(got, head), "\x1b\\"))
	if err != nil {
		t.Fatal(err)
	}
	dec, err := png.Decode(bytes.NewReader(raw))
	if err != nil {
		t.Fatal(err)
	}
	if dec.Bounds() != img.Bounds() || color.RGBAModel.Convert(dec.At(1, 1)) != img.At(1, 1) {
		t.Errorf("sent a different image: %v", dec.Bounds())
	}
}

func TestTransmitChunksLargeImages(t *testing.T) {
	// Noise doesn't compress, so its PNG runs to several chunks.
	img := image.NewRGBA(image.Rect(0, 0, 64, 64))
	x := uint32(1)
	for i := range img.Pix {
		x ^= x << 13
		x ^= x >> 17
		x ^= x << 5
		img.Pix[i] = byte(x)
	}
	got := kitty.Transmit(1<<24|16, img, 8, 4)
	chunks := strings.Split(strings.TrimSuffix(got, "\x1b\\"), "\x1b\\")
	if len(chunks) < 2 {
		t.Fatalf("one chunk for a large image")
	}
	if !strings.HasPrefix(chunks[0], "\x1b_Gf=100,q=2,i=16777232,U=1,c=8,r=4,a=T,m=1;") {
		t.Errorf("first chunk starts %q", chunks[0][:60])
	}
	for _, c := range chunks[1 : len(chunks)-1] {
		if !strings.HasPrefix(c, "\x1b_Gq=2,m=1;") {
			t.Errorf("middle chunk starts %q", c[:20])
		}
	}
	if !strings.HasPrefix(chunks[len(chunks)-1], "\x1b_Gq=2,m=0;") {
		t.Errorf("last chunk starts %q", chunks[len(chunks)-1][:20])
	}
}

// Deleting by ID frees the image too, and removes its virtual placement,
// which Clear leaves.
func TestDeleteRemovesAnImageByID(t *testing.T) {
	if got, want := kitty.Delete(1<<24|16), "\x1b_Gq=2,i=16777232,d=I,a=d\x1b\\"; got != want {
		t.Errorf("Delete = %q, want %q", got, want)
	}
}

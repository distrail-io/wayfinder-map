package kitty_test

import (
	"strings"
	"testing"

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

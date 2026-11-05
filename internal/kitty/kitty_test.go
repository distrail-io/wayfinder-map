package kitty_test

import (
	"strings"
	"testing"
	"testing/iotest"

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

// Package kitty writes the Kitty graphics protocol escapes that show the map
// image, asks the terminal whether it shows them, and reads the terminal's
// cell size in pixels.
package kitty

import (
	"bytes"
	"encoding/base64"
	"errors"
	"fmt"
	"io"
	"os"
	"regexp"
	"strings"
	"time"

	"golang.org/x/sys/unix"
)

// Clear deletes every image placement, quietly.
const Clear = "\x1b_Ga=d,d=A,q=2\x1b\\"

const chunk = 4096

// Place replaces any shown image with a PNG scaled to fill cols×rows cells
// from the top-left corner, leaving the cursor where it was.
func Place(png []byte, cols, rows int) string {
	data := base64.StdEncoding.EncodeToString(png)
	var b strings.Builder
	b.WriteString(Clear)
	b.WriteString("\x1b7\x1b[1;1H") // save the cursor, go home
	for i := 0; i < len(data); i += chunk {
		end, more := min(i+chunk, len(data)), 1
		if end == len(data) {
			more = 0
		}
		if i == 0 {
			// C=1: don't move the cursor; q=2: no replies to confuse the TUI.
			fmt.Fprintf(&b, "\x1b_Ga=T,f=100,q=2,C=1,c=%d,r=%d,m=%d;%s\x1b\\", cols, rows, more, data[i:end])
		} else {
			fmt.Fprintf(&b, "\x1b_Gm=%d;%s\x1b\\", more, data[i:end])
		}
	}
	b.WriteString("\x1b8") // restore the cursor
	return b.String()
}

// CellSize returns a cell's size in pixels, from TIOCGWINSZ on fd. ok is
// false when the terminal doesn't report pixel sizes.
func CellSize(fd int, cols, rows int) (w, h int, ok bool) {
	ws, err := unix.IoctlGetWinsize(fd, unix.TIOCGWINSZ)
	if err != nil || ws.Xpixel == 0 || ws.Ypixel == 0 || cols <= 0 || rows <= 0 {
		return 0, 0, false
	}
	return int(ws.Xpixel) / cols, int(ws.Ypixel) / rows, true
}

// Query asks whether the terminal shows Kitty graphics: a 1×1 image query
// that stores nothing, then a primary device attributes (DA1) request. A
// terminal answers the query, if it can, before DA1, which every terminal
// answers.
const Query = "\x1b_Gi=31,s=1,v=1,a=q,t=d,f=24;AAAA\x1b\\" + "\x1b[c"

var (
	queryOK = []byte("\x1b_Gi=31;OK\x1b\\")
	da1     = regexp.MustCompile(`\x1b\[\?[0-9;]*c$`)
)

// Supported reads the terminal's replies to Query from r, up to the DA1
// reply or the end of r, and reports whether the image query was answered
// OK. It reads a byte at a time so nothing after the replies is consumed.
func Supported(r io.Reader) bool {
	var got []byte
	b := make([]byte, 1)
	for {
		n, err := r.Read(b)
		if n == 1 {
			got = append(got, b[0])
			if b[0] == 'c' && da1.Match(got) {
				break
			}
		}
		if err != nil {
			break
		}
	}
	return bytes.Contains(got, queryOK)
}

// Probe sends Query to the controlling terminal and waits up to timeout for
// its replies. It is false if there is no terminal.
func Probe(timeout time.Duration) bool {
	tty, err := os.OpenFile("/dev/tty", os.O_RDWR, 0)
	if err != nil {
		return false
	}
	defer tty.Close()
	fd := int(tty.Fd())
	old, err := unix.IoctlGetTermios(fd, getTermios)
	if err != nil {
		return false
	}
	// No echo or line buffering, and a read returns empty after 0.1s
	// without input, so the deadline is checked.
	raw := *old
	raw.Lflag &^= unix.ICANON | unix.ECHO
	raw.Cc[unix.VMIN], raw.Cc[unix.VTIME] = 0, 1
	if err := unix.IoctlSetTermios(fd, setTermios, &raw); err != nil {
		return false
	}
	defer unix.IoctlSetTermios(fd, setTermios, old)
	if _, err := tty.WriteString(Query); err != nil {
		return false
	}
	return Supported(&deadlineReader{tty, time.Now().Add(timeout)})
}

// deadlineReader retries empty reads until its deadline, then ends.
type deadlineReader struct {
	r        io.Reader
	deadline time.Time
}

func (d *deadlineReader) Read(b []byte) (int, error) {
	for {
		n, err := d.r.Read(b)
		if n > 0 || (err != nil && !errors.Is(err, io.EOF)) || time.Now().After(d.deadline) {
			return n, err
		}
	}
}

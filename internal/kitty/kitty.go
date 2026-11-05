// Package kitty writes the Kitty graphics protocol escapes that show the map
// image, and reads the terminal's cell size in pixels.
package kitty

import (
	"encoding/base64"
	"fmt"
	"strings"

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

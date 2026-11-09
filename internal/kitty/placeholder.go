package kitty

import (
	"fmt"
	"image"
	"strings"

	"github.com/charmbracelet/x/ansi"
	ckitty "github.com/charmbracelet/x/ansi/kitty"
)

// Images shown with Unicode placeholders are sent once, by ID, with a
// virtual placement. The text then prints placeholder cells where the image
// goes, so it scrolls and repaints as ordinary text. Clear doesn't remove
// virtual placements; Delete does.

// MaxCells is the most rows or columns a placeholder image can span: one
// per row and column diacritic.
const MaxCells = 297

// ID is the n-th image ID for placeholders, from 0. Its low byte is the
// cells' 256-colour foreground, so it is kept at 16 or above, clear of the
// colours a terminal theme remaps; its top byte, the cells' third
// diacritic, is never 0, clear of the small IDs other programs pick.
// Truecolour IDs are avoided as Bubble Tea may downsample the colour.
func ID(n int) int { return (1+n/240)<<24 | (16 + n%240) }

// Placeholders is a row of text for each of rows rows that shows image id
// over cols cells. Every cell carries its row, column and top-byte
// diacritics, so a cell repainted alone still shows its piece.
func Placeholders(id, cols, rows int) []string {
	top := string(ckitty.Diacritic(id >> 24 & 0xff))
	lines := make([]string, rows)
	for r := range lines {
		var b strings.Builder
		fmt.Fprintf(&b, "\x1b[38;5;%dm", id&0xff)
		for c := range cols {
			b.WriteRune(ckitty.Placeholder)
			b.WriteRune(ckitty.Diacritic(r))
			b.WriteRune(ckitty.Diacritic(c))
			b.WriteString(top)
		}
		b.WriteString("\x1b[39m")
		lines[r] = b.String()
	}
	return lines
}

// Transmit sends img as a PNG with image id, replacing any image with that
// id, and puts it in a virtual placement over cols×rows cells, for
// Placeholders to show. It is empty if img doesn't encode.
func Transmit(id int, img image.Image, cols, rows int) string {
	var b strings.Builder
	err := ckitty.EncodeGraphics(&b, img, &ckitty.Options{
		Action: ckitty.TransmitAndPut, Format: ckitty.PNG, Transmission: ckitty.Direct,
		Quiet: 2, ID: id, VirtualPlacement: true, Columns: cols, Rows: rows, Chunk: true,
	})
	if err != nil {
		return ""
	}
	return b.String()
}

// Delete deletes image id, its placements and its data, quietly.
func Delete(id int) string {
	o := &ckitty.Options{Action: ckitty.Delete, Delete: ckitty.DeleteID, DeleteResources: true, ID: id, Quiet: 2}
	return ansi.KittyGraphics(nil, o.Options()...)
}

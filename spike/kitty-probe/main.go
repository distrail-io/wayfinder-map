// Throwaway: draws coloured boxes + lines to a PNG and shows it via the Kitty graphics protocol.
package main

import (
	"bytes"
	"encoding/base64"
	"fmt"
	"image"
	"image/color"
	"image/draw"
	"image/png"
	"os"
)

func box(img *image.RGBA, x, y, w, h int, c color.RGBA) {
	draw.Draw(img, image.Rect(x, y, x+w, y+h), &image.Uniform{c}, image.Point{}, draw.Src)
}

func main() {
	img := image.NewRGBA(image.Rect(0, 0, 600, 260))
	box(img, 0, 0, 600, 260, color.RGBA{30, 30, 36, 255})
	box(img, 20, 30, 200, 60, color.RGBA{46, 125, 50, 255})   // done
	box(img, 20, 170, 200, 60, color.RGBA{21, 101, 192, 255}) // frontier
	box(img, 380, 100, 200, 60, color.RGBA{198, 40, 40, 255}) // blocked
	for x := 220; x < 380; x++ { // crude edges
		y1 := 60 + (x-220)*(130-60)/160
		y2 := 200 - (x-220)*(200-130)/160
		box(img, x, y1, 1, 3, color.RGBA{180, 180, 180, 255})
		box(img, x, y2, 1, 3, color.RGBA{180, 180, 180, 255})
	}
	var buf bytes.Buffer
	png.Encode(&buf, img)
	data := base64.StdEncoding.EncodeToString(buf.Bytes())
	for i := 0; i < len(data); i += 4096 {
		end, more := i+4096, 1
		if end >= len(data) {
			end, more = len(data), 0
		}
		if i == 0 {
			fmt.Printf("\x1b_Ga=T,f=100,m=%d;%s\x1b\\", more, data[i:end])
		} else {
			fmt.Printf("\x1b_Gm=%d;%s\x1b\\", more, data[i:end])
		}
	}
	fmt.Println("\n^ green / blue / red boxes with grey lines = Kitty graphics works in this Herdr pane")
	os.Stdin.Read(make([]byte, 1))
}

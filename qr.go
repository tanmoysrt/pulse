package main

import (
	"fmt"
	"strings"

	qrcode "github.com/skip2/go-qrcode"
)

// qrTerminal renders content as a scannable QR code for the terminal.
//
// It packs two module-rows per text line using the upper-half-block glyph
// (▀), coloring the top half with the foreground and the bottom half with the
// background. Colors are forced to black-on-white via explicit ANSI codes so
// the code scans on both light and dark terminals — the library's plain
// ToSmallString relies on the terminal's default colors, which inverts on dark
// backgrounds and many phone scanners then miss it.
func qrTerminal(content string) (string, error) {
	q, err := qrcode.New(content, qrcode.Medium)
	if err != nil {
		return "", err
	}
	bm := q.Bitmap() // includes the quiet-zone border; true = dark module

	const (
		blackFG, whiteFG = 30, 97
		blackBG, whiteBG = 40, 107
	)
	var b strings.Builder
	for y := 0; y < len(bm); y += 2 {
		for x := range bm[y] {
			top := bm[y][x]
			bottom := false
			if y+1 < len(bm) {
				bottom = bm[y+1][x]
			}
			fg, bg := whiteFG, whiteBG
			if top {
				fg = blackFG
			}
			if bottom {
				bg = blackBG
			}
			fmt.Fprintf(&b, "\x1b[%d;%dm▀", fg, bg)
		}
		b.WriteString("\x1b[0m\n")
	}
	return b.String(), nil
}

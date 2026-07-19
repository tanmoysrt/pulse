package main

import (
	"fmt"
	"strings"

	qrcode "github.com/skip2/go-qrcode"
)

// qrTerminal renders a scannable QR for the terminal, packing two module-rows
// per line with the ▀ half-block. Colors are forced black-on-white so it scans
// on dark terminals too (default-colored output inverts and phones miss it).
func qrTerminal(content string) (string, error) {
	q, err := qrcode.New(content, qrcode.Medium)
	if err != nil {
		return "", err
	}
	bm := q.Bitmap() // includes the quiet-zone border; true = dark module

	const (
		blackFG, whiteFG  = 30, 97
		blackBG, whiteBG  = 40, 107
		horizontalPadding = 5
	)
	var b strings.Builder
	for y := 0; y < len(bm); y += 2 {
		fmt.Fprintf(&b, "\x1b[%dm%*s", whiteBG, horizontalPadding, "")
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
		fmt.Fprintf(&b, "\x1b[%dm%*s", whiteBG, horizontalPadding, "")
		b.WriteString("\x1b[0m\n")
	}
	return b.String(), nil
}

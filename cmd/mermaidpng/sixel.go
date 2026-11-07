package main

import (
	"bufio"
	"context"
	"fmt"
	"image"
	"image/color"
	"image/color/palette"
	"io"
	"slices"
)

// sixelAlphaCutoff is the alpha below which a pixel is left unpainted
// (shows the terminal background) when the image has transparency.
const sixelAlphaCutoff = 0x80

// encodeSixel writes img as a DEC sixel image. Colours are reduced to the
// 216-colour web-safe palette with Floyd–Steinberg dithering; transparent
// pixels are left unpainted so the terminal background shows through.
func encodeSixel(ctx context.Context, w io.Writer, img image.Image) error {
	b := img.Bounds()
	width, height := b.Dx(), b.Dy()
	if width == 0 || height == 0 {
		return nil
	}

	idx, visible, transparent, err := quantize(ctx, img)
	if err != nil {
		return err
	}
	pal := color.Palette(palette.WebSafe)

	bw := bufio.NewWriter(w)
	p2 := 0 // 0: paint background with colour 0; 1: leave it transparent
	if transparent {
		p2 = 1
	}
	fmt.Fprintf(bw, "\x1bP0;%d;0q\"1;1;%d;%d", p2, width, height)
	for i, c := range pal {
		r, g, bl, _ := c.RGBA()
		fmt.Fprintf(bw, "#%d;2;%d;%d;%d", i, r*100/0xffff, g*100/0xffff, bl*100/0xffff)
	}

	// rows[ci] holds the sixel bits of colour ci for the current band; it
	// is filled in one pass over the band, so the cost is O(pixels), not
	// O(pixels × colours).
	var rows [256][]byte
	var used []int
	var inBand [256]bool
	for y0 := 0; y0 < height; y0 += 6 {
		if err := ctx.Err(); err != nil {
			return err
		}
		used = used[:0]
		for dy := 0; dy < 6 && y0+dy < height; dy++ {
			y := y0 + dy
			for x := 0; x < width; x++ {
				if !visible[y*width+x] {
					continue
				}
				ci := idx[y*width+x]
				if rows[ci] == nil {
					rows[ci] = make([]byte, width)
				}
				if !inBand[ci] {
					inBand[ci] = true
					used = append(used, int(ci))
				}
				rows[ci][x] |= 1 << dy
			}
		}
		slices.Sort(used) // palette order, for deterministic output
		for k, ci := range used {
			row := rows[ci]
			for x := range row {
				row[x] += '?'
			}
			if k > 0 {
				bw.WriteByte('$') // back to the start of the band
			}
			fmt.Fprintf(bw, "#%d", ci)
			writeRuns(bw, row)
			clear(row)
			inBand[ci] = false
		}
		bw.WriteByte('-') // next band
	}
	bw.WriteString("\x1b\\")
	return bw.Flush()
}

// cubeLevel maps a channel value to the nearest of the six web-safe levels
// (0x00, 0x33, …, 0xff).
func cubeLevel(v int) int { return min(5, max(0, (v+25)/51)) }

// quantize reduces img to the web-safe palette, whose index is
// r*36 + g*6 + b over a 6×6×6 cube, so the nearest colour is computed
// directly instead of searched for. Floyd–Steinberg error diffusion runs
// over visible pixels only; transparent pixels (alpha below
// sixelAlphaCutoff) are reported in visible. Partially transparent edge
// pixels keep their un-premultiplied colour.
func quantize(ctx context.Context, img image.Image) (idx []uint8, visible []bool, transparent bool, err error) {
	b := img.Bounds()
	width, height := b.Dx(), b.Dy()
	idx = make([]uint8, width*height)
	visible = make([]bool, width*height)
	// Error rows for the current and next line, 3 channels per pixel, with
	// one pixel of padding on each side.
	cur := make([]int32, 3*(width+2))
	next := make([]int32, 3*(width+2))
	rgba, _ := img.(*image.RGBA)
	for y := 0; y < height; y++ {
		if y%64 == 0 {
			if err := ctx.Err(); err != nil {
				return nil, nil, false, err
			}
		}
		clear(next)
		for x := 0; x < width; x++ {
			var c color.NRGBA
			if rgba != nil {
				o := rgba.PixOffset(b.Min.X+x, b.Min.Y+y)
				c = color.NRGBAModel.Convert(color.RGBA{rgba.Pix[o], rgba.Pix[o+1], rgba.Pix[o+2], rgba.Pix[o+3]}).(color.NRGBA)
			} else {
				c = color.NRGBAModel.Convert(img.At(b.Min.X+x, b.Min.Y+y)).(color.NRGBA)
			}
			if c.A < sixelAlphaCutoff {
				transparent = true
				continue
			}
			visible[y*width+x] = true
			e := 3 * (x + 1)
			var lv [3]int
			for ch, v := range [3]uint8{c.R, c.G, c.B} {
				want := min(255, max(0, int(v)+int(cur[e+ch])/16))
				lv[ch] = cubeLevel(want)
				diff := int32(want - lv[ch]*51)
				cur[e+3+ch] += diff * 7
				next[e-3+ch] += diff * 3
				next[e+ch] += diff * 5
				next[e+3+ch] += diff * 1
			}
			idx[y*width+x] = uint8(lv[0]*36 + lv[1]*6 + lv[2])
		}
		cur, next = next, cur
	}
	return idx, visible, transparent, nil
}

// writeRuns writes sixel characters with run-length compression, dropping
// a trailing run of empty columns.
func writeRuns(w *bufio.Writer, row []byte) {
	end := len(row)
	for end > 0 && row[end-1] == '?' {
		end--
	}
	for i := 0; i < end; {
		j := i + 1
		for j < end && row[j] == row[i] {
			j++
		}
		if n := j - i; n > 3 {
			fmt.Fprintf(w, "!%d%c", n, row[i])
		} else {
			for k := 0; k < n; k++ {
				w.WriteByte(row[i])
			}
		}
		i = j
	}
}

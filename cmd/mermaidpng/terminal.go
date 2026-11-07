package main

import (
	"bufio"
	"bytes"
	"context"
	"encoding/base64"
	"fmt"
	"image"
	"image/png"
	"io"
	"strings"
)

// Output formats for --out.
const (
	outPNG    = "png"
	outSixel  = "sixel"
	outITerm2 = "iterm2"
	outKitty  = "kitty"
	outAuto   = "auto"
)

// kittyChunk is the largest base64 payload per kitty graphics escape; the
// protocol requires chunks of at most 4096 bytes.
const kittyChunk = 4096

// tmuxSeqLimit is the largest single escape sequence sent through tmux.
// tmux discards passthrough sequences longer than its input buffer limit
// (1 MiB), so larger iTerm2 images are sent in multipart form.
const tmuxSeqLimit = 512 << 10

// iterm2Part is the base64 payload per FilePart in multipart form.
const iterm2Part = 256 << 10

// detectTerminal picks an inline image protocol from the environment.
// Terminals do not advertise sixel support in the environment, so sixel is
// the fallback.
func detectTerminal(getenv func(string) string) string {
	term, prog := getenv("TERM"), getenv("TERM_PROGRAM")
	switch {
	case getenv("KITTY_WINDOW_ID") != "", strings.Contains(term, "kitty"),
		prog == "ghostty", strings.Contains(term, "ghostty"):
		return outKitty
	case prog == "iTerm.app", prog == "WezTerm", getenv("LC_TERMINAL") == "iTerm2":
		return outITerm2
	}
	return outSixel
}

// encodePNG returns img as PNG bytes.
func encodePNG(img image.Image) ([]byte, error) {
	var buf bytes.Buffer
	if err := png.Encode(&buf, img); err != nil {
		return nil, err
	}
	return buf.Bytes(), nil
}

// encodeITerm2 writes img with the iTerm2 inline image protocol (OSC 1337),
// which WezTerm and others also understand.
func encodeITerm2(w io.Writer, img image.Image) error {
	data, err := encodePNG(img)
	if err != nil {
		return err
	}
	bw := bufio.NewWriter(w)
	fmt.Fprintf(bw, "\x1b]1337;File=inline=1;size=%d;width=%dpx;height=%dpx;preserveAspectRatio=1:",
		len(data), img.Bounds().Dx(), img.Bounds().Dy())
	bw.WriteString(base64.StdEncoding.EncodeToString(data))
	bw.WriteString("\a")
	return bw.Flush()
}

// encodeITerm2Multipart writes img with the multipart variant of the iTerm2
// protocol (MultipartFile, FilePart…, FileEnd; iTerm2 3.5 and later), which
// keeps every sequence short enough for tmux.
func encodeITerm2Multipart(w io.Writer, img image.Image) error {
	data, err := encodePNG(img)
	if err != nil {
		return err
	}
	bw := bufio.NewWriter(w)
	fmt.Fprintf(bw, "\x1b]1337;MultipartFile=inline=1;size=%d;width=%dpx;height=%dpx;preserveAspectRatio=1\a",
		len(data), img.Bounds().Dx(), img.Bounds().Dy())
	payload := base64.StdEncoding.EncodeToString(data)
	for payload != "" {
		n := min(iterm2Part, len(payload))
		fmt.Fprintf(bw, "\x1b]1337;FilePart=%s\a", payload[:n])
		payload = payload[n:]
	}
	bw.WriteString("\x1b]1337;FileEnd\a")
	return bw.Flush()
}

// encodeKitty writes img with the kitty graphics protocol as a PNG
// transmitted in chunks. q=2 suppresses the terminal's replies, which would
// otherwise appear as input after the program exits; it is repeated on every
// chunk because the protocol allows it there and terminals differ in whether
// they carry it over from the first one.
func encodeKitty(w io.Writer, img image.Image) error {
	data, err := encodePNG(img)
	if err != nil {
		return err
	}
	payload := base64.StdEncoding.EncodeToString(data)
	bw := bufio.NewWriter(w)
	for first := true; first || payload != ""; first = false {
		n := min(kittyChunk, len(payload))
		chunk := payload[:n]
		payload = payload[n:]
		more := 0
		if payload != "" {
			more = 1
		}
		if first {
			fmt.Fprintf(bw, "\x1b_Ga=T,f=100,q=2,m=%d;%s\x1b\\", more, chunk)
		} else {
			fmt.Fprintf(bw, "\x1b_Gq=2,m=%d;%s\x1b\\", more, chunk)
		}
	}
	return bw.Flush()
}

// resolveFormat checks format and replaces auto with a detected protocol.
func resolveFormat(format string, getenv func(string) string) (string, error) {
	switch format {
	case outAuto:
		return detectTerminal(getenv), nil
	case outPNG, outSixel, outITerm2, outKitty:
		return format, nil
	}
	return "", fmt.Errorf("unknown output format %q (want png, sixel, iterm2, kitty or auto)", format)
}

// inTmux reports whether output goes through tmux, which drops kitty APC
// and iTerm2 OSC 1337 sequences unless they are wrapped for passthrough.
func inTmux(getenv func(string) string) bool {
	return getenv("TMUX") != ""
}

// tmuxWrap wraps every escape sequence of a kitty or iTerm2 stream in a tmux
// passthrough sequence (ESC P tmux; … ESC \, with each ESC doubled). tmux
// 3.3 and later need "set -g allow-passthrough on" for these to arrive.
// Sixel is not wrapped: tmux 3.4 and later understand it natively.
func tmuxWrap(data []byte) []byte {
	var out bytes.Buffer
	for len(data) > 0 {
		end := sequenceEnd(data)
		out.WriteString("\x1bPtmux;")
		out.Write(bytes.ReplaceAll(data[:end], []byte("\x1b"), []byte("\x1b\x1b")))
		out.WriteString("\x1b\\")
		data = data[end:]
	}
	return out.Bytes()
}

// sequenceEnd returns the length of the escape sequence at the start of
// data: up to and including ST (ESC \) or BEL.
func sequenceEnd(data []byte) int {
	for i := 1; i < len(data); i++ {
		switch {
		case data[i] == '\a':
			return i + 1
		case data[i] == 0x1b && i+1 < len(data) && data[i+1] == '\\':
			return i + 2
		}
	}
	return len(data)
}

// writeImage writes img to w in format (already resolved by resolveFormat).
// ctx bounds the encoding time.
func writeImage(ctx context.Context, w io.Writer, img image.Image, format string, tmux bool) error {
	if format == outPNG {
		return png.Encode(w, img)
	}
	var buf bytes.Buffer
	var err error
	switch format {
	case outSixel:
		err = encodeSixel(ctx, &buf, img)
	case outITerm2:
		err = encodeITerm2(&buf, img)
		if err == nil && tmux && buf.Len() > tmuxSeqLimit {
			buf.Reset()
			err = encodeITerm2Multipart(&buf, img)
		}
	case outKitty:
		err = encodeKitty(&buf, img)
	default:
		return fmt.Errorf("unknown output format %q", format)
	}
	if err != nil {
		return err
	}
	if err := ctx.Err(); err != nil {
		return err
	}
	data := buf.Bytes()
	if tmux && format != outSixel {
		data = tmuxWrap(data)
	}
	if _, err := w.Write(data); err != nil {
		return err
	}
	// Move below the image so the shell prompt does not overwrite it.
	_, err = io.WriteString(w, "\n")
	return err
}

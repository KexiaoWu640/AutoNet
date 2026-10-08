//go:build ignore

// Generates the native multi-size Windows icon. Run: go run tools/make_icon.go
package main

import (
	"bytes"
	"encoding/binary"
	"math"
	"os"
)

func segment(x, y, ax, ay, bx, by float64) float64 {
	dx, dy := bx-ax, by-ay
	t := math.Max(0, math.Min(1, ((x-ax)*dx+(y-ay)*dy)/(dx*dx+dy*dy)))
	return math.Hypot(x-ax-t*dx, y-ay-t*dy)
}

func pixel(x, y float64) (float64, float64, float64, float64) {
	// Blue rounded tile with three connected nodes and a green check.
	qx, qy := math.Abs(x-.5)-.32, math.Abs(y-.5)-.32
	if math.Hypot(math.Max(qx, 0), math.Max(qy, 0))+math.Min(math.Max(qx, qy), 0) > .14 {
		return 0, 0, 0, 0
	}
	r, g, b := 25.+16*y, 84.+30*y, 170.+34*y
	white := segment(x, y, .28, .3, .28, .68) < .023 || segment(x, y, .28, .68, .7, .68) < .023
	for _, p := range [][2]float64{{.28, .3}, {.28, .68}, {.7, .68}} {
		if math.Hypot(x-p[0], y-p[1]) < .076 {
			white = true
		}
	}
	if white {
		r, g, b = 242, 249, 255
	}
	if segment(x, y, .49, .35, .58, .44) < .032 || segment(x, y, .58, .44, .78, .23) < .032 {
		r, g, b = 100, 239, 188
	}
	return r, g, b, 255
}

func bitmap(n int) []byte {
	var out bytes.Buffer
	put := func(v interface{}) {
		if err := binary.Write(&out, binary.LittleEndian, v); err != nil {
			panic(err)
		}
	}
	maskStride := ((n + 31) / 32) * 4
	put(uint32(40))
	put(int32(n))
	put(int32(n * 2))
	put(uint16(1))
	put(uint16(32))
	put(uint32(0))
	put(uint32(n*n*4 + maskStride*n))
	for i := 0; i < 4; i++ {
		put(uint32(0))
	}
	for y := n - 1; y >= 0; y-- {
		for x := 0; x < n; x++ {
			var r, g, b, a float64
			for sy := 0; sy < 4; sy++ {
				for sx := 0; sx < 4; sx++ {
					pr, pg, pb, pa := pixel((float64(x)+(float64(sx)+.5)/4)/float64(n), (float64(y)+(float64(sy)+.5)/4)/float64(n))
					r += pr * pa
					g += pg * pa
					b += pb * pa
					a += pa
				}
			}
			if a > 0 {
				r /= a
				g /= a
				b /= a
			}
			out.Write([]byte{byte(b), byte(g), byte(r), byte(a / 16)})
		}
	}
	out.Write(make([]byte, maskStride*n))
	return out.Bytes()
}

func main() {
	sizes := []int{16, 24, 32, 48, 64, 128}
	var out bytes.Buffer
	put := func(v interface{}) {
		if err := binary.Write(&out, binary.LittleEndian, v); err != nil {
			panic(err)
		}
	}
	put(uint16(0))
	put(uint16(1))
	put(uint16(len(sizes)))
	images := make([][]byte, len(sizes))
	offset := 6 + 16*len(sizes)
	for i, n := range sizes {
		images[i] = bitmap(n)
		out.Write([]byte{byte(n), byte(n), 0, 0})
		put(uint16(1))
		put(uint16(32))
		put(uint32(len(images[i])))
		put(uint32(offset))
		offset += len(images[i])
	}
	for _, im := range images {
		out.Write(im)
	}
	if err := os.MkdirAll("assets", 0755); err != nil {
		panic(err)
	}
	if err := os.WriteFile("assets/autonet.ico", out.Bytes(), 0644); err != nil {
		panic(err)
	}
}

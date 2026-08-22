// Генератор иконок WinDTT: перерисовывает «белую молнию» из сайдбара
// (bolt-полигон 24x24: M13 2 3 14h9l-1 8 10-12h-9l1-8z) в PNG/ICO.
//
// Запуск из корня репозитория:
//
//	go run ./tools/icon
//
// Результат:
//   - build/appicon.png         1024x1024, синий квадрат #2563eb + белая молния
//   - build/windows/icon.ico    многокадровый PNG-ico (256/64/48/32/16)
//   - build/tray_icon_mac.png   36x36 (18pt @2x для Retina), прозрачный фон + белая молния (template)
package main

import (
	"bytes"
	"encoding/binary"
	"image"
	"image/color"
	"image/png"
	"log"
	"os"
	"path/filepath"
	"runtime"
)

// boltPts — вершины молнии из SVG-пути, нормированные на 24x24 viewBox.
var boltPts = [][2]float64{
	{13.0 / 24, 2.0 / 24},
	{3.0 / 24, 14.0 / 24},
	{12.0 / 24, 14.0 / 24},
	{11.0 / 24, 22.0 / 24},
	{21.0 / 24, 10.0 / 24},
	{12.0 / 24, 10.0 / 24},
}

func main() {
	_, thisFile, _, _ := runtime.Caller(0)
	root := filepath.Join(filepath.Dir(thisFile), "..", "..")

	appIcon := renderIcon(1024, true)
	if err := os.WriteFile(filepath.Join(root, "build", "appicon.png"), encodePNG(appIcon), 0o644); err != nil {
		log.Fatalf("appicon.png: %v", err)
	}

	ico := buildICO([]int{256, 64, 48, 32, 16})
	if err := os.WriteFile(filepath.Join(root, "build", "windows", "icon.ico"), ico, 0o644); err != nil {
		log.Fatalf("icon.ico: %v", err)
	}

	trayMac := renderIcon(36, false)
	if err := os.WriteFile(filepath.Join(root, "build", "tray_icon_mac.png"), encodePNG(trayMac), 0o644); err != nil {
		log.Fatalf("tray_icon_mac.png: %v", err)
	}

	log.Printf("Иконки записаны в %s", filepath.Join(root, "build"))
}

// renderIcon рисует молнию (сплошную) поверх синего скруглённого квадрата,
// если drawBG=true. Возвращает NRGBA (straight alpha).
func renderIcon(size int, drawBG bool) *image.NRGBA {
	img := image.NewNRGBA(image.Rect(0, 0, size, size))

	// Молния в 24x24 занимает 18x20 → w=0.75, h=0.8333 от viewBox,
	// центр бокса молнии (и её центроид) = (0.5, 0.5).
	const boltH = 0.833333
	// Высота молнии ~58% холста (в логотипе — 20/36).
	s := 0.58 / boltH
	// Сдвиг, при котором центр молнии (0.5, 0.5) ложится в центр холста:
	// (0.5*s + o) == 0.5  →  o = 0.5 - 0.5*s.
	ox := 0.5 - 0.5*s
	oy := 0.5 - 0.5*s
	transform := func(nx, ny float64) (float64, float64) {
		return (nx*s + ox) * float64(size), (ny*s + oy) * float64(size)
	}

	pts := make([][2]float64, len(boltPts))
	for i, p := range boltPts {
		x, y := transform(p[0], p[1])
		pts[i] = [2]float64{x, y}
	}

	// Радиус скругления ~27.8% (10/36 как у .brand-logo).
	rad := 0.278 * float64(size)
	bg := color.NRGBA{0x25, 0x63, 0xeb, 0xff} // #2563eb
	bolt := color.NRGBA{0xff, 0xff, 0xff, 0xff}

	const ss = 4 // суперсэмплинг 4x4 для сглаживания
	for y := 0; y < size; y++ {
		for x := 0; x < size; x++ {
			var cr, cg, cb, ca int
			for sy := 0; sy < ss; sy++ {
				for sx := 0; sx < ss; sx++ {
					px := float64(x) + (float64(sx)+0.5)/float64(ss)
					py := float64(y) + (float64(sy)+0.5)/float64(ss)
					if pointInPolygon(pts, px, py) {
						cr += int(bolt.R)
						cg += int(bolt.G)
						cb += int(bolt.B)
						ca += int(bolt.A)
					} else if drawBG && pointInRoundRect(px, py, float64(size), rad) {
						cr += int(bg.R)
						cg += int(bg.G)
						cb += int(bg.B)
						ca += int(bg.A)
					}
				}
			}
			n := ss * ss
			img.SetNRGBA(x, y, color.NRGBA{uint8(cr / n), uint8(cg / n), uint8(cb / n), uint8(ca / n)})
		}
	}
	return img
}

func pointInPolygon(pts [][2]float64, x, y float64) bool {
	inside := false
	n := len(pts)
	for i, j := 0, n-1; i < n; j, i = i, i+1 {
		xi, yi := pts[i][0], pts[i][1]
		xj, yj := pts[j][0], pts[j][1]
		if (yi > y) != (yj > y) {
			if x < (xj-xi)*(y-yi)/(yj-yi)+xi {
				inside = !inside
			}
		}
	}
	return inside
}

func pointInRoundRect(x, y, size, rad float64) bool {
	l, t := rad, rad
	r, b := size-rad, size-rad
	if x >= l && x <= r {
		return true
	}
	if y >= t && y <= b {
		return true
	}
	for _, c := range [][2]float64{{l, t}, {r, t}, {l, b}, {r, b}} {
		dx, dy := x-c[0], y-c[1]
		if dx*dx+dy*dy <= rad*rad {
			return true
		}
	}
	return false
}

func encodePNG(img image.Image) []byte {
	var buf bytes.Buffer
	if err := png.Encode(&buf, img); err != nil {
		log.Fatalf("png: %v", err)
	}
	return buf.Bytes()
}

// buildICO собирает PNG-компрессированный ICO-контейнер (поддержан Vista+).
func buildICO(sizes []int) []byte {
	type frame struct {
		size int
		png  []byte
	}
	frames := make([]frame, 0, len(sizes))
	for _, s := range sizes {
		frames = append(frames, frame{s, encodePNG(renderIcon(s, true))})
	}

	var out bytes.Buffer
	binary.Write(&out, binary.LittleEndian, uint16(0)) // reserved
	binary.Write(&out, binary.LittleEndian, uint16(1)) // type: icon
	binary.Write(&out, binary.LittleEndian, uint16(len(frames)))

	offset := 6 + 16*len(frames)
	for _, f := range frames {
		w := byte(f.size & 0xff)
		if f.size >= 256 {
			w = 0
		}
		out.WriteByte(w)
		out.WriteByte(w)
		out.WriteByte(0)                                    // palette colors
		out.WriteByte(0)                                    // reserved
		binary.Write(&out, binary.LittleEndian, uint16(1))  // planes
		binary.Write(&out, binary.LittleEndian, uint16(32)) // bpp
		binary.Write(&out, binary.LittleEndian, uint32(len(f.png)))
		binary.Write(&out, binary.LittleEndian, uint32(offset))
		offset += len(f.png)
	}
	for _, f := range frames {
		out.Write(f.png)
	}
	return out.Bytes()
}

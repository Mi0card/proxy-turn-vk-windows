package main

import (
	_ "embed"
	"encoding/binary"
	"errors"
)

// go:embed требует файл в пределах модуля; это же окно-иконка, что и у
// основного окна (build/windows/icon.ico). Из неё вытаскивается PNG-кадр
// для трея: на Windows собирается HICON, на macOS — NSImage.
//
//go:embed build/windows/icon.ico
var trayIconICO []byte

// Прозрачная «белая» молния для macOS-трея (SetTemplate(true) перекрашивает
// её под тему меню-бара — цвет роли не играет, важен только alpha-канал).
//
//go:embed build/tray_icon_mac.png
var trayIconMacPNGData []byte

// trayIconPNG извлекает PNG-кадр из ICO-контейнера, наиболее близкий по
// размеру к 32px (типичный размер иконки трея). Если кадров несколько —
// берётся ближайший; при отсутствии — самый крупный.
func trayIconPNG() ([]byte, error) {
	if len(trayIconICO) < 6 {
		return nil, errors.New("ico: файл слишком короткий")
	}
	count := int(binary.LittleEndian.Uint16(trayIconICO[4:6]))
	if count == 0 || len(trayIconICO) < 6+count*16 {
		return nil, errors.New("ico: повреждённый заголовок")
	}
	type frame struct {
		off, size int
		px        int
	}
	frames := make([]frame, 0, count)
	for i := 0; i < count; i++ {
		o := 6 + i*16
		w := int(trayIconICO[o])
		if w == 0 {
			w = 256
		}
		frames = append(frames, frame{
			off:  int(binary.LittleEndian.Uint32(trayIconICO[o+12:])),
			size: int(binary.LittleEndian.Uint32(trayIconICO[o+8:])),
			px:   w,
		})
	}
	best := 0
	for i, f := range frames {
		if abs(f.px-32) < abs(frames[best].px-32) {
			best = i
		}
	}
	f := frames[best]
	if uint64(f.off)+uint64(f.size) > uint64(len(trayIconICO)) {
		return nil, errors.New("ico: кадр обрезан")
	}
	return trayIconICO[f.off : f.off+f.size], nil
}

// trayIconMacPNG возвращает прозрачную молнию для macOS-трея.
func trayIconMacPNG() ([]byte, error) {
	if len(trayIconMacPNGData) == 0 {
		return nil, errors.New("нет macOS-иконки трея")
	}
	return trayIconMacPNGData, nil
}

func abs(x int) int {
	if x < 0 {
		return -x
	}
	return x
}

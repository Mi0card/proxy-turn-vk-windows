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

// trayIconPNG извлекает самый крупный PNG-кадр из ICO-контейнера.
// В текущем build/windows/icon.ico единственный кадр — 16x16 PNG.
func trayIconPNG() ([]byte, error) {
	if len(trayIconICO) < 6 {
		return nil, errors.New("ico: файл слишком короткий")
	}
	count := int(binary.LittleEndian.Uint16(trayIconICO[4:6]))
	if count == 0 || len(trayIconICO) < 6+count*16 {
		return nil, errors.New("ico: повреждённый заголовок")
	}
	best := 0
	bestSize := 0
	for i := 0; i < count; i++ {
		o := 6 + i*16
		w := int(trayIconICO[o])
		if w == 0 {
			w = 256
		}
		if w > bestSize {
			bestSize = w
			best = i
		}
	}
	o := 6 + best*16
	off := binary.LittleEndian.Uint32(trayIconICO[o+12:])
	size := binary.LittleEndian.Uint32(trayIconICO[o+8:])
	if uint64(off)+uint64(size) > uint64(len(trayIconICO)) {
		return nil, errors.New("ico: кадр обрезан")
	}
	return trayIconICO[off : off+size], nil
}

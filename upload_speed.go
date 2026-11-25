package main

import "strconv"

func uploadSpeedLabel(speed int) string {
	if speed <= 0 {
		return "Board default"
	}
	return strconv.Itoa(speed) + " baud"
}

func (m *model) openUploadSpeed() {
	if m.sketch == "" {
		m.notice = "Choose a sketch first."
		return
	}
	current := m.settings().UploadSpeed
	rows := []item{entry(uploadSpeedLabel(0), "Use the board package setting", "0")}
	selected := 0
	for _, speed := range []int{57600, 115200, 230400, 460800, 921600} {
		if speed == current {
			selected = len(rows)
		}
		rows = append(rows, entry(uploadSpeedLabel(speed), "", strconv.Itoa(speed)))
	}
	if current > 0 && selected == 0 {
		selected = len(rows)
		rows = append(rows, entry(uploadSpeedLabel(current), "Saved custom speed", strconv.Itoa(current)))
	}
	rows = append(rows, entry("Custom speed", "Enter a baud rate", "custom"))
	m.openPicker("upload-speed", "Upload Speed", rows)
	m.cursor = selected
}

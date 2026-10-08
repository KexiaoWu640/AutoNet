package main

import (
	"github.com/lxn/walk"
	. "github.com/lxn/walk/declarative"
)

func main() {
	var window *walk.MainWindow

	if err := (MainWindow{
		AssignTo: &window,
		Title:    "Walk Win7 Smoke Test",
		MinSize:  Size{Width: 420, Height: 180},
		Layout:   VBox{Margins: Margins{Left: 16, Top: 16, Right: 16, Bottom: 16}, Spacing: 10},
		Children: []Widget{
			Label{Text: "Walk GUI 初始化成功"},
			LineEdit{Text: "Windows 7 x64"},
			PushButton{Text: "确定"},
		},
	}).Create(); err != nil {
		walk.MsgBox(nil, "Walk Smoke Test", err.Error(), walk.MsgBoxIconError)
		return
	}

	window.Run()
}

package main

import (
	"os"
	"path/filepath"
	"strings"

	"github.com/lxn/walk"
)

func main() {
	if len(os.Args) == 2 {
		mode := strings.ToLower(strings.TrimSpace(os.Args[1]))
		if mode == "input-test" || mode == "input-test-safe" {
			RunInputTest(mode == "input-test-safe")
			return
		}
	}

	exe, err := os.Executable()
	if err != nil {
		walk.MsgBox(nil, "调网通", "无法确定程序目录："+err.Error(), walk.MsgBoxIconError)
		return
	}
	baseDir := filepath.Dir(exe)
	config, err := LoadConfig(filepath.Join(baseDir, "devices.csv"), filepath.Join(baseDir, "links.csv"))
	if err != nil {
		walk.MsgBox(nil, "调网通 - 配置错误", err.Error(), walk.MsgBoxIconError)
		return
	}
	if err := RunGUI(config, baseDir); err != nil {
		walk.MsgBox(nil, "调网通", "GUI启动失败："+err.Error(), walk.MsgBoxIconError)
	}
}

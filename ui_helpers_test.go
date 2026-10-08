package main

import (
	"github.com/lxn/walk"
	"os"
	"strings"
	"testing"
	"time"
)

func TestEmbeddedWindowIcon(t *testing.T) {
	icon, err := walk.NewIconFromResourceIdWithSize(2, walk.Size{Width: 48, Height: 48})
	if err != nil {
		t.Fatal(err)
	}
	icon.Dispose()
}

func TestFormatUIMessage(t *testing.T) {
	now := time.Date(2026, 10, 8, 12, 34, 56, 0, time.Local)
	got := FormatUIMessage("\r\n调网成功。\r\nIP：192.0.2.1\r\n", now)
	if !strings.HasPrefix(got, "12:34:56   调网成功。\r\n") || !strings.Contains(got, "IP：192.0.2.1") || strings.Contains(strings.ReplaceAll(got, "\r\n", ""), "\n") {
		t.Fatal(got)
	}
	if FormatUIMessage(" \r\n", now) != "" {
		t.Fatal("blank message not ignored")
	}
}

func TestLogCreatedReportsActualFile(t *testing.T) {
	var path string
	r := TaskRunner{LogDir: t.TempDir(), LogCreated: func(p string) { path = p }}
	logger, closeLog, err := r.newLogger()
	if err != nil {
		t.Fatal(err)
	}
	logger.Print("本次日志")
	closeLog()
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(data), "本次日志\r\n") {
		t.Fatal(string(data))
	}
}

func TestFormatTaskResult(t *testing.T) {
	r := TaskResult{
		Hops:        []Hop{{Device: Device{ID: "CORE", IP: "192.0.2.1"}, Port: "G1/0/1"}, {Device: Device{ID: "ACC", IP: "192.0.2.2"}, Port: "G1/0/5"}},
		FinalDevice: Device{ID: "ACC", IP: "192.0.2.2"}, FinalPort: "G1/0/5",
	}
	got := FormatTaskResult(r, 2500*time.Millisecond)
	for _, want := range []string{"调网成功", "接入设备：ACC (192.0.2.2)", "终端 IP：未获取", "CORE G1/0/1 → ACC G1/0/5", "耗时：2.5 秒"} {
		if !strings.Contains(got, want) {
			t.Fatalf("missing %q in %q", want, got)
		}
	}
}

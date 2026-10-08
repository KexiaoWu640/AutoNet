package main

import (
	"fmt"
	"strings"
	"time"
)

// FormatUIMessage changes presentation only; the full device log is untouched.
func FormatUIMessage(message string, now time.Time) string {
	message = strings.TrimSpace(strings.ReplaceAll(message, "\r\n", "\n"))
	if message == "" {
		return ""
	}
	lines := strings.Split(message, "\n")
	var out strings.Builder
	fmt.Fprintf(&out, "%s   %s\r\n", now.Format("15:04:05"), strings.TrimSpace(lines[0]))
	for _, line := range lines[1:] {
		if line = strings.TrimSpace(line); line != "" {
			fmt.Fprintf(&out, "               %s\r\n", line)
		}
	}
	out.WriteString("\r\n")
	return out.String()
}

func FormatTaskStart(d Device, mac string, vlan int, user string) string {
	return fmt.Sprintf("开始调网\n起始设备：%s\n目标 MAC：%s\nVLAN：%d\n登录用户：%s", DeviceLabel(d), mac, vlan, user)
}

func FormatTaskResult(r TaskResult, elapsed time.Duration) string {
	ip := r.IP
	if ip == "" {
		ip = "未获取"
	}
	path := make([]string, len(r.Hops))
	for i, h := range r.Hops {
		path[i] = h.Device.ID + " " + h.Port
	}
	lines := []string{
		r.StatusText(),
		"接入设备：" + DeviceLabel(r.FinalDevice),
		"接入端口：" + r.FinalPort,
		"终端 IP：" + ip,
	}
	if len(path) > 0 {
		lines = append(lines, "查找路径："+strings.Join(path, " → "))
	}
	lines = append(lines, fmt.Sprintf("耗时：%.1f 秒", elapsed.Seconds()))
	return strings.Join(lines, "\n")
}

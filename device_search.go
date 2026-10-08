package main

import (
	"fmt"
	"sort"
	"strings"
)

func DeviceLabel(d Device) string { return d.ID + " (" + d.IP + ")" }

func SearchDevices(devices map[string]Device, query string) []string {
	query = strings.ToLower(strings.TrimSpace(query))
	ids := []string{}
	for id, d := range devices {
		if strings.Contains(strings.ToLower(id), query) || strings.Contains(strings.ToLower(d.IP), query) || strings.EqualFold(DeviceLabel(d), query) {
			ids = append(ids, id)
		}
	}
	sort.Strings(ids)
	return ids
}

func ResolveDevice(devices map[string]Device, query string) (string, error) {
	query = strings.TrimSpace(query)
	for id, d := range devices {
		if strings.EqualFold(id, query) || d.IP == query || strings.EqualFold(DeviceLabel(d), query) {
			return id, nil
		}
	}
	matches := SearchDevices(devices, query)
	if query != "" && len(matches) == 1 {
		return matches[0], nil
	}
	if len(matches) == 0 {
		return "", fmt.Errorf("未找到设备，请检查 IP 或设备 ID")
	}
	return "", fmt.Errorf("请从匹配列表选择一台设备，或输入完整 IP / 设备 ID")
}

package main

import (
	"errors"
	"fmt"
	"net"
	"regexp"
	"strings"
)

var (
	ErrMACNotFound  = errors.New("MAC未找到")
	ErrPortNotFound = errors.New("无法从MAC表解析端口")
	macSeparatorRE  = regexp.MustCompile(`[-:.\s]`)
	macCharsRE      = regexp.MustCompile(`(?i)^[0-9a-f]{12}$`)
	portRE          = regexp.MustCompile(`(?i)\b(?:XGE\s*\d+(?:/\d+)+|GE\s*\d+(?:/\d+)+|GigabitEthernet\s*\d+(?:/\d+)+)\b`)
	accessGERe      = regexp.MustCompile(`(?i)^(?:GE|GigabitEthernet)\s*(\d+(?:/\d+)+)$`)
	ipv4RE          = regexp.MustCompile(`\b(?:\d{1,3}\.){3}\d{1,3}\b`)
)

func NormalizeMAC(input string) (string, error) {
	raw := strings.ToLower(macSeparatorRE.ReplaceAllString(strings.TrimSpace(input), ""))
	if !macCharsRE.MatchString(raw) {
		return "", fmt.Errorf("MAC地址必须包含12个十六进制字符")
	}
	return raw[0:4] + "-" + raw[4:8] + "-" + raw[8:12], nil
}

func NormalizePort(port string) string {
	p := strings.ToUpper(strings.ReplaceAll(strings.TrimSpace(port), " ", ""))
	return strings.Replace(p, "GIGABITETHERNET", "GE", 1)
}

func ParseMACPort(output, mac string) (string, error) {
	normalized, err := NormalizeMAC(mac)
	if err != nil {
		return "", err
	}
	foundMAC := false
	for _, line := range strings.Split(strings.ReplaceAll(output, "\r", ""), "\n") {
		lower := strings.ToLower(line)
		if !strings.Contains(lower, normalized) {
			continue
		}
		foundMAC = true
		match := portRE.FindString(line)
		if match != "" {
			return canonicalPort(match), nil
		}
	}
	if foundMAC {
		return "", ErrPortNotFound
	}
	return "", ErrMACNotFound
}

func canonicalPort(port string) string {
	p := strings.ToUpper(strings.ReplaceAll(strings.TrimSpace(port), " ", ""))
	if strings.HasPrefix(p, "GIGABITETHERNET") {
		return "GigabitEthernet" + p[len("GIGABITETHERNET"):]
	}
	return p
}

func HuaweiInterfaceName(port string) (string, error) {
	match := accessGERe.FindStringSubmatch(strings.TrimSpace(port))
	if len(match) != 2 {
		return "", fmt.Errorf("最终端口 %s 不是允许配置的用户端口", port)
	}
	return "GigabitEthernet " + match[1], nil
}

func FindARPEntry(output, mac string) (string, bool) {
	normalized, err := NormalizeMAC(mac)
	if err != nil {
		return "", false
	}
	for _, line := range strings.Split(strings.ReplaceAll(output, "\r", ""), "\n") {
		lower := strings.ToLower(line)
		if !strings.Contains(lower, normalized) || strings.Contains(lower, "display arp") {
			continue
		}
		ip := ipv4RE.FindString(line)
		if net.ParseIP(ip) != nil && !HasCLIError(line) {
			return ip, true
		}
	}
	return "", false
}

// Verify both the access VLAN and the actual static MAC table entry. A CLI
// response such as "MAC address already exists" alone is not proof of success.
func VerifyPortConfig(configOutput, macOutput, mac, port string, vlan int) bool {
	if HasCLIError(configOutput) || HasCLIError(macOutput) {
		return false
	}
	wantVLAN := fmt.Sprint(vlan)
	access := false
	interfaceFound, explicitVLAN, nonAccess := false, false, false
	for _, line := range strings.Split(configOutput, "\n") {
		line = strings.TrimSpace(line)
		if strings.HasPrefix(line, "interface ") && NormalizePort(strings.TrimPrefix(line, "interface ")) == NormalizePort(port) {
			interfaceFound = true
		}
		if strings.HasPrefix(line, "port access vlan ") {
			explicitVLAN = true
		}
		if line == "port link-type trunk" || line == "port link-type hybrid" {
			nonAccess = true
		}
		if strings.TrimSpace(line) == "port access vlan "+wantVLAN {
			access = true
		}
	}
	// Default access VLAN 1 can be omitted from current-configuration output.
	if vlan == 1 && !explicitVLAN {
		access = true
	}
	if !interfaceFound || !access || nonAccess {
		return false
	}
	for _, line := range strings.Split(macOutput, "\n") {
		fields := strings.Fields(line)
		if len(fields) < 3 || !strings.EqualFold(fields[0], mac) || fields[1] != wantVLAN {
			continue
		}
		foundPort, err := ParseMACPort(line, mac)
		if err != nil || NormalizePort(foundPort) != NormalizePort(port) {
			continue
		}
		for _, value := range fields[2:] {
			if strings.EqualFold(value, "static") || strings.EqualFold(value, "S") {
				return true
			}
		}
	}
	return false
}

func SaveSucceeded(output string) bool {
	lower := strings.ToLower(output)
	return !HasCLIError(output) && strings.Contains(lower, "success") &&
		(strings.Contains(lower, "saved") || strings.Contains(lower, "saving") || strings.Contains(lower, "save the configuration"))
}

func HasCLIError(output string) bool {
	lower := strings.ToLower(output)
	for _, marker := range []string{"error", "unrecognized", "incomplete", "wrong", "failed", "failure"} {
		if strings.Contains(lower, marker) {
			return true
		}
	}
	return false
}

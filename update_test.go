package main

import (
	"fmt"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
	"time"
)

func TestIPConfiguration(t *testing.T) {
	for _, scenario := range []string{"valid", "unknown-ip", "duplicate-ip", "missing-endpoint", "conflict"} {
		t.Run(scenario, func(t *testing.T) {
			dir := t.TempDir()
			devices := "id,ip,type\nCORE,192.168.0.10,h3c\nACCESS,192.168.0.30,h3c\n"
			links := "input_ip,port,output_ip\n192.168.0.10,XGE1/0/1,192.168.0.30\n"
			switch scenario {
			case "unknown-ip":
				links = strings.ReplaceAll(links, "192.168.0.30", "192.168.0.99")
			case "duplicate-ip":
				devices = strings.ReplaceAll(devices, "192.168.0.30", "192.168.0.10")
			case "missing-endpoint":
				links = "input_ip,port\n192.168.0.10,XGE1/0/1\n"
			case "conflict":
				links += "192.168.0.10,XGE1/0/1,192.168.0.10\n"
			}
			for name, content := range map[string]string{"devices.csv": devices, "links.csv": links} {
				if err := os.WriteFile(filepath.Join(dir, name), []byte(content), 0600); err != nil {
					t.Fatal(err)
				}
			}
			cfg, err := LoadConfig(filepath.Join(dir, "devices.csv"), filepath.Join(dir, "links.csv"))
			if scenario != "valid" {
				if err == nil {
					t.Fatal("expected validation error")
				}
				return
			}
			if err != nil {
				t.Fatal(err)
			}
			if cfg.Devices["CORE"].Name != "CORE" || cfg.Links["CORE|XGE1/0/1"] != "ACCESS" {
				t.Fatalf("wrong IP mapping: %+v", cfg)
			}
		})
	}
}

func TestDeviceSearch(t *testing.T) {
	d := map[string]Device{"CORE01": {ID: "CORE01", IP: "192.168.0.10"}, "CORE02": {ID: "CORE02", IP: "192.168.0.20"}, "ACCESS": {ID: "ACCESS", IP: "10.0.0.1"}}
	for query, want := range map[string][]string{"192.168.0.": {"CORE01", "CORE02"}, "core": {"CORE01", "CORE02"}, "  .20  ": {"CORE02"}, "none": {}, "": {"ACCESS", "CORE01", "CORE02"}} {
		if got := SearchDevices(d, query); !reflect.DeepEqual(got, want) {
			t.Fatalf("%q: %v want %v", query, got, want)
		}
	}
	for _, query := range []string{"core02", "192.168.0.20", ".20", "CORE02 (192.168.0.20)"} {
		id, err := ResolveDevice(d, query)
		if err != nil || id != "CORE02" {
			t.Fatalf("%q: %s %v", query, id, err)
		}
	}
	for _, query := range []string{"", "core", "missing"} {
		if _, err := ResolveDevice(d, query); err == nil {
			t.Fatalf("accepted ambiguous/missing %q", query)
		}
	}
}

func TestRepeatedTaskSkipsConfiguration(t *testing.T) {
	d := Device{ID: "ACCESS", Name: "ACCESS", IP: "192.168.0.30", Type: "h3c"}
	r := &TaskRunner{Config: &AppConfig{Devices: map[string]Device{"ACCESS": d}, Links: map[string]string{}}, Sleep: func(time.Duration) {}}
	configured, writes, saves := false, 0, 0
	r.RunCommands = func(_ Device, _, _ string, cmd []string) (string, error) {
		if len(cmd) > 1 {
			if configured {
				t.Fatal("repeated VLAN/static MAC write")
			}
			configured = true
			writes++
			return "<SW>", nil
		}
		switch {
		case strings.HasPrefix(cmd[0], "display current-configuration"):
			v := 2
			if configured {
				v = 9
			}
			return fmt.Sprintf("interface GigabitEthernet1/0/12\n port access vlan %d", v), nil
		case strings.HasPrefix(cmd[0], "display mac-address"):
			if configured {
				return "aabb-ccdd-eeff 9 Static GE1/0/12 N", nil
			}
			return "aabb-ccdd-eeff 2 Learned GE1/0/12 Y", nil
		case cmd[0] == "save force":
			saves++
			return "Saved successfully", nil
		case strings.HasPrefix(cmd[0], "display arp"):
			return "192.168.0.99 aabb-ccdd-eeff 20 D 9 GE1/0/1", nil
		}
		return "", fmt.Errorf("unexpected %v", cmd)
	}
	for i := 0; i < 2; i++ {
		r.LogDir = t.TempDir()
		var messages []string
		result, err := r.Run(TaskInput{StartDeviceID: "ACCESS", MAC: "aabbccddeeff", VLAN: 9, Username: "admin", Password: "test-only"}, func(s string) { messages = append(messages, s) })
		if err != nil {
			t.Fatal(err)
		}
		want := "调网成功"
		if i == 1 {
			want = "网络已调通"
		}
		if result.AlreadyConfigured != (i == 1) || result.StatusText() != want || !strings.Contains(strings.Join(messages, "\n"), want) {
			t.Fatalf("result %+v messages %v", result, messages)
		}
	}
	if writes != 1 || saves != 2 {
		t.Fatalf("writes=%d saves=%d", writes, saves)
	}
}

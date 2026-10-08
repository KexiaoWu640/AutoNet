package main

import (
	"context"
	"errors"
	"fmt"
	"io"
	"log"
	"strings"
	"testing"
	"time"
)

func TestCompleteTaskVerification(t *testing.T) {
	for _, scenario := range []string{"success", "bad-vlan", "bad-static", "save-error", "save-unconfirmed", "arp-echo", "arp-retry"} {
		t.Run(scenario, func(t *testing.T) {
			core := Device{ID: "CORE", Name: "核心", Type: "h3c"}
			access := Device{ID: "ACCESS", Name: "接入", Type: "h3c"}
			cfg := &AppConfig{Devices: map[string]Device{"CORE": core, "ACCESS": access}, Links: map[string]string{"CORE|XGE1/0/1": "ACCESS"}}
			arpCalls, saveCalls, accessMAC := 0, 0, 0
			r := &TaskRunner{Config: cfg, LogDir: t.TempDir(), Sleep: func(time.Duration) {}}
			r.RunCommands = func(d Device, _, _ string, commands []string) (string, error) {
				command := commands[0]
				if len(commands) > 1 {
					if d.ID != "ACCESS" {
						t.Fatal("configuration sent to core")
					}
					return "The MAC address already exists.\n<SW>", nil
				}
				switch {
				case strings.HasPrefix(command, "display current-configuration"):
					v := 9
					if scenario == "bad-vlan" {
						v = 2
					}
					return fmt.Sprintf("interface GigabitEthernet1/0/12\n port access vlan %d\n", v), nil
				case strings.HasPrefix(command, "display mac-address"):
					if d.ID == "CORE" {
						return "aabb-ccdd-eeff 9 Learned XGE1/0/1 Y", nil
					}
					accessMAC++
					kind := "Static"
					if scenario == "bad-static" && accessMAC > 1 {
						kind = "Learned"
					}
					return "aabb-ccdd-eeff 9 " + kind + " GE1/0/12 N", nil
				case command == "save force":
					saveCalls++
					if scenario == "save-error" {
						return "Failed to save", nil
					}
					if scenario == "save-unconfirmed" {
						return "<SW>", nil
					}
					return "Saved the current configuration to mainboard device successfully.\n<SW>", nil
				case strings.HasPrefix(command, "display arp"):
					arpCalls++
					if scenario == "arp-echo" || scenario == "arp-retry" && arpCalls == 1 {
						return command + "\n<CORE>", nil
					}
					return "192.168.0.99 aabb-ccdd-eeff 20 D 9 GE1/0/1", nil
				}
				t.Fatalf("unexpected command %s", command)
				return "", nil
			}
			result, err := r.Run(TaskInput{StartDeviceID: "CORE", MAC: "aabbccddeeff", VLAN: 9, Username: "admin", Password: "test-only"}, func(string) {})
			success := scenario == "success" || scenario == "arp-retry"
			if (err == nil) != success {
				t.Fatalf("err=%v", err)
			}
			if success && (result.IP != "192.168.0.99" || saveCalls != 1) {
				t.Fatalf("result=%+v saves=%d", result, saveCalls)
			}
			if (scenario == "bad-vlan" || scenario == "bad-static") && saveCalls != 0 {
				t.Fatal("saved invalid configuration")
			}
			if (scenario == "arp-retry" || scenario == "arp-echo") && arpCalls != 2 {
				t.Fatalf("ARP calls %d", arpCalls)
			}
		})
	}
}

func TestTraceTopology(t *testing.T) {
	config := &AppConfig{
		Devices: map[string]Device{
			"CORE01":   {ID: "CORE01", Name: "核心01", IP: "192.168.0.10", Type: "huawei"},
			"AGG01":    {ID: "AGG01", Name: "汇聚01", IP: "192.168.0.248", Type: "huawei"},
			"ACCESS01": {ID: "ACCESS01", Name: "接入01", IP: "192.168.0.44", Type: "huawei"},
		},
		Links: map[string]string{"CORE01|XGE1/3/0/5": "AGG01", "AGG01|XGE1/0/1": "ACCESS01"},
	}
	outputs := map[string]string{
		"CORE01":   "aabb-ccdd-eeff 9 XGE1/3/0/5 1052 D",
		"AGG01":    "aabb-ccdd-eeff 9 XGE1/0/1 1052 D",
		"ACCESS01": "aabb-ccdd-eeff 9 GE1/0/12 1052 D",
	}
	runner := &TaskRunner{Config: config, RunCommands: func(d Device, _, _ string, _ []string) (string, error) { return outputs[d.ID], nil }}
	result, err := runner.trace(config.Devices["CORE01"], "aabb-ccdd-eeff", "user", "password", func(string) {}, log.New(&strings.Builder{}, "", 0))
	if err != nil {
		t.Fatal(err)
	}
	if result.FinalDevice.ID != "ACCESS01" || result.FinalPort != "GE1/0/12" {
		t.Fatalf("final = %s/%s", result.FinalDevice.ID, result.FinalPort)
	}
	if len(result.Hops) != 3 {
		t.Fatalf("hops = %d", len(result.Hops))
	}
}

func TestCancelStopsNextHop(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	a := Device{ID: "A", IP: "192.0.2.1", Type: "h3c"}
	b := Device{ID: "B", IP: "192.0.2.2", Type: "h3c"}
	calls := 0
	r := &TaskRunner{Context: ctx, Config: &AppConfig{
		Devices: map[string]Device{"A": a, "B": b},
		Links:   map[string]string{"A|GE1/0/1": "B"},
	}, RunCommands: func(Device, string, string, []string) (string, error) {
		calls++
		cancel()
		return "aabb-ccdd-eeff 9 Learned GE1/0/1", nil
	}}
	_, err := r.trace(a, "aabb-ccdd-eeff", "admin", "password", func(string) {}, log.New(io.Discard, "", 0))
	if !errors.Is(err, context.Canceled) || calls != 1 {
		t.Fatalf("err=%v, command calls=%d", err, calls)
	}
}

func TestCancelInterruptsARPRetryDelay(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	r := &TaskRunner{Context: ctx}
	done := make(chan error, 1)
	go func() { done <- r.pause(time.Minute) }()
	cancel()
	select {
	case err := <-done:
		if !errors.Is(err, context.Canceled) {
			t.Fatal(err)
		}
	case <-time.After(time.Second):
		t.Fatal("retry delay did not stop")
	}
}

package main

import "testing"

func TestNormalizeMAC(t *testing.T) {
	inputs := []string{"aabbccddeeff", "AA-BB-CC-DD-EE-FF", "aa:bb:cc:dd:ee:ff", "aabb-ccdd-eeff"}
	for _, input := range inputs {
		got, err := NormalizeMAC(input)
		if err != nil {
			t.Fatalf("NormalizeMAC(%q): %v", input, err)
		}
		if got != "aabb-ccdd-eeff" {
			t.Errorf("NormalizeMAC(%q) = %q", input, got)
		}
	}
	if _, err := NormalizeMAC("aabbccddeeffZ"); err == nil {
		t.Fatal("invalid MAC character must be rejected")
	}
}

func TestParseMACPort(t *testing.T) {
	tests := []struct{ output, want string }{
		{"aabb-ccdd-eeff    9    XGE1/3/0/5    1052    D", "XGE1/3/0/5"},
		{"aabb-ccdd-eeff    9    Learned    GE1/0/12    Y", "GE1/0/12"},
		{"aabb-ccdd-eeff  6 Learned GigabitEthernet 1/0/12 Y", "GigabitEthernet1/0/12"},
	}
	for _, tc := range tests {
		got, err := ParseMACPort(tc.output, "aabb-ccdd-eeff")
		if err != nil || got != tc.want {
			t.Errorf("ParseMACPort() = %q, %v; want %q", got, err, tc.want)
		}
	}
}

func TestFindARPEntryIgnoresCommandEcho(t *testing.T) {
	if _, found := FindARPEntry("display arp | include aabb-ccdd-eeff\n<CORE01>", "aabb-ccdd-eeff"); found {
		t.Fatal("command echo must not be treated as an ARP entry")
	}
	ip, found := FindARPEntry("display arp | include aabb-ccdd-eeff\n192.168.0.99 aabb-ccdd-eeff GE1/0/1", "aabb-ccdd-eeff")
	if !found || ip != "192.168.0.99" {
		t.Fatalf("FindARPEntry = %q, %v", ip, found)
	}
}

func TestHuaweiInterfaceNameSafety(t *testing.T) {
	got, err := HuaweiInterfaceName("GE1/0/12")
	if err != nil || got != "GigabitEthernet 1/0/12" {
		t.Fatalf("HuaweiInterfaceName = %q, %v", got, err)
	}
	if _, err := HuaweiInterfaceName("XGE1/0/1"); err == nil {
		t.Fatal("XGE final port must be rejected")
	}
}

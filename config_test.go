package main

import (
	"bytes"
	"golang.org/x/text/encoding/simplifiedchinese"
	"golang.org/x/text/encoding/unicode"
	"os"
	"path/filepath"
	"testing"
)

func TestCSVEncodings(t *testing.T) {
	text := []byte("id,name,ip,type\r\nCORE,核心交换机,192.168.0.10,h3c\r\n")
	gbk, _ := simplifiedchinese.GBK.NewEncoder().Bytes(text)
	utf16, _ := unicode.UTF16(unicode.LittleEndian, unicode.UseBOM).NewEncoder().Bytes(text)
	for _, data := range [][]byte{text, append([]byte{0xef, 0xbb, 0xbf}, text...), gbk, utf16} {
		path := filepath.Join(t.TempDir(), "devices.csv")
		if err := os.WriteFile(path, data, 0600); err != nil {
			t.Fatal(err)
		}
		devices, err := loadDevices(path)
		if err != nil {
			t.Fatal(err)
		}
		if devices["CORE"].Name != "核心交换机" {
			t.Fatal(devices)
		}
	}
}

func TestSavedCredentialsRoundTrip(t *testing.T) {
	path := filepath.Join(t.TempDir(), "credentials.dat")
	want := []SavedCredential{{Name: "测试账号", Username: "operator", Password: "LOCAL-TEST-secret"}}
	if err := SaveCredentials(path, want); err != nil {
		t.Fatal(err)
	}
	raw, _ := os.ReadFile(path)
	if bytes.Contains(raw, []byte(want[0].Password)) {
		t.Fatal("plaintext password on disk")
	}
	got, err := LoadCredentials(path)
	if err != nil || len(got) != 1 || got[0] != want[0] {
		t.Fatalf("credential roundtrip failed: %v", err)
	}
}

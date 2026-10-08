package main

import (
	"bytes"
	"strings"
	"testing"
)

func TestPrepareCredentials(t *testing.T) {
	username, password := PrepareCredentials("  admin\r\n", "\tabc123 \r\n")
	if username != "admin" || password != "abc123" {
		t.Fatalf("PrepareCredentials() = %q, %q", username, password)
	}
}

func TestInputTestShowsRawAndTrimmedValues(t *testing.T) {
	var output bytes.Buffer
	if err := runInputTest(strings.NewReader(" admin \r\n\tabc123 \r\n"), &output, false); err != nil {
		t.Fatal(err)
	}
	text := output.String()
	for _, expected := range []string{
		`RawUsername=" admin \r\n"`,
		`TrimmedUsername="admin"`,
		`RawPassword="\tabc123 \r\n"`,
		`TrimmedPassword="abc123"`,
		"CR (\\r)",
		"LF (\\n)",
		"TAB (\\t)",
		"20 61 64 6D 69 6E 20 0D 0A",
	} {
		if !strings.Contains(text, expected) {
			t.Errorf("output does not contain %q\n%s", expected, text)
		}
	}
}

func TestInputTestSafeDoesNotPrintPassword(t *testing.T) {
	var output bytes.Buffer
	if err := runInputTest(strings.NewReader("admin\nsecret123\n"), &output, true); err != nil {
		t.Fatal(err)
	}
	text := output.String()
	if strings.Contains(text, "secret123") || strings.Contains(text, "73 65 63 72 65 74") {
		t.Fatalf("safe output leaked password: %s", text)
	}
	for _, expected := range []string{"PasswordLength=10", "TrimmedPasswordLength=9", "PasswordBytesLength=10"} {
		if !strings.Contains(text, expected) {
			t.Errorf("safe output does not contain %q", expected)
		}
	}
}

package main

import (
	"bufio"
	"fmt"
	"io"
	"os"
	"strings"
	"unicode/utf8"

	"golang.org/x/sys/windows"
)

var attachConsole = windows.NewLazySystemDLL("kernel32.dll").NewProc("AttachConsole")

const attachParentProcess = ^uint32(0)

// Set this to false and rebuild to force input-test to use safe output too.
const allowPlaintextPasswordDiagnostics = true

// RunInputTest performs local console input diagnostics only. It never creates
// an SSH client, opens a TCP connection, or sends a device command.
func RunInputTest(safe bool) {
	if !allowPlaintextPasswordDiagnostics {
		safe = true
	}
	in, out, closeConsole := diagnosticConsole()
	defer closeConsole()
	if err := runInputTest(in, out, safe); err != nil {
		fmt.Fprintf(out, "Input test failed: %v\n", err)
	}
}

func diagnosticConsole() (io.Reader, io.Writer, func()) {
	// Redirected handles and consoles inherited by the GUI process should be
	// used as-is. This also makes the mode scriptable for local diagnostics.
	if _, inErr := os.Stdin.Stat(); inErr == nil {
		if _, outErr := os.Stdout.Stat(); outErr == nil {
			return os.Stdin, os.Stdout, func() {}
		}
	}

	_, _, _ = attachConsole.Call(uintptr(attachParentProcess))
	in, inErr := os.OpenFile("CONIN$", os.O_RDONLY, 0)
	out, outErr := os.OpenFile("CONOUT$", os.O_WRONLY, 0)
	if inErr != nil || outErr != nil {
		if in != nil {
			_ = in.Close()
		}
		if out != nil {
			_ = out.Close()
		}
		return os.Stdin, os.Stdout, func() {}
	}
	return in, out, func() {
		_ = in.Close()
		_ = out.Close()
	}
}

func runInputTest(input io.Reader, output io.Writer, safe bool) error {
	reader := bufio.NewReader(input)
	fmt.Fprint(output, "Username: ")
	rawUsername, err := reader.ReadString('\n')
	if err != nil && err != io.EOF {
		return fmt.Errorf("read username: %w", err)
	}
	fmt.Fprint(output, "Password: ")
	rawPassword, passwordErr := reader.ReadString('\n')
	if passwordErr != nil && passwordErr != io.EOF {
		return fmt.Errorf("read password: %w", passwordErr)
	}

	username, password := PrepareCredentials(rawUsername, rawPassword)
	fmt.Fprintln(output)
	printValueDetails(output, "RawUsername", rawUsername, true)
	printValueDetails(output, "TrimmedUsername", username, true)

	if safe {
		fmt.Fprintf(output, "PasswordLength=%d\n", utf8.RuneCountInString(rawPassword))
		fmt.Fprintf(output, "TrimmedPasswordLength=%d\n", utf8.RuneCountInString(password))
		fmt.Fprintf(output, "PasswordBytesLength=%d\n", len([]byte(rawPassword)))
		return nil
	}

	printValueDetails(output, "RawPassword", rawPassword, true)
	printValueDetails(output, "TrimmedPassword", password, true)
	return nil
}

func printValueDetails(output io.Writer, name, value string, includeCharacters bool) {
	fmt.Fprintf(output, "%s=%q\n", name, value)
	fmt.Fprintf(output, "%sLength=%d\n", name, utf8.RuneCountInString(value))
	fmt.Fprintf(output, "%sBytesLength=%d\n", name, len([]byte(value)))
	fmt.Fprintf(output, "%s bytes:\n%s\n", name, bytesAsHex(value))
	if includeCharacters {
		fmt.Fprintf(output, "%s characters:\n", name)
		for byteIndex, character := range value {
			label := fmt.Sprintf("%q", character)
			switch character {
			case '\r':
				label = `CR (\r)`
			case '\n':
				label = `LF (\n)`
			case '\t':
				label = `TAB (\t)`
			case ' ':
				label = "SPACE"
			}
			encoded := []byte(string(character))
			fmt.Fprintf(output, "  byte[%d] U+%04X %-8s bytes=%s\n", byteIndex, character, label, bytesAsHexBytes(encoded))
		}
	}
	fmt.Fprintln(output)
}

func bytesAsHex(value string) string {
	return bytesAsHexBytes([]byte(value))
}

func bytesAsHexBytes(value []byte) string {
	if len(value) == 0 {
		return "(empty)"
	}
	parts := make([]string, len(value))
	for i, b := range value {
		parts[i] = fmt.Sprintf("%02X", b)
	}
	return strings.Join(parts, " ")
}

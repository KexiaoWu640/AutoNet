package main

import (
	"encoding/json"
	"os"
	"unsafe"

	"golang.org/x/sys/windows"
)

type SavedCredential struct {
	Name     string
	Username string
	Password string
}

// Windows protects the local file for the current user; no extra password,
// permission dialog, service or runtime is required (also available on Win7).
func credentialData(data []byte, decrypt bool) ([]byte, error) {
	in := windows.DataBlob{Size: uint32(len(data)), Data: &data[0]}
	var out windows.DataBlob
	var err error
	if decrypt {
		err = windows.CryptUnprotectData(&in, nil, nil, 0, nil, windows.CRYPTPROTECT_UI_FORBIDDEN, &out)
	} else {
		err = windows.CryptProtectData(&in, nil, nil, 0, nil, windows.CRYPTPROTECT_UI_FORBIDDEN, &out)
	}
	if err != nil {
		return nil, err
	}
	defer windows.LocalFree(windows.Handle(unsafe.Pointer(out.Data)))
	return append([]byte(nil), unsafe.Slice(out.Data, int(out.Size))...), nil
}

func LoadCredentials(path string) ([]SavedCredential, error) {
	data, err := os.ReadFile(path)
	if os.IsNotExist(err) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	if len(data) == 0 {
		return nil, nil
	}
	data, err = credentialData(data, true)
	if err != nil {
		return nil, err
	}
	var values []SavedCredential
	err = json.Unmarshal(data, &values)
	return values, err
}

func SaveCredentials(path string, values []SavedCredential) error {
	data, err := json.Marshal(values)
	if err != nil {
		return err
	}
	data, err = credentialData(data, false)
	if err != nil {
		return err
	}
	// Write the protected bytes next to the executable for easy local reuse.
	return os.WriteFile(path, data, 0600)
}

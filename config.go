package main

import (
	"bytes"
	"encoding/csv"
	"fmt"
	"io"
	"net"
	"os"
	"strings"
	"unicode/utf8"

	"golang.org/x/text/encoding/simplifiedchinese"
	"golang.org/x/text/encoding/unicode"
)

type Device struct {
	ID   string
	Name string
	IP   string
	Type string
}

type AppConfig struct {
	Devices map[string]Device
	Links   map[string]string
}

func LoadConfig(devicesPath, linksPath string) (*AppConfig, error) {
	devices, err := loadDevices(devicesPath)
	if err != nil {
		return nil, err
	}
	links, err := loadLinks(linksPath, devices)
	if err != nil {
		return nil, err
	}
	return &AppConfig{Devices: devices, Links: links}, nil
}

func openCSV(path string) (*csv.Reader, *os.File, error) {
	f, err := os.Open(path)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, nil, fmt.Errorf("配置文件不存在：%s", path)
		}
		return nil, nil, fmt.Errorf("无法读取配置文件 %s：%w", path, err)
	}
	data, err := io.ReadAll(f)
	if err != nil {
		f.Close()
		return nil, nil, err
	}
	if bytes.HasPrefix(data, []byte{0xff, 0xfe}) || bytes.HasPrefix(data, []byte{0xfe, 0xff}) {
		data, err = unicode.UTF16(unicode.LittleEndian, unicode.UseBOM).NewDecoder().Bytes(data)
	} else if !utf8.Valid(data) {
		data, err = simplifiedchinese.GB18030.NewDecoder().Bytes(data)
	}
	if err != nil || bytes.Contains(data, []byte("�")) {
		f.Close()
		return nil, nil, fmt.Errorf("配置文件编码损坏：%s，请重新填写乱码名称并另存为 UTF-8 CSV", path)
	}
	r := csv.NewReader(bytes.NewReader(bytes.TrimPrefix(data, []byte{0xef, 0xbb, 0xbf})))
	r.TrimLeadingSpace = true
	r.FieldsPerRecord = -1
	return r, f, nil
}

func readHeader(r *csv.Reader, expected []string, path string) (map[string]int, error) {
	record, err := r.Read()
	if err != nil {
		return nil, fmt.Errorf("配置文件 %s 缺少表头：%w", path, err)
	}
	index := make(map[string]int)
	for i, value := range record {
		value = strings.TrimPrefix(strings.TrimSpace(value), "\ufeff")
		index[strings.ToLower(value)] = i
	}
	for _, name := range expected {
		if _, ok := index[name]; !ok {
			return nil, fmt.Errorf("配置文件 %s 缺少字段：%s", path, name)
		}
	}
	return index, nil
}

func field(row []string, index map[string]int, name string) string {
	i, ok := index[name]
	if !ok || i >= len(row) {
		return ""
	}
	return strings.TrimSpace(row[i])
}

func loadDevices(path string) (map[string]Device, error) {
	r, f, err := openCSV(path)
	if err != nil {
		return nil, err
	}
	defer f.Close()
	idx, err := readHeader(r, []string{"id", "ip", "type"}, path)
	if err != nil {
		return nil, err
	}
	devices := make(map[string]Device)
	addresses := make(map[string]string)
	for line := 2; ; line++ {
		row, err := r.Read()
		if err == io.EOF {
			break
		}
		if err != nil {
			return nil, fmt.Errorf("配置文件 %s 第%d行格式错误：%w", path, line, err)
		}
		id := strings.ToUpper(field(row, idx, "id"))
		if id == "" && field(row, idx, "ip") == "" {
			continue
		}
		device := Device{ID: id, Name: field(row, idx, "name"), IP: field(row, idx, "ip"), Type: strings.ToLower(field(row, idx, "type"))}
		if device.ID == "" || net.ParseIP(device.IP) == nil || device.Type == "" {
			return nil, fmt.Errorf("devices.csv 第%d行设备信息无效", line)
		}
		if _, exists := devices[device.ID]; exists {
			return nil, fmt.Errorf("devices.csv 第%d行设备ID重复：%s", line, device.ID)
		}
		device.IP = net.ParseIP(device.IP).String()
		if old, exists := addresses[device.IP]; exists {
			return nil, fmt.Errorf("devices.csv 第%d行 IP 重复：%s（%s / %s）", line, device.IP, old, id)
		}
		addresses[device.IP] = id
		if device.Name == "" {
			device.Name = id
		}
		devices[device.ID] = device
	}
	if len(devices) == 0 {
		return nil, fmt.Errorf("devices.csv 中没有设备")
	}
	return devices, nil
}

func loadLinks(path string, devices map[string]Device) (map[string]string, error) {
	r, f, err := openCSV(path)
	if err != nil {
		return nil, err
	}
	defer f.Close()
	idx, err := readHeader(r, []string{"port"}, path)
	if err != nil {
		return nil, err
	}
	_, inputIP := idx["input_ip"]
	_, outputIP := idx["output_ip"]
	_, legacyDevice := idx["device"]
	_, legacyNext := idx["next_device"]
	if inputIP != outputIP || (!inputIP && !(legacyDevice && legacyNext)) {
		return nil, fmt.Errorf("links.csv 需要字段：input_ip,port,output_ip")
	}
	byIP := make(map[string]string)
	for id, device := range devices {
		byIP[device.IP] = id
	}
	links := make(map[string]string)
	for line := 2; ; line++ {
		row, err := r.Read()
		if err == io.EOF {
			break
		}
		if err != nil {
			return nil, fmt.Errorf("配置文件 %s 第%d行格式错误：%w", path, line, err)
		}
		deviceID := strings.ToUpper(field(row, idx, "device"))
		port := NormalizePort(field(row, idx, "port"))
		nextID := strings.ToUpper(field(row, idx, "next_device"))
		if inputIP {
			from, to := field(row, idx, "input_ip"), field(row, idx, "output_ip")
			if from == "" && to == "" && port == "" {
				continue
			}
			resolve := func(address string) (string, error) {
				ip := net.ParseIP(address)
				if ip != nil {
					if id, ok := byIP[ip.String()]; ok {
						return id, nil
					}
				}
				return "", fmt.Errorf("links.csv 第%d行 IP 无效或不在 devices.csv 中：%s", line, address)
			}
			deviceID, err = resolve(from)
			if err != nil {
				return nil, err
			}
			nextID, err = resolve(to)
			if err != nil {
				return nil, err
			}
		}
		if deviceID == "" && port == "" && nextID == "" {
			continue
		}
		if _, ok := devices[deviceID]; !ok {
			return nil, fmt.Errorf("拓扑配置错误：%s 不存在于 devices.csv（links.csv 第%d行）", deviceID, line)
		}
		if _, ok := devices[nextID]; !ok {
			return nil, fmt.Errorf("拓扑配置错误：%s 不存在于 devices.csv（links.csv 第%d行）", nextID, line)
		}
		if port == "" {
			return nil, fmt.Errorf("links.csv 第%d行端口为空", line)
		}
		key := deviceID + "|" + port
		if old, exists := links[key]; exists && old != nextID {
			return nil, fmt.Errorf("links.csv 第%d行链路冲突：%s", line, key)
		}
		links[key] = nextID
	}
	return links, nil
}

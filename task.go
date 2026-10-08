package main

import (
	"context"
	"errors"
	"fmt"
	"log"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"
)

const maxHops = 5

type TaskInput struct {
	StartDeviceID string
	MAC           string
	VLAN          int
	Username      string
	Password      string
}

type Hop struct {
	Device Device
	Port   string
}

type TaskResult struct {
	Hops              []Hop
	FinalDevice       Device
	FinalPort         string
	IP                string
	AlreadyConfigured bool
}

func (r TaskResult) StatusText() string {
	if r.AlreadyConfigured {
		return "网络已调通"
	}
	return "调网成功"
}

type CommandRunner func(Device, string, string, []string) (string, error)

type TaskRunner struct {
	Context     context.Context
	LogCreated  func(string)
	Config      *AppConfig
	RunCommands CommandRunner
	LogDir      string
	Sleep       func(time.Duration)
}

func NewTaskRunner(config *AppConfig, baseDir string) *TaskRunner {
	return NewTaskRunnerContext(context.Background(), config, baseDir)
}

func NewTaskRunnerContext(ctx context.Context, config *AppConfig, baseDir string) *TaskRunner {
	sshClient := NewSSHClient()
	return &TaskRunner{Context: ctx, Config: config, RunCommands: func(d Device, u, p string, commands []string) (string, error) {
		return sshClient.RunCommandsContext(ctx, d, u, p, commands)
	}, LogDir: filepath.Join(baseDir, "logs")}
}

func (r *TaskRunner) context() context.Context {
	if r.Context != nil {
		return r.Context
	}
	return context.Background()
}

func (r *TaskRunner) execute(d Device, u, p string, commands []string) (string, error) {
	if err := r.context().Err(); err != nil {
		return "", err
	}
	return r.RunCommands(d, u, p, commands)
}

func (r *TaskRunner) pause(duration time.Duration) error {
	if err := r.context().Err(); err != nil {
		return err
	}
	if r.Sleep != nil {
		r.Sleep(duration)
		return r.context().Err()
	}
	timer := time.NewTimer(duration)
	defer timer.Stop()
	select {
	case <-r.context().Done():
		return r.context().Err()
	case <-timer.C:
		return nil
	}
}

func (r *TaskRunner) Run(input TaskInput, emit func(string)) (TaskResult, error) {
	var result TaskResult
	if err := r.context().Err(); err != nil {
		return result, err
	}
	input.Username, input.Password = PrepareCredentials(input.Username, input.Password)
	mac, err := NormalizeMAC(input.MAC)
	if err != nil {
		return result, err
	}
	if input.VLAN < 1 || input.VLAN > 4094 {
		return result, fmt.Errorf("VLAN必须是1到4094之间的整数")
	}
	if input.Username == "" || input.Password == "" {
		return result, fmt.Errorf("SSH用户名和密码不能为空")
	}
	start, ok := r.Config.Devices[strings.ToUpper(input.StartDeviceID)]
	if !ok {
		return result, fmt.Errorf("起始设备不存在：%s", input.StartDeviceID)
	}
	logger, closeLog, err := r.newLogger()
	if err != nil {
		return result, fmt.Errorf("无法创建任务日志：%w", err)
	}
	defer closeLog()
	logger.Printf("任务开始\nMAC: %s\nVLAN: %d\n起始设备: %s %s", mac, input.VLAN, start.ID, start.IP)

	result, err = r.trace(start, mac, input.Username, input.Password, emit, logger)
	if err != nil {
		logger.Printf("任务失败: %v", err)
		return result, err
	}
	interfaceName, err := HuaweiInterfaceName(result.FinalPort)
	if err != nil {
		logger.Printf("安全检查失败: %v", err)
		return result, err
	}
	emit(fmt.Sprintf("\r\n最终接入口：\r\n%s / %s\r\n正在检查现有配置...", result.FinalDevice.ID, result.FinalPort))
	verifyCommands := []string{"display current-configuration interface " + interfaceName, "display mac-address " + mac}
	readConfig := func() ([]string, error) {
		outputs := make([]string, len(verifyCommands))
		for i, command := range verifyCommands {
			logger.Printf("验证命令: %s", command)
			var readErr error
			outputs[i], readErr = r.execute(result.FinalDevice, input.Username, input.Password, []string{command})
			logger.Printf("验证原始返回:\n%s\n技术错误: %v", outputs[i], readErr)
			if readErr != nil || HasCLIError(outputs[i]) {
				return nil, fmt.Errorf("读取配置失败：%s", result.FinalDevice.ID)
			}
		}
		return outputs, nil
	}
	existing, err := readConfig()
	if err != nil {
		return result, err
	}
	result.AlreadyConfigured = VerifyPortConfig(existing[0], existing[1], mac, result.FinalPort, input.VLAN)
	if result.AlreadyConfigured {
		emit("现有 VLAN / 静态 MAC 已符合要求，跳过重复配置，继续确认保存和 ARP。")
		logger.Print("现有配置符合要求，跳过 VLAN / 静态 MAC 配置命令")
	} else {
		emit(fmt.Sprintf("正在配置 VLAN %d...", input.VLAN))
		commands := []string{
			"system-view",
			"interface " + interfaceName,
			"port access vlan " + strconv.Itoa(input.VLAN),
			fmt.Sprintf("mac-address static %s vlan %d", mac, input.VLAN),
			"quit",
			"quit",
		}
		logger.Printf("最终设备: %s %s\n最终端口: %s", result.FinalDevice.ID, result.FinalDevice.IP, result.FinalPort)
		logger.Printf("发送配置命令:\n%s", strings.Join(commands, "\n"))
		output, err := r.execute(result.FinalDevice, input.Username, input.Password, commands)
		logger.Printf("配置原始返回:\n%s", output)
		if err != nil {
			logger.Printf("配置技术错误: %v", err)
			return result, fmt.Errorf("VLAN配置失败：%s", result.FinalDevice.Name)
		}
		if HasCLIError(output) {
			return result, fmt.Errorf("VLAN配置失败：设备返回错误")
		}
		emit("配置命令已执行，正在回读端口 VLAN / 静态 MAC...")
		verified, err := readConfig()
		if err != nil {
			return result, fmt.Errorf("配置已下发，但回读验证失败：%s", result.FinalDevice.ID)
		}
		if !VerifyPortConfig(verified[0], verified[1], mac, result.FinalPort, input.VLAN) {
			return result, fmt.Errorf("配置校验失败：端口 VLAN 或静态 MAC 与目标不一致，未执行保存")
		}
	}
	emit("端口 VLAN / 静态 MAC 校验通过。正在保存设备配置...")
	saveCommand := "save"
	if result.FinalDevice.Type == "h3c" {
		saveCommand = "save force"
	}
	logger.Printf("保存命令: %s", saveCommand)
	saveOutput, saveErr := r.execute(result.FinalDevice, input.Username, input.Password, []string{saveCommand})
	logger.Printf("保存原始返回:\n%s\n技术错误: %v", saveOutput, saveErr)
	if saveErr != nil || !SaveSucceeded(saveOutput) {
		return result, fmt.Errorf("配置已生效，但保存失败或未收到保存成功确认，请查看日志")
	}
	emit("配置已保存。正在回起始设备验证 ARP...")

	arpCommand := []string{"display arp | include " + mac}
	for attempt := 1; attempt <= 2; attempt++ {
		if err := r.pause(2 * time.Second); err != nil {
			return result, err
		}
		logger.Printf("ARP验证第%d次，发送命令: %s", attempt, arpCommand[0])
		arpOutput, arpErr := r.execute(start, input.Username, input.Password, arpCommand)
		logger.Printf("ARP原始返回（第%d次）:\n%s", attempt, arpOutput)
		if arpErr != nil {
			logger.Printf("ARP技术错误（第%d次）: %v", attempt, arpErr)
			continue
		}
		if ip, found := FindARPEntry(arpOutput, mac); found {
			result.IP = ip
			logger.Printf("ARP验证成功，IP: %s", ip)
			emit("\r\n" + result.StatusText() + "。\r\nIP：" + ip)
			logger.Print(result.StatusText())
			return result, nil
		}
	}
	logger.Printf("ARP验证失败")
	return result, fmt.Errorf("配置已校验并保存，但 ARP 验证失败；请确认终端在线并产生流量")
}

func (r *TaskRunner) trace(start Device, mac, username, password string, emit func(string), logger *log.Logger) (TaskResult, error) {
	result := TaskResult{}
	current := start
	for hop := 1; hop <= maxHops; hop++ {
		if current.Type != "huawei" && current.Type != "h3c" {
			return result, fmt.Errorf("设备类型暂不支持：%s（%s）", current.Type, current.Name)
		}
		emit(fmt.Sprintf("[%d/5] 正在查询 %s %s", hop, current.Name, current.IP))
		command := "display mac-address " + mac
		logger.Printf("第%d跳\n设备ID: %s\nIP: %s\n准备执行命令: %s", hop, current.ID, current.IP, command)
		output, err := r.execute(current, username, password, []string{command})
		logger.Printf("设备原始返回:\n%s", output)
		if err != nil {
			logger.Printf("SSH技术错误: %v", err)
			return result, fmt.Errorf("设备 %s 执行失败：%w", current.IP, err)
		}
		port, err := ParseMACPort(output, mac)
		if err != nil {
			if errors.Is(err, ErrMACNotFound) {
				return result, fmt.Errorf("MAC未找到：%s", current.Name)
			}
			return result, fmt.Errorf("无法从MAC表解析端口：%s", current.Name)
		}
		logger.Printf("识别端口: %s", port)
		emit("发现 MAC：" + port + "\r\n")
		result.Hops = append(result.Hops, Hop{Device: current, Port: port})
		nextID, exists := r.Config.Links[current.ID+"|"+NormalizePort(port)]
		if !exists {
			result.FinalDevice = current
			result.FinalPort = port
			return result, nil
		}
		if hop == maxHops {
			return result, fmt.Errorf("超过最大链路深度5")
		}
		current = r.Config.Devices[nextID]
	}
	return result, fmt.Errorf("超过最大链路深度5")
}

func (r *TaskRunner) newLogger() (*log.Logger, func(), error) {
	if err := os.MkdirAll(r.LogDir, 0755); err != nil {
		return nil, nil, err
	}
	name := time.Now().Format("20060102_150405.000") + ".log"
	f, err := os.OpenFile(filepath.Join(r.LogDir, name), os.O_CREATE|os.O_WRONLY|os.O_EXCL, 0644)
	if err != nil {
		return nil, nil, err
	}
	if r.LogCreated != nil {
		r.LogCreated(f.Name())
	}
	return log.New(windowsLogWriter{f}, "", log.Ldate|log.Ltime), func() { _ = f.Close() }, nil
}

// Win7 Notepad requires CRLF for readable multi-line logs.
type windowsLogWriter struct{ file *os.File }

func (w windowsLogWriter) Write(p []byte) (int, error) {
	text := strings.ReplaceAll(strings.ReplaceAll(string(p), "\r\n", "\n"), "\n", "\r\n")
	_, err := w.file.WriteString(text)
	if err != nil {
		return 0, err
	}
	return len(p), nil
}

package main

import (
	"bytes"
	"fmt"
	"io"
	"net"
	"strings"
	"sync"
	"time"

	"golang.org/x/crypto/ssh"
)

type SSHClient struct {
	ConnectTimeout time.Duration
	CommandTimeout time.Duration
}

type lockedBuffer struct {
	mu sync.Mutex
	b  bytes.Buffer
}

func (b *lockedBuffer) Write(p []byte) (int, error) {
	b.mu.Lock()
	defer b.mu.Unlock()
	return b.b.Write(p)
}
func (b *lockedBuffer) Len() int { b.mu.Lock(); defer b.mu.Unlock(); return b.b.Len() }
func (b *lockedBuffer) StringFrom(n int) string {
	b.mu.Lock()
	defer b.mu.Unlock()
	data := b.b.Bytes()
	if n < 0 || n > len(data) {
		n = 0
	}
	return string(append([]byte(nil), data[n:]...))
}

func NewSSHClient() *SSHClient {
	return &SSHClient{ConnectTimeout: 7 * time.Second, CommandTimeout: 10 * time.Second}
}

// PrepareCredentials is shared by the GUI task path and the local input
// diagnostic mode. These returned values are the ones passed to ssh.ClientConfig.
func PrepareCredentials(rawUsername, rawPassword string) (username, password string) {
	return strings.TrimSpace(rawUsername), strings.TrimSpace(rawPassword)
}

// 旧算法排在现代算法之后：新设备仍协商现代算法，只支持旧算法的老交换机也能握手。
var sshKeyExchanges = []string{
	"curve25519-sha256", "curve25519-sha256@libssh.org",
	"ecdh-sha2-nistp256", "ecdh-sha2-nistp384", "ecdh-sha2-nistp521",
	"diffie-hellman-group14-sha256", "diffie-hellman-group16-sha512",
	"diffie-hellman-group-exchange-sha256",
	"diffie-hellman-group14-sha1", "diffie-hellman-group-exchange-sha1", "diffie-hellman-group1-sha1",
}

var sshCiphers = []string{
	"aes128-gcm@openssh.com", "aes256-gcm@openssh.com", "chacha20-poly1305@openssh.com",
	"aes128-ctr", "aes192-ctr", "aes256-ctr",
	"aes128-cbc", "3des-cbc",
}

func newSSHClientConfig(username, password string, timeout time.Duration) *ssh.ClientConfig {
	return &ssh.ClientConfig{
		Config: ssh.Config{KeyExchanges: sshKeyExchanges, Ciphers: sshCiphers},
		User:   username,
		Auth: []ssh.AuthMethod{
			ssh.Password(password),
			// 部分交换机拒绝 password 方式但接受 keyboard-interactive，此时用同一密码回答所有提示。
			ssh.KeyboardInteractive(func(_, _ string, questions []string, _ []bool) ([]string, error) {
				answers := make([]string, len(questions))
				for i := range answers {
					answers[i] = password
				}
				return answers, nil
			}),
		},
		HostKeyCallback: ssh.InsecureIgnoreHostKey(),
		Timeout:         timeout,
	}
}

func (c *SSHClient) RunCommands(device Device, username, password string, commands []string) (string, error) {
	username, password = PrepareCredentials(username, password)
	address := net.JoinHostPort(device.IP, "22")
	conn, err := net.DialTimeout("tcp", address, c.ConnectTimeout)
	if err != nil {
		return "", fmt.Errorf("TCP连接失败：%w", err)
	}
	defer conn.Close()
	_ = conn.SetDeadline(time.Now().Add(c.ConnectTimeout))
	config := newSSHClientConfig(username, password, c.ConnectTimeout)
	sshConn, chans, reqs, err := ssh.NewClientConn(conn, address, config)
	if err != nil {
		return "", fmt.Errorf("SSH握手或认证失败：%w", err)
	}
	client := ssh.NewClient(sshConn, chans, reqs)
	defer client.Close()
	_ = conn.SetDeadline(time.Now().Add(c.CommandTimeout))
	session, err := client.NewSession()
	if err != nil {
		return "", fmt.Errorf("无法创建SSH会话：%w", err)
	}
	defer session.Close()
	stdin, err := session.StdinPipe()
	if err != nil {
		return "", fmt.Errorf("无法打开SSH输入：%w", err)
	}
	stdout, err := session.StdoutPipe()
	if err != nil {
		return "", fmt.Errorf("无法打开SSH输出：%w", err)
	}
	stderr, err := session.StderrPipe()
	if err != nil {
		return "", fmt.Errorf("无法打开SSH错误输出：%w", err)
	}
	var output lockedBuffer
	go func() { _, _ = io.Copy(&output, stdout) }()
	go func() { _, _ = io.Copy(&output, stderr) }()
	if err := session.RequestPty("vt100", 40, 120, ssh.TerminalModes{ssh.ECHO: 1}); err != nil {
		return "", fmt.Errorf("交换机拒绝Shell终端：%w", err)
	}
	if err := session.Shell(); err != nil {
		return "", fmt.Errorf("无法启动交换机Shell：%w", err)
	}
	if err := waitShellOutput(&output, 0, c.CommandTimeout); err != nil {
		return output.StringFrom(0), err
	}
	isH3C := device.Type == "h3c" || strings.Contains(strings.ToLower(output.StringFrom(0)), "h3c")
	pageCommand := "screen-length 0 temporary"
	if isH3C {
		pageCommand = "screen-length disable"
	}
	beforePage := output.Len()
	_ = conn.SetDeadline(time.Now().Add(c.CommandTimeout))
	if _, err := io.WriteString(stdin, pageCommand+"\n"); err != nil {
		return "", err
	}
	if err := waitShellOutput(&output, beforePage, c.CommandTimeout); err != nil {
		return "", err
	}
	if HasCLIError(output.StringFrom(beforePage)) {
		fallback := "screen-length disable"
		if isH3C {
			fallback = "screen-length 0 temporary"
		}
		beforePage = output.Len()
		_ = conn.SetDeadline(time.Now().Add(c.CommandTimeout))
		if _, err := io.WriteString(stdin, fallback+"\n"); err != nil {
			return "", err
		}
		if err := waitShellOutput(&output, beforePage, c.CommandTimeout); err != nil {
			return "", err
		}
	}
	start := output.Len()
	for _, command := range commands {
		timeout := c.CommandTimeout
		isSave := strings.HasPrefix(command, "save")
		if isSave {
			timeout = 60 * time.Second
			if isH3C {
				command = "save force"
			}
		}
		_ = conn.SetDeadline(time.Now().Add(timeout))
		before := output.Len()
		if _, err := io.WriteString(stdin, command+"\n"); err != nil {
			return output.StringFrom(start), fmt.Errorf("发送命令失败：%w", err)
		}
		if err := waitCommandOutput(&output, before, timeout, stdin, isSave); err != nil {
			return output.StringFrom(start), fmt.Errorf("命令超时（%s）：%w", command, err)
		}
		if HasCLIError(output.StringFrom(before)) {
			return output.StringFrom(start), fmt.Errorf("设备命令返回错误：%s", command)
		}
	}
	return output.StringFrom(start), nil
}

func waitShellOutput(output *lockedBuffer, start int, timeout time.Duration) error {
	return waitCommandOutput(output, start, timeout, nil, false)
}

func waitCommandOutput(output *lockedBuffer, start int, timeout time.Duration, stdin io.Writer, save bool) error {
	deadline := time.Now().Add(timeout)
	started := time.Now()
	lastLen := start
	stableSince := time.Now()
	repliedAt := -1
	for time.Now().Before(deadline) {
		time.Sleep(100 * time.Millisecond)
		current := output.Len()
		if current != lastLen {
			lastLen = current
			stableSince = time.Now()
		}
		if current > start {
			text := strings.TrimSpace(output.StringFrom(start))
			lastLine := text
			if index := strings.LastIndex(text, "\n"); index >= 0 {
				lastLine = strings.TrimSpace(text[index+1:])
			}
			if save && stdin != nil && current != repliedAt {
				lower := strings.ToLower(lastLine)
				if strings.Contains(lower, "[y/n]") || strings.Contains(lower, "(y/n)") {
					if _, err := io.WriteString(stdin, "y\n"); err != nil {
						return err
					}
					repliedAt = current
				} else if strings.Contains(lower, "file name") || strings.Contains(lower, "filename unchanged") {
					if _, err := io.WriteString(stdin, "\n"); err != nil {
						return err
					}
					repliedAt = current
				}
			}
			isPrompt := (strings.HasPrefix(lastLine, "<") && strings.HasSuffix(lastLine, ">")) ||
				(strings.HasPrefix(lastLine, "[") && strings.HasSuffix(lastLine, "]"))
			if text != "" && time.Since(started) >= 300*time.Millisecond && isPrompt && time.Since(stableSince) >= 150*time.Millisecond {
				return nil
			}
		}
	}
	return fmt.Errorf("等待设备返回超过 %s", timeout)
}

package main

import (
	"context"
	"fmt"
	"github.com/lxn/walk"
	. "github.com/lxn/walk/declarative"
	"io"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"time"
)

func RunGUI(config *AppConfig, baseDir string) error {
	var mw *walk.MainWindow
	var devices, accountsBox *walk.ComboBox
	var mac, vlan, username, password, accountName *walk.LineEdit
	var start, reload *walk.PushButton
	var status *walk.TextEdit
	var summary *walk.Label
	var accountPanel *walk.Composite
	var accountToggle, logButton *walk.PushButton
	latestLog := ""
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	var closing int32
	var taskDone <-chan struct{}
	var filterTimer *time.Timer
	var filterGeneration uint64
	var logWindow *walk.Dialog
	var uiMu sync.Mutex
	syncUI := func(f func()) {
		uiMu.Lock()
		defer uiMu.Unlock()
		if atomic.LoadInt32(&closing) != 0 {
			return
		}
		mw.Synchronize(func() {
			if atomic.LoadInt32(&closing) == 0 {
				f()
			}
		})
	}
	runner := NewTaskRunnerContext(ctx, config, baseDir)
	credentialPath := filepath.Join(baseDir, "credentials.dat")
	var accounts []SavedCredential
	var ids []string
	updatingDevices := false
	deviceNames := func(query string) []string {
		ids = SearchDevices(config.Devices, query)
		names := make([]string, len(ids))
		for i, id := range ids {
			d := config.Devices[id]
			names[i] = DeviceLabel(d)
		}
		return names
	}
	accountNames := func() []string {
		names := make([]string, len(accounts))
		for i, a := range accounts {
			names[i] = a.Name
		}
		return names
	}
	appendStatus := func(s string) {
		status.AppendText(FormatUIMessage(s, time.Now()))
	}
	showError := func(err error) { walk.MsgBox(mw, "调网通", err.Error(), walk.MsgBoxIconWarning) }
	// rsrc reserves ID 1 for the manifest and ID 2 for the first icon group.
	appIcon, err := walk.NewIconFromResourceIdWithSize(2, walk.Size{Width: 48, Height: 48})
	if err != nil {
		return fmt.Errorf("加载窗口图标：%w", err)
	}
	defer appIcon.Dispose()
	labelSize := Size{Width: 44}
	buttonSize := Size{Width: 88}
	groupLayout := VBox{Margins: Margins{Left: 10, Top: 8, Right: 10, Bottom: 10}}
	// Children go in a Composite: native ComboBoxes keep notifying the parent they
	// were created under, and GroupBox cannot route those to its re-parented children.
	gridLayout := Grid{Columns: 5, Spacing: 8, MarginsZero: true}
	err = (MainWindow{
		AssignTo: &mw, Title: "调网通", Icon: appIcon, Size: Size{Width: 680, Height: 640}, MinSize: Size{Width: 600, Height: 540},
		Background: SolidColorBrush{Color: walk.RGB(245, 247, 250)},
		Font:       Font{Family: "Microsoft YaHei", PointSize: 10},
		Layout:     VBox{Margins: Margins{Left: 16, Top: 12, Right: 16, Bottom: 14}, Spacing: 12},
		Children: []Widget{
			Composite{Layout: HBox{MarginsZero: true, Spacing: 10}, Children: []Widget{
				ImageView{Image: appIcon, Mode: ImageViewModeShrink, MinSize: Size{Width: 36, Height: 36}, MaxSize: Size{Width: 36, Height: 36}},
				Label{Text: "调网通", TextColor: walk.RGB(25, 52, 82), Font: Font{Family: "Microsoft YaHei", PointSize: 16, Bold: true}},
				HSpacer{},
			}},
			GroupBox{Title: "调网参数", Layout: groupLayout, Children: []Widget{Composite{Layout: gridLayout, Children: []Widget{
				Label{Text: "设备", MinSize: labelSize},
				ComboBox{AssignTo: &devices, ColumnSpan: 3, Editable: true, Model: deviceNames(""), CurrentIndex: 0, ToolTipText: "输入 IP 或设备 ID 筛选"},
				PushButton{AssignTo: &reload, Text: "重载 CSV", MinSize: buttonSize, MaxSize: buttonSize, OnClicked: func() {
					updated, err := LoadConfig(filepath.Join(baseDir, "devices.csv"), filepath.Join(baseDir, "links.csv"))
					if err != nil {
						showError(err)
						return
					}
					oldID := ""
					if i := devices.CurrentIndex(); i >= 0 && i < len(ids) {
						oldID = ids[i]
					}
					config = updated
					runner.Config = updated
					updatingDevices = true
					devices.SetModel(deviceNames(""))
					selected := 0
					for i, id := range ids {
						if id == oldID {
							selected = i
						}
					}
					devices.SetCurrentIndex(selected)
					updatingDevices = false
					filterGeneration++
					appendStatus(fmt.Sprintf("CSV 已重新加载：%d 台设备，%d 条链路", len(updated.Devices), len(updated.Links)))
				}},
				Label{Text: "MAC", MinSize: labelSize},
				LineEdit{AssignTo: &mac, CueBanner: "aabb-ccdd-eeff", StretchFactor: 3},
				Label{Text: "VLAN"},
				LineEdit{AssignTo: &vlan, ColumnSpan: 2, Text: "9", StretchFactor: 1},
			}}}},
			GroupBox{Title: "设备登录", Layout: groupLayout, Children: []Widget{Composite{Layout: gridLayout, Children: []Widget{
				Label{Text: "用户", MinSize: labelSize},
				LineEdit{AssignTo: &username, Text: "admin", StretchFactor: 1},
				Label{Text: "密码"},
				LineEdit{AssignTo: &password, PasswordMode: true, StretchFactor: 1},
				PushButton{AssignTo: &accountToggle, Text: "已存账号 ▾", Enabled: false, MinSize: buttonSize, MaxSize: buttonSize, OnClicked: func() {
					expanded := !accountPanel.Visible()
					accountPanel.SetVisible(expanded)
					if expanded {
						accountToggle.SetText("已存账号 ▴")
					} else {
						accountToggle.SetText("已存账号 ▾")
					}
				}},
				Composite{AssignTo: &accountPanel, ColumnSpan: 5, Visible: false, Layout: Grid{Columns: 3, MarginsZero: true, Spacing: 8}, Children: []Widget{
					Label{Text: "账号", MinSize: labelSize},
					ComboBox{AssignTo: &accountsBox, Model: accountNames(), StretchFactor: 1},
					PushButton{Text: "填入", MinSize: buttonSize, MaxSize: buttonSize, OnClicked: func() {
						i := accountsBox.CurrentIndex()
						if i < 0 || i >= len(accounts) {
							return
						}
						a := accounts[i]
						username.SetText(a.Username)
						password.SetText(a.Password)
						accountName.SetText(a.Name)
						appendStatus("已填入账号「" + a.Name + "」，用户：" + a.Username)
					}},
					Label{Text: "名称", MinSize: labelSize},
					LineEdit{AssignTo: &accountName, CueBanner: "如：办公网管理员", StretchFactor: 1},
					PushButton{Text: "保存", MinSize: buttonSize, MaxSize: buttonSize, OnClicked: func() {
						name := strings.TrimSpace(accountName.Text())
						u, p := PrepareCredentials(username.Text(), password.Text())
						if name == "" || u == "" || p == "" {
							showError(fmt.Errorf("请填写账号名称、用户名和密码"))
							return
						}
						updated := append([]SavedCredential(nil), accounts...)
						index := -1
						for i, a := range updated {
							if a.Name == name {
								index = i
							}
						}
						a := SavedCredential{Name: name, Username: u, Password: p}
						if index < 0 {
							index = len(updated)
							updated = append(updated, a)
						} else {
							updated[index] = a
						}
						if err := SaveCredentials(credentialPath, updated); err != nil {
							showError(err)
							return
						}
						accounts = updated
						accountsBox.SetModel(accountNames())
						accountsBox.SetCurrentIndex(index)
						appendStatus("账号「" + name + "」已保存")
					}},
				}},
			}}}},
			Composite{Layout: HBox{MarginsZero: true, Spacing: 8}, Children: []Widget{
				Label{AssignTo: &summary, Text: "准备就绪", TextColor: walk.RGB(25, 52, 82), Font: Font{Family: "Microsoft YaHei", PointSize: 11, Bold: true}},
				HSpacer{},
				PushButton{AssignTo: &start, Text: "开始调网", MinSize: Size{Width: 130, Height: 34}, MaxSize: Size{Width: 130, Height: 34}, OnClicked: func() {
					deviceID, err := ResolveDevice(config.Devices, devices.Text())
					if err != nil {
						showError(err)
						return
					}
					v, err := strconv.Atoi(strings.TrimSpace(vlan.Text()))
					if err != nil || v < 1 || v > 4094 {
						showError(fmt.Errorf("VLAN必须为1到4094"))
						return
					}
					m, err := NormalizeMAC(mac.Text())
					if err != nil {
						showError(err)
						return
					}
					u, p := PrepareCredentials(username.Text(), password.Text())
					if u == "" || p == "" {
						showError(fmt.Errorf("请输入用户名和密码"))
						return
					}
					input := TaskInput{StartDeviceID: deviceID, MAC: m, VLAN: v, Username: u, Password: p}
					status.SetText("")
					summary.SetText("正在调网，请稍候…")
					summary.SetTextColor(walk.RGB(37, 99, 180))
					latestLog = ""
					logButton.SetEnabled(false)
					start.SetEnabled(false)
					reload.SetEnabled(false)
					appendStatus(FormatTaskStart(config.Devices[deviceID], m, v, u))
					began := time.Now()
					done := make(chan struct{})
					taskDone = done
					go func() {
						defer close(done)
						result, runErr := runner.Run(input, func(s string) { syncUI(func() { appendStatus(s) }) })
						syncUI(func() {
							if runErr != nil {
								summary.SetText("未完成 · 请查看运行记录")
								summary.SetTextColor(walk.RGB(164, 66, 38))
								appendStatus(fmt.Sprintf("调网未完成\n原因：%s\n耗时：%.1f 秒", runErr.Error(), time.Since(began).Seconds()))
							} else {
								summary.SetText(result.StatusText() + " · " + result.FinalPort + " · " + result.IP)
								summary.SetTextColor(walk.RGB(24, 115, 76))
								appendStatus(FormatTaskResult(result, time.Since(began)))
							}
							if latestLog != "" {
								appendStatus("完整日志已保存：" + latestLog + "\n点击「查看日志」可在程序内打开")
							} else {
								appendStatus("本次未生成日志")
							}
							start.SetEnabled(true)
							reload.SetEnabled(true)
						})
					}()
				}},
			}},
			Composite{Layout: VBox{MarginsZero: true, Spacing: 6}, StretchFactor: 1, Children: []Widget{
				Composite{Layout: HBox{MarginsZero: true}, Children: []Widget{
					Label{Text: "运行记录", TextColor: walk.RGB(80, 95, 112), Font: Font{Family: "Microsoft YaHei", PointSize: 10, Bold: true}},
					HSpacer{},
					PushButton{AssignTo: &logButton, Text: "查看日志", MinSize: buttonSize, MaxSize: buttonSize, Enabled: false, OnClicked: func() {
						if latestLog == "" {
							return
						}
						path := latestLog
						go func() {
							text, err := readLogPreview(path)
							syncUI(func() {
								if err != nil {
									showError(err)
									return
								}
								if logWindow != nil && !logWindow.IsDisposed() {
									logWindow.Dispose()
								}
								err := (Dialog{AssignTo: &logWindow, Title: "运行日志", Size: Size{Width: 800, Height: 600}, Layout: VBox{}, Children: []Widget{
									TextEdit{ReadOnly: true, VScroll: true, Text: text},
								}}).Create(mw)
								if err != nil {
									showError(err)
									return
								}
								logWindow.Show()
							})
						}()
					}},
				}},
				TextEdit{AssignTo: &status, ReadOnly: true, VScroll: true, StretchFactor: 1, MinSize: Size{Height: 170}, TextColor: walk.RGB(47, 64, 82), Background: SolidColorBrush{Color: walk.RGB(255, 255, 255)}, Font: Font{Family: "Microsoft YaHei", PointSize: 10}},
			}},
		},
	}).Create()
	if err != nil {
		return err
	}
	runner.LogCreated = func(path string) {
		syncUI(func() {
			latestLog = path
			logButton.SetEnabled(true)
		})
	}
	devices.TextChanged().Attach(func() {
		if updatingDevices {
			return
		}
		filterGeneration++
		generation := filterGeneration
		if filterTimer != nil {
			filterTimer.Stop()
		}
		query := devices.Text()
		// A list selection is not a new search; preserve all current matches.
		if i := devices.CurrentIndex(); i >= 0 && i < len(ids) && query == DeviceLabel(config.Devices[ids[i]]) {
			return
		}
		from, to := devices.TextSelection()
		filterTimer = time.AfterFunc(150*time.Millisecond, func() {
			syncUI(func() {
				if generation != filterGeneration || devices.Text() != query {
					return
				}
				matches := SearchDevices(config.Devices, query)
				if sameDeviceIDs(ids, matches) {
					return
				}
				updatingDevices = true
				mw.SetSuspended(true)
				devices.SetModel(deviceNames(query))
				devices.SetCurrentIndex(-1)
				devices.SetText(query)
				mw.SetSuspended(false)
				devices.SetTextSelection(from, to)
				updatingDevices = false
				// Restore once after the queued layout, without a SizeChanged loop.
				syncUI(func() {
					if generation == filterGeneration && devices.Text() == query {
						devices.SetTextSelection(from, to)
					}
				})
			})
		})
	})
	devices.CurrentIndexChanged().Attach(func() {
		i := devices.CurrentIndex()
		if !updatingDevices && i >= 0 && i < len(ids) && devices.Text() == DeviceLabel(config.Devices[ids[i]]) {
			filterGeneration++
			if filterTimer != nil {
				filterTimer.Stop()
			}
		}
	})
	mw.Closing().Attach(func(_ *bool, _ walk.CloseReason) {
		uiMu.Lock()
		atomic.StoreInt32(&closing, 1)
		uiMu.Unlock()
		if filterTimer != nil {
			filterTimer.Stop()
		}
		cancel()
		if logWindow != nil && !logWindow.IsDisposed() {
			logWindow.Dispose()
		}
	})
	appendStatus(fmt.Sprintf("已加载 %d 台设备、%d 条链路", len(config.Devices), len(config.Links)))
	// DPAPI may wait for Windows profile services. Never block window creation.
	go func() {
		loaded, loadErr := LoadCredentials(credentialPath)
		syncUI(func() {
			accounts = loaded
			accountsBox.SetModel(accountNames())
			accountToggle.SetEnabled(true)
			if loadErr != nil {
				appendStatus("已存账号读取失败，请重新保存账号：" + loadErr.Error())
			} else {
				appendStatus(fmt.Sprintf("已读取 %d 个已存账号", len(accounts)))
			}
		})
	}()
	appendStatus("准备就绪。设备框可输入 IP 或设备 ID 筛选；修改 CSV 后点「重载 CSV」")
	mw.Run()
	cancel()
	if taskDone != nil {
		// Let the worker close its SSH socket and log; never hang exit.
		select {
		case <-taskDone:
		case <-time.After(time.Second):
		}
	}
	return nil
}

func readLogPreview(path string) (string, error) {
	f, err := os.Open(path)
	if err != nil {
		return "", fmt.Errorf("无法打开日志：%w", err)
	}
	defer f.Close()
	const limit = 1024 * 1024
	data, err := io.ReadAll(io.LimitReader(f, limit+1))
	if err != nil {
		return "", err
	}
	if len(data) > limit {
		return string(data[:limit]) + "\r\n（预览仅显示前1MB，完整内容在日志文件中）", nil
	}
	return string(data), nil
}

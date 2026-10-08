# 调网通 AutoNet

面向 Windows 7 SP1 x64 的交换机调网工具。输入终端 MAC 和目标 VLAN，沿配置好的交换机链路查找接入口，修改配置、保存并检查 ARP。

支持华三和华为，最多追踪 5 台设备。采用 Go 1.20.14 和 Walk，无需安装运行库。

## 快速使用

1. 将 `AutoNet.exe`、`devices.csv`、`links.csv` 放在同一文件夹。
2. 按现场情况填写两个 CSV，电脑需能访问交换机管理 IP，设备需开启 SSH。
3. 双击程序，在起始设备框输入部分 IP 或设备 ID，展开列表选择设备。
4. 填写 MAC、VLAN、SSH 用户名和密码，点击“开始调网”。

修改 CSV 后点击“重载 CSV”。输入完整 IP、完整设备 ID 或唯一匹配值时，也可直接开始。

窗口使用原生 Walk 界面，账号保存与调用默认折叠。运行记录带时间，任务结果用文字与颜色区分；日志生成后可点击“查看日志”，直接用记事本打开。运行中打开的日志不会自动刷新，结束后重新打开即可查看完整内容。

## 配置文件

仓库提供 [设备清单示例](examples/devices.csv) 和 [链路示例](examples/links.csv)。示例地址仅用于说明，使用前必须替换为现场地址。现场配置不纳入 Git。

### devices.csv

```csv
id,ip,type
CORE01,192.0.2.10,h3c
ACCESS01,192.0.2.30,h3c
```

- `id`：设备 ID，不可重复；不再需要 `name` 列。
- `ip`：管理 IP，不可重复。
- `type`：华三填 `h3c`，华为填 `huawei`。

### links.csv

```csv
input_ip,port,output_ip
192.0.2.10,XGE1/0/1,192.0.2.30
```

表示 `192.0.2.10` 的 `XGE1/0/1` 连接 `192.0.2.30`。`port` 是输入 IP 所在设备的端口，不是下一台设备端口。两端 IP 都必须在设备清单中。

只填写交换机间的链路，最终连接终端的接入口不要填入链路表。仍兼容旧版表头；支持 UTF-8、GBK/GB18030 和带 BOM 的 UTF-16。

## 结果含义

| 提示 | 含义 |
| --- | --- |
| 调网成功 | 本次修改后，配置回读、保存和 ARP 检查均通过 |
| 网络已调通 | 原配置已符合要求，跳过重复配置；保存和 ARP 检查均通过 |
| 保存失败或未收到成功确认 | 配置已生效，但未确认保存成功，请检查日志 |
| 配置已校验并保存，但 ARP 验证失败 | 配置和保存已完成，请检查终端在线并产生流量 |
| 配置校验失败 | 回读的 VLAN 或静态 MAC 不符，未执行保存 |
| 读取配置或回读验证失败 | 无法确认配置，请检查设备返回和日志 |
| VLAN配置失败 | 命令执行异常或设备拒绝命令 |
| SSH连接失败 | 检查网络、管理 IP、SSH 服务和账号密码 |
| MAC未找到或无法解析端口 | 检查 MAC、起始设备及设备返回的 MAC 表 |

“网络已调通”只代表本工具的配置、保存及 ARP 检查通过，不代表互联网或业务系统测试通过。

只在最终接入设备上执行配置和保存，其他设备只查询。华三使用 `save force`，华为使用 `save` 并处理确认提示；保存会写入设备整份当前运行配置。ARP 最多检查两次。

## 账号与日志

填好账号名称、用户名和密码后点击“添加 / 保存”；下次选择账号并点击“调用”。同名账号会更新。

账号保存在本机 `credentials.dat`，由当前 Windows 用户加密。换电脑或 Windows 用户后需重新保存。日志保存在 `logs/`，不记录密码；原始设备返回可能包含现场信息，请勿公开上传。

## 构建与测试

必须使用 **Go 1.20.14**，目标为 Windows amd64，禁用 CGO。依赖版本由 `go.mod` 和 `go.sum` 固定。

在 Windows 上运行：

```bat
build.bat
```

脚本检查 Go 版本，执行测试，再生成 `dist\AutoNet.exe`、`dist\AutoNet-debug.exe` 和 `dist\walk-smoke.exe`。已有现场 CSV 不会被覆盖；首次构建复制示例 CSV，使用前需修改。

仓库 `release/` 目录提供已编译好的正式版 `AutoNet.exe`，下载后与现场 `devices.csv`、`links.csv` 放在同一文件夹即可使用；同目录 `SHA256SUMS.txt` 给出该文件的 SHA-256 校验值。该二进制由 `build.bat` 用 Go 1.20.14 从本仓库源码编译，可执行文件内记录的 `vcs.revision` 与提交号一致，可据此核对来源。

也可手动执行：

```bat
set GOOS=windows
set GOARCH=amd64
set CGO_ENABLED=0
go test ./...
go build -trimpath -ldflags="-s -w -H windowsgui" -o dist\AutoNet.exe .
```

`AutoNet.manifest` 与已生成的 `rsrc.syso` 保留在源码中，Go 构建会嵌入 Common Controls 6 manifest。现场不需要额外的 manifest 文件或资源编译工具。

若修改 manifest，在开发机使用已安装的 `rsrc` 重新生成资源：

```bat
go run tools/make_icon.go
rsrc -arch amd64 -manifest AutoNet.manifest -ico assets/autonet.ico -o rsrc.syso
rsrc -arch amd64 -manifest AutoNet.manifest -o cmd\walk-smoke\rsrc.syso
```

`walk-smoke.exe` 只用于检查 Walk GUI 启动，不连接网络。

## 本地输入诊断

```bat
AutoNet-debug.exe input-test-safe
AutoNet-debug.exe input-test
```

两个模式均不连接网络。推荐 safe 模式，只输出密码长度；普通模式会明文输出密码及字符信息，仅限开发诊断。GUI 版也接受相同参数，命令行交互建议使用 debug 版。正式 GUI 直接运行 `AutoNet.exe`。

## 已知限制

- 仅允许在 GE/GigabitEthernet 最终端口配置；不会对 XGE、Eth-Trunk 等端口执行调网。
- 不扫描全网，追踪依赖正确的 CSV 链路。
- SSH 支持旧设备算法，当前不校验主机密钥；请在可信管理网络使用。
- 不同机型的命令和输出仍需现场确认。自动测试通过不等同于完成现场验收。

简明操作说明见 [Word 使用说明](docs/AutoNet使用说明.docx)。

# WakeGo

一个基于 Go 的局域网唤醒服务，内置 H5 页面，可直接查看设备、发送 Wake-on-LAN 魔术包，并在网页上维护设备配置。
![alt text](docs/localhost_9090_.png)
![h5demo](docs/localhost_9090_(iPhone 14 Pro Max).png)

## 功能

- 设备列表展示与一键唤醒
- 管理员口令保护的设备增删改
- 页面标题、默认唤醒端口、管理员口令在线修改
- 配置持久化到本地 JSON 文件，保存后立即生效
- 访问日志、鉴权失败日志、设备与配置变更日志
- 单二进制部署，无第三方依赖

## 目录

- `cmd/wakego`: 程序入口
- `internal/config`: 配置加载、校验、原子写入
- `internal/wol`: WOL 魔术包组装与发送
- `internal/applog`: 日志输出与文件落盘
- `internal/server`: HTTP 路由与页面渲染
- `internal/server/web/index.html`: 内置 H5 页面
- `scripts/deploy.sh`: 一键部署脚本

## 启动

```bash
go run ./cmd/wakego -addr :8080 -config ./config.json
```

首次启动时如果 `config.json` 不存在，会自动生成默认配置。

如果需要把日志落盘：

```bash
go run ./cmd/wakego -addr :9090 -config ./config.json -log-file ./logs/wakego.log
```

## 构建

```bash
go build -o wakego ./cmd/wakego
```

## 一键部署

默认用 `9090` 端口启动，适合本机调试：

```bash
./scripts/deploy.sh start
```

常用命令：

- `./scripts/deploy.sh start`
- `./scripts/deploy.sh stop`
- `./scripts/deploy.sh restart`
- `./scripts/deploy.sh status`
- `./scripts/deploy.sh logs`

如需修改端口，可以临时覆盖环境变量：

```bash
ADDR=:8088 ./scripts/deploy.sh restart
```

## 配置文件

配置文件路径通过 `-config` 指定，格式示例见 [config.example.json](./config.example.json)。

字段说明：

- `title`: 页面标题
- `admin_password`: 管理员口令
- `default_port`: 新设备默认 WOL 端口
- `devices`: 设备列表

## 管理方式

1. 打开首页，点击“管理员入口”
2. 输入管理员口令
3. 读取并修改基础配置或设备配置
4. 保存后立即写入本地 JSON 文件

管理接口使用请求头 `X-Admin-Password` 校验，建议仅在内网或 HTTPS 反向代理后暴露。

## API

- `GET /`: H5 页面
- `GET /api/devices`: 获取设备列表
- `POST /api/wake`: 发送唤醒请求，Body: `{"id":"pc-1"}`
- `GET /api/admin/config`: 读取配置，需要管理员口令
- `POST /api/admin/config/save`: 保存基础配置
- `POST /api/admin/device/save`: 新增或更新设备
- `POST /api/admin/device/delete`: 删除设备

## 部署建议

- 建议绑定在内网地址或放在反向代理后
- 调试阶段可以直接使用 `9090` 端口
- 若跨网段唤醒，需确认路由器支持定向广播
- 目标主机需提前启用 BIOS/UEFI、网卡驱动和交换机相关的 WOL 配置

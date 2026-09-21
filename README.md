# TarkovPilot Atlas

> 离线优先、自托管的 Escape from Tarkov 实时地图、撤离点与任务图谱助手。

TarkovPilot Atlas 通过游戏自己生成的截图文件名和日志识别地图、世界坐标与任务状态，
并把当前位置、撤离点、地图转场和任务目标显示在电脑或手机浏览器中。

它不会读取游戏进程内存，不注入游戏，不修改游戏文件，也不调用游戏私有接口。

## 主要能力

- 实时定位：监听 EFT 截图文件名中的世界坐标，通过 SSE 推送到地图页面。
- 移动端地图：支持单指拖动、双指缩放、跟随玩家和局域网手机访问。
- 地图内容：覆盖当前注册的 13 张底图，包括多层地图、撤离点和地图转场。
- 任务图谱：提供 PvP、PvE 独立任务目录、前置关系、Kappa、Lightkeeper 和剧情章节筛选。
- 任务状态：从公开游戏日志识别任务开始、失败和完成，并支持历史日志补录及手动纠正。
- 离线兜底：程序内置最后可用的内容和地图；自动更新失败不会破坏当前可用版本。
- 数据更新：后台从 Tarkov.dev 公开数据源刷新任务、地图特征、SVG、瓦片和静态参考图。
- 单文件运行：前端和内置数据嵌入 Go 可执行文件，Windows 双击即可启动。

## 快速开始

### 从源码构建

需要 Go 1.25.7+ 和 Node.js 20+：

```bash
cd mapapp/frontend
npm install
npm run build

cd ..
go build -o tarkovpilot-atlas .
./tarkovpilot-atlas
```

Windows 交叉编译：

```bash
cd mapapp
GOOS=windows GOARCH=amd64 CGO_ENABLED=0 go build -trimpath -ldflags "-s -w" -o tarkovpilot-atlas-windows-amd64.exe .
```

启动后，电脑访问 `http://127.0.0.1:8400`。手机与电脑连接同一局域网后，打开终端中显示的局域网地址。

游戏中请使用 EFT 自带截图键；Steam F12 截图文件名不包含定位坐标。

## 工作方式

```text
EFT 截图文件名 ─┐
                 ├─ 本地 Agent ─ HTTP ─ 自托管服务 ─ SSE ─ 浏览器/手机
EFT 公开日志 ────┘                         │
                                    本地状态与离线数据包
```

默认情况下，Agent、服务端和网页运行在同一个程序中。游戏电脑与地图服务器分离时，也可以单独运行 Agent。

## 项目结构

- [`mapapp/`](mapapp/)：TarkovPilot Atlas 主程序，包含 Go 服务端、Agent、React 前端和内置数据。
- [`mapapp/docs/research/`](mapapp/docs/research/)：数据来源、地图版本和能力边界的调研记录。
- [`mapapp/THIRD_PARTY_NOTICES.md`](mapapp/THIRD_PARTY_NOTICES.md)：地图、任务资料及其他第三方内容署名。
- [`app/`](app/)：原 TarkovPilot Wails 客户端的历史代码，不是当前产品入口。

完整命令、接口、校准方式和数据更新说明见 [`mapapp/README.md`](mapapp/README.md)。

## 数据与隐私

- 截图监听只读取文件名，不上传截图内容。
- 任务监听只解析游戏主动写入的日志，不读取内存。
- 默认数据、任务进度和校准参数均保存在本机。
- 局域网或公网部署时可配置摄入令牌；请勿在公网裸露未授权的服务端口。
- Tarkov.dev 数据只在版本包完整校验通过后原子切换，损坏更新不会覆盖最后可用版本。

## 开发与验证

```bash
cd mapapp
go test ./...

cd frontend
npm test
npm run build
```

提交涉及坐标投影、日志状态机或数据同步的改动时，请同时增加回归样本，确保已有地图和任务结果不漂移。

## 贡献

欢迎提交问题、数据修正和代码贡献。开始开发前请阅读 [`CONTRIBUTING.md`](CONTRIBUTING.md)。
安全问题请按照 [`SECURITY.md`](SECURITY.md) 私下报告，不要在公开 Issue 中披露可利用细节。

## 授权与免责声明

TarkovPilot Atlas 的原创软件代码采用 MIT License，具体范围和第三方例外见 [`LICENSE`](LICENSE)。
地图、游戏数据、名称和图片保留各自权利人的授权条件，详见
[`mapapp/THIRD_PARTY_NOTICES.md`](mapapp/THIRD_PARTY_NOTICES.md)。

本项目是非官方社区工具，与 Battlestate Games 或 Escape from Tarkov 官方没有关联，也未获得其背书。
使用者应自行遵守游戏服务条款及所在地法律法规。

---

**English:** TarkovPilot Atlas is an offline-first, self-hosted Escape from Tarkov live map,
extraction and quest companion. It reads only game-generated screenshot filenames and logs;
it does not access process memory, inject into the game, or modify game files.

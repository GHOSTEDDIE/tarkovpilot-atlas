// tarkovmap — self-hosted Escape from Tarkov position map.
//
// 双击运行（无参数）：启动服务器 + 内置 agent（自动监听截图目录），
// 并自动用浏览器打开地图页面。数据文件保存在 exe 同目录。
//
// 子命令（一般用不到）：
//
//	tarkovmap serve   [-addr :8400] [-data FILE] [-token T] [-svg-base URL] [-no-browser] [-agent=false] [-screenshots DIR] [-logs DIR]
//	tarkovmap agent   -server URL [-screenshots DIR] [-logs DIR] [-token T]   （游戏和服务器分开两台机器时用）
//	tarkovmap parse   <screenshot filename>                                   （调试图文件名解析）
//
// Windows 默认目录：截图为 %USERPROFILE%\Documents\Escape from Tarkov\Screenshots；
// 日志目录自动从常见安装路径检测，也可用 -logs 指定。
package main

import (
	"context"
	"flag"
	"fmt"
	"log"
	"net"
	"net/http"
	"os"
	"os/exec"
	"os/signal"
	"path/filepath"
	"runtime"
	"strings"
	"syscall"
	"time"

	"tarkovmap/internal/agent"
	"tarkovmap/internal/parser"
	"tarkovmap/internal/quests"
	"tarkovmap/internal/registry"
	"tarkovmap/internal/server"
	"tarkovmap/internal/store"
)

func main() {
	log.SetFlags(log.LstdFlags | log.Lmsgprefix)
	log.SetPrefix("[tarkovmap] ")

	if len(os.Args) < 2 {
		serveCmd(nil) // double-click: everything with defaults
		return
	}
	switch os.Args[1] {
	case "serve":
		serveCmd(os.Args[2:])
	case "agent":
		agentCmd(os.Args[2:])
	case "parse":
		parseCmd(os.Args[2:])
	default:
		if strings.HasPrefix(os.Args[1], "-") {
			serveCmd(os.Args[1:]) // flags without a subcommand = serve
			return
		}
		usage()
		os.Exit(2)
	}
}

func usage() {
	fmt.Fprintf(os.Stderr, `usage:
  tarkovmap            双击启动：服务器 + 内置 agent，自动打开浏览器
  tarkovmap serve [-addr :8400] [-data FILE] [-token T] [-svg-base URL] [-no-browser] [-agent=false]
  tarkovmap agent -server URL [-screenshots DIR] [-logs DIR] [-token T]
  tarkovmap parse "2025-12-20[02-09]-420.18, 1.00, 319.01-...png"
`)
}

func serveCmd(args []string) {
	fs := flag.NewFlagSet("serve", flag.ExitOnError)
	addr := fs.String("addr", ":8400", "listen address")
	data := fs.String("data", defaultDataFile(), "state/calibration data file")
	token := fs.String("token", "", "optional ingest token (X-Token header)")
	svgBase := fs.String("svg-base", server.DefaultSVGBaseURL, "base URL for map SVG files")
	noBrowser := fs.Bool("no-browser", false, "do not open the browser on start")
	embedAgent := fs.Bool("agent", true, "run the built-in screenshots/logs agent")
	screens := fs.String("screenshots", defaultScreenshotsDir(), "EFT screenshots folder (built-in agent)")
	logsDir := fs.String("logs", detectLogsDir(), "EFT Logs folder (built-in agent)")
	_ = fs.Parse(args)

	reg := registry.MustLoad()
	qr := quests.MustLoad()
	st := store.New(*data, reg)
	srv := server.New(st, reg, qr, server.Config{Token: *token, SVGBaseURL: *svgBase})

	httpSrv := &http.Server{Addr: *addr, Handler: srv.Handler()}
	ln, err := net.Listen("tcp", *addr)
	if err != nil {
		log.Fatalf("listen %s: %v", *addr, err)
	}
	go func() {
		if err := httpSrv.Serve(ln); err != nil && err != http.ErrServerClosed {
			log.Fatal(err)
		}
	}()

	port := ln.Addr().(*net.TCPAddr).Port
	localURL := fmt.Sprintf("http://127.0.0.1:%d", port)
	log.Printf("地图页面: %s （手机与电脑同一局域网，打开 http://%s:%d）", localURL, lanIP(), port)

	ctx, cancel := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer cancel()

	if *embedAgent {
		a := agent.New(agent.Config{
			ServerURL:      localURL,
			Token:          *token,
			ScreenshotsDir: *screens,
			LogsDir:        *logsDir,
		})
		go a.Run(ctx)
	}
	if !*noBrowser {
		go func() {
			time.Sleep(400 * time.Millisecond)
			openBrowser(localURL)
		}()
	}

	<-ctx.Done()
	_ = httpSrv.Shutdown(context.Background())
}

func agentCmd(args []string) {
	fs := flag.NewFlagSet("agent", flag.ExitOnError)
	serverURL := fs.String("server", "", "map server URL, e.g. http://192.168.1.10:8400")
	token := fs.String("token", "", "ingest token")
	screens := fs.String("screenshots", defaultScreenshotsDir(), "EFT screenshots folder")
	logsDir := fs.String("logs", detectLogsDir(), "EFT Logs folder (optional, enables auto map/quest detection)")
	_ = fs.Parse(args)

	if *serverURL == "" {
		log.Fatal("agent: -server is required")
	}
	if *screens == "" && *logsDir == "" {
		log.Fatal("agent: nothing to watch — set -screenshots and/or -logs")
	}

	a := agent.New(agent.Config{
		ServerURL:      *serverURL,
		Token:          *token,
		ScreenshotsDir: *screens,
		LogsDir:        *logsDir,
	})

	ctx, cancel := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer cancel()
	a.Run(ctx)
}

func parseCmd(args []string) {
	if len(args) != 1 {
		log.Fatal("parse: pass exactly one screenshot filename")
	}
	p, err := parser.Parse(args[0])
	if err != nil {
		log.Fatal(err)
	}
	fmt.Printf("date:     %s %s\n", p.Date, p.Time)
	fmt.Printf("raw xyz:  %.5f, %.5f, %.5f\n", p.World.X, p.World.Y, p.World.Z)
	fmt.Printf("quat:     %.5f, %.5f, %.5f, %.5f\n", p.Rotation.X, p.Rotation.Y, p.Rotation.Z, p.Rotation.W)
	fmt.Printf("suffix:   %s\n", p.Suffix)
	for _, order := range []string{"x,y,z", "y,z,x"} {
		cp := *p
		if err := cp.Remap(order); err == nil {
			fmt.Printf("as %-6s: x=%.2f y=%.2f z=%.2f\n", order, cp.World.X, cp.World.Y, cp.World.Z)
		}
	}
}

// defaultDataFile keeps state next to the executable, so a double-clicked
// exe always finds its calibration/quest data again.
func defaultDataFile() string {
	exe, err := os.Executable()
	if err != nil {
		return "mapapp-data.json"
	}
	return filepath.Join(filepath.Dir(exe), "mapapp-data.json")
}

func defaultScreenshotsDir() string {
	if runtime.GOOS == "windows" {
		if home, err := os.UserHomeDir(); err == nil {
			return filepath.Join(home, "Documents", "Escape from Tarkov", "Screenshots")
		}
	}
	return ""
}

// detectLogsDir probes common EFT install locations for the Logs folder.
func detectLogsDir() string {
	if runtime.GOOS != "windows" {
		return ""
	}
	candidates := []string{}
	for _, drive := range []string{"C", "D", "E", "F", "G"} {
		for _, p := range []string{
			`Battlestate Games\EFT\Logs`,
			`Battlestate Games\Escape from Tarkov\Logs`,
			`EFT\Logs`,
			`Escape from Tarkov\Logs`,
			`Games\EFT\Logs`,
			`Games\Escape from Tarkov\Logs`,
		} {
			candidates = append(candidates, drive+`:\`+p)
		}
	}
	for _, c := range candidates {
		if fi, err := os.Stat(c); err == nil && fi.IsDir() {
			return c
		}
	}
	return ""
}

func lanIP() string {
	addrs, err := net.InterfaceAddrs()
	if err != nil {
		return "<本机IP>"
	}
	for _, a := range addrs {
		if ipnet, ok := a.(*net.IPNet); ok && !ipnet.IP.IsLoopback() && ipnet.IP.To4() != nil {
			return ipnet.IP.String()
		}
	}
	return "<本机IP>"
}

func openBrowser(url string) {
	var err error
	switch runtime.GOOS {
	case "windows":
		// rundll32 avoids spawning a visible console window (unlike cmd /c start)
		err = exec.Command("rundll32", "url.dll,FileProtocolHandler", url).Start()
	case "darwin":
		err = exec.Command("open", url).Start()
	default:
		err = exec.Command("xdg-open", url).Start()
	}
	if err != nil {
		log.Printf("无法自动打开浏览器，请手动访问 %s (%v)", url, err)
	}
}

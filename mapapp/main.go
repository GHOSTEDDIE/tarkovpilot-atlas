// TarkovPilot Atlas — self-hosted Escape from Tarkov map and quest companion.
//
// 双击运行（无参数）：启动服务器 + 内置 agent（自动监听截图目录），
// 并自动用浏览器打开地图页面。数据文件保存在 exe 同目录。
//
// 子命令（一般用不到）：
//
//	tarkovpilot-atlas serve   [-addr :8400] [-data FILE] [-token T] [-svg-base URL] [-no-browser] [-agent=false] [-screenshots DIR] [-logs DIR]
//	tarkovpilot-atlas agent   -server URL [-screenshots DIR] [-logs DIR] [-token T]   （游戏和服务器分开两台机器时用）
//	tarkovpilot-atlas parse   <screenshot filename>                                   （调试图文件名解析）
//
// Windows 默认目录：截图为 %USERPROFILE%\Documents\Escape from Tarkov\Screenshots；
// 日志目录自动从常见安装路径检测，也可用 -logs 指定。
package main

import (
	"context"
	"encoding/json"
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
	"tarkovmap/internal/autoupdate"
	"tarkovmap/internal/content"
	"tarkovmap/internal/mapassets"
	"tarkovmap/internal/parser"
	"tarkovmap/internal/quests"
	"tarkovmap/internal/registry"
	"tarkovmap/internal/server"
	"tarkovmap/internal/store"
)

func main() {
	log.SetFlags(log.LstdFlags | log.Lmsgprefix)
	log.SetPrefix("[TarkovPilot Atlas] ")

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
	case "data":
		dataCmd(os.Args[2:])
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
  tarkovpilot-atlas            双击启动：服务器 + 内置 agent，自动打开浏览器
  tarkovpilot-atlas serve [-addr :8400] [-data FILE] [-token T] [-svg-base URL] [-auto-update=true] [-no-browser] [-agent=false]
  tarkovpilot-atlas agent -server URL [-screenshots DIR] [-logs DIR] [-token T]
  tarkovpilot-atlas agent replay-quests -server URL -logs DIR [-profile default] [-mode pvp|pve] [-wipe current]
  tarkovpilot-atlas data refresh [-file FILE] [-base-url URL] [-apply]
  tarkovpilot-atlas data maps-refresh [-dir DIR] [-svg-base URL]
  tarkovpilot-atlas data rollback [-file FILE]
  tarkovpilot-atlas parse "2025-12-20[02-09]-420.18, 1.00, 319.01-...png"
`)
}

func dataCmd(args []string) {
	if len(args) == 0 {
		log.Fatal("data: expected refresh or rollback")
	}
	switch args[0] {
	case "refresh":
		fs := flag.NewFlagSet("data refresh", flag.ExitOnError)
		file := fs.String("file", defaultContentFile(), "active content-pack file")
		baseURL := fs.String("base-url", content.DefaultUpstreamBaseURL, "tarkov.dev static JSON base URL")
		apply := fs.Bool("apply", false, "activate the validated pack (without this flag only preview the report)")
		_ = fs.Parse(args[1:])
		report, err := content.Refresh(context.Background(), content.RefreshOptions{
			BaseURL: *baseURL,
			Target:  *file,
			Apply:   *apply,
		})
		if err != nil {
			log.Fatal(err)
		}
		b, _ := json.MarshalIndent(report, "", "  ")
		fmt.Println(string(b))
		if !*apply {
			fmt.Println("预览完成；确认报告后追加 -apply 激活该数据包。")
		}
	case "rollback":
		fs := flag.NewFlagSet("data rollback", flag.ExitOnError)
		file := fs.String("file", defaultContentFile(), "active content-pack file")
		_ = fs.Parse(args[1:])
		if err := content.Rollback(*file); err != nil {
			log.Fatal(err)
		}
		fmt.Printf("已回滚内容包：%s\n", *file)
	case "maps-refresh":
		fs := flag.NewFlagSet("data maps-refresh", flag.ExitOnError)
		dir := fs.String("dir", defaultMapCacheDir(), "versioned runtime map-art cache")
		svgSource := fs.String("svg-base", mapassets.DefaultSVGBaseURL, "remote SVG source base URL")
		_ = fs.Parse(args[1:])
		report, err := mapassets.Refresh(context.Background(), registry.MustLoad(), mapassets.RefreshOptions{
			Root: *dir, SVGBaseURL: *svgSource,
		})
		if err != nil {
			log.Fatal(err)
		}
		b, _ := json.MarshalIndent(report, "", "  ")
		fmt.Println(string(b))
	default:
		log.Fatalf("data: unknown action %q", args[0])
	}
}

func serveCmd(args []string) {
	fs := flag.NewFlagSet("serve", flag.ExitOnError)
	addr := fs.String("addr", ":8400", "listen address")
	data := fs.String("data", defaultDataFile(), "state/calibration data file")
	contentFile := fs.String("content", defaultContentFile(), "versioned game-content pack")
	token := fs.String("token", "", "optional ingest token (X-Token header)")
	svgBase := fs.String("svg-base", server.DefaultSVGBaseURL, "base URL for map SVG files")
	noBrowser := fs.Bool("no-browser", false, "do not open the browser on start")
	embedAgent := fs.Bool("agent", true, "run the built-in screenshots/logs agent")
	screens := fs.String("screenshots", defaultScreenshotsDir(), "EFT screenshots folder (built-in agent)")
	logsDir := fs.String("logs", detectLogsDir(), "EFT Logs folder (built-in agent)")
	autoUpdate := fs.Bool("auto-update", true, "refresh game data and map art in the background")
	updateInterval := fs.Duration("update-interval", 24*time.Hour, "successful automatic update interval")
	updateRetry := fs.Duration("update-retry", time.Hour, "automatic update retry interval after failure")
	updateBaseURL := fs.String("update-base-url", content.DefaultUpstreamBaseURL, "tarkov.dev static JSON base URL")
	mapCache := fs.String("map-cache", defaultMapCacheDir(), "versioned runtime map-art cache")
	_ = fs.Parse(args)

	reg := registry.MustLoad()
	qr := quests.MustLoad()
	catalog, err := content.Load(*contentFile)
	if err != nil {
		log.Fatalf("content pack: %v", err)
	}
	mapDir, mapAssetVersion, mapErr := mapassets.LoadActive(*mapCache, reg)
	if mapErr != nil && !os.IsNotExist(mapErr) {
		log.Printf("地图资源缓存不可用，使用内置地图：%v", mapErr)
	}
	updateStateFile := *contentFile + ".update.json"
	updateStatus, statusErr := autoupdate.Load(updateStateFile)
	if statusErr != nil {
		log.Printf("自动更新状态不可读，将重新检查：%v", statusErr)
	}
	updateStatus.Enabled = *autoUpdate
	updateStatus.ContentVersion = catalog.Meta().Version
	updateStatus.MapAssetVersion = mapAssetVersion

	st := store.New(*data, reg)
	srv := server.New(st, reg, qr, server.Config{
		Token: *token, SVGBaseURL: *svgBase, Catalog: catalog, LogsDir: *logsDir,
		MapDir: mapDir, MapAssetVersion: mapAssetVersion, UpdateStatus: updateStatus,
	})

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
	if *autoUpdate {
		_, contentStatErr := os.Stat(*contentFile)
		forceUpdate := contentStatErr != nil || catalog.Meta().Source == "embedded" || mapDir == ""
		go autoupdate.Run(ctx, autoupdate.Config{
			StateFile: updateStateFile, Interval: *updateInterval, RetryInterval: *updateRetry, Force: forceUpdate,
			ContentUpdate: func(updateCtx context.Context) (string, error) {
				report, err := content.Refresh(updateCtx, content.RefreshOptions{
					BaseURL: *updateBaseURL, Target: *contentFile, Apply: true,
				})
				if err != nil {
					log.Printf("游戏数据自动更新失败，继续使用当前版本：%v", err)
					return "", err
				}
				updated, err := content.Load(*contentFile)
				if err != nil {
					return "", err
				}
				srv.SetCatalog(updated)
				if report.Unchanged {
					log.Printf("游戏数据已是最新版本：%s", updated.Meta().Version)
				} else {
					log.Printf("游戏数据已自动更新：%s", updated.Meta().Version)
				}
				return updated.Meta().Version, nil
			},
			MapAssetUpdate: func(updateCtx context.Context) (string, error) {
				if *svgBase != server.DefaultSVGBaseURL {
					return mapAssetVersion, nil
				}
				report, err := mapassets.Refresh(updateCtx, reg, mapassets.RefreshOptions{Root: *mapCache})
				if err != nil {
					log.Printf("地图资源自动更新失败，继续使用当前或内置地图：%v", err)
					return "", err
				}
				srv.SetMapAssets(report.Directory, report.Version)
				if report.Unchanged {
					log.Printf("地图资源已是最新版本：%s", report.Version)
				} else {
					log.Printf("地图资源已自动更新：%s（%d 个文件）", report.Version, report.Files)
				}
				return report.Version, nil
			},
			OnStatus: srv.SetUpdateStatus,
		})
	}

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
	replay := len(args) > 0 && args[0] == "replay-quests"
	if replay {
		args = args[1:]
	}
	fs := flag.NewFlagSet("agent", flag.ExitOnError)
	serverURL := fs.String("server", "", "map server URL, e.g. http://192.168.1.10:8400")
	token := fs.String("token", "", "ingest token")
	screens := fs.String("screenshots", defaultScreenshotsDir(), "EFT screenshots folder")
	logsDir := fs.String("logs", detectLogsDir(), "EFT Logs folder (optional, enables auto map/quest detection)")
	profile := fs.String("profile", "default", "local profile name")
	mode := fs.String("mode", "pvp", "task mode: pvp or pve")
	wipe := fs.String("wipe", "current", "wipe-cycle identifier")
	fromSession := fs.String("from-session", "", "first historical log-session folder to import")
	sourceProfile := fs.String("source-profile", "", "game profile id to import")
	applyReplay := fs.Bool("apply", false, "upload the previewed historical task events")
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
		Profile:        *profile,
		Mode:           *mode,
		Wipe:           *wipe,
		FromSession:    *fromSession,
		SourceProfile:  *sourceProfile,
	})
	if replay {
		if *logsDir == "" {
			log.Fatal("agent replay-quests: -logs is required")
		}
		result, err := a.ReplayQuests(*applyReplay)
		if err != nil {
			log.Fatal(err)
		}
		log.Printf("历史任务日志：%d 个会话，%d 个事件，%d 个候选起点", result.Sessions, len(result.Events), len(result.Breakpoints))
		for _, breakpoint := range result.Breakpoints {
			log.Printf("候选起点：%s profile=%s mode=%s version=%s", breakpoint.SessionID, breakpoint.GameProfileID, breakpoint.Mode, breakpoint.GameVersion)
		}
		if *applyReplay {
			log.Printf("历史任务日志上报完成：%d 个事件", len(result.Events))
		} else {
			log.Printf("仅预览；确认候选起点后追加 -from-session <目录名> -apply")
		}
		return
	}

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

func defaultContentFile() string {
	exe, err := os.Executable()
	if err != nil {
		return "mapapp-content.json"
	}
	return filepath.Join(filepath.Dir(exe), "mapapp-content.json")
}

func defaultMapCacheDir() string {
	exe, err := os.Executable()
	if err != nil {
		return "mapapp-map-cache"
	}
	return filepath.Join(filepath.Dir(exe), "mapapp-map-cache")
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

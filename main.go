package main

import (
	"embed"
	"io/fs"
	"log"
	"sync"

	"github.com/wailsapp/wails/v3/pkg/events"

	"github.com/wailsapp/wails/v3/pkg/application"

	"hypervvmmanager/internal/hosts"
	"hypervvmmanager/internal/httpapi"
	"hypervvmmanager/internal/service"
	"hypervvmmanager/internal/settings"
	"hypervvmmanager/internal/tasks"
)

//go:embed all:frontend/dist
var assets embed.FS

//go:embed build/trayicon.png
var trayIcon []byte

func init() {
	// 注册事件类型，Wails 绑定生成器会据此生成强类型的前端事件定义。
	application.RegisterEvent[tasks.Task]("task:update")
	application.RegisterEvent[tasks.Task]("task:finished")
}

func main() {
	store, err := settings.NewStore()
	if err != nil {
		log.Fatalf("无法加载设置: %v", err)
	}

	queue, err := tasks.NewQueue(store.TasksPath())
	if err != nil {
		log.Fatalf("无法加载任务历史: %v", err)
	}

	hostStore, err := hosts.NewStore(store.HostsPath())
	if err != nil {
		log.Fatalf("无法加载主机配置: %v", err)
	}

	svc := service.New(queue, store, hostStore)

	dist, err := fs.Sub(assets, "frontend/dist")
	if err != nil {
		log.Fatalf("无法加载前端资源: %v", err)
	}
	api := httpapi.New(svc, dist)
	svc.AttachAPI(api)

	app := application.New(application.Options{
		Name:        "Hyper-V 虚拟机管理助手",
		Description: "Hyper-V 虚拟机可视化管理工具",
		Services: []application.Service{
			application.NewService(svc),
		},
		Assets: application.AssetOptions{
			Handler: application.AssetFileServerFS(assets),
		},
		Mac: application.MacOptions{
			ApplicationShouldTerminateAfterLastWindowClosed: true,
		},
	})

	windowTheme := application.SystemDefault
	switch store.Get().Theme {
	case "dark":
		windowTheme = application.Dark
	case "light":
		windowTheme = application.Light
	}

	window := app.Window.NewWithOptions(application.WebviewWindowOptions{
		Title:            "Hyper-V 虚拟机管理助手",
		Frameless:        true,
		Width:            1440,
		Height:           900,
		MinWidth:         1080,
		MinHeight:        700,
		BackgroundType:   application.BackgroundTypeSolid,
		BackgroundColour: application.NewRGB(14, 16, 20),
		URL:              "/",
		Windows: application.WindowsWindow{
			// 关闭系统绘制的无边框装饰（Aero 阴影/圆角），避免窗口顶部出现
			// 与内容不一致的白色条带；标题栏完全由前端自绘。
			DisableFramelessWindowDecorations: true,
			Theme:                             windowTheme,
		},
	})
	window.Center()

	// 系统托盘：关闭到托盘时可以从托盘重新显示或退出。
	tray := app.SystemTray.New()
	tray.SetTooltip("Hyper-V 虚拟机管理助手")
	tray.SetIcon(trayIcon)
	trayMenu := application.NewMenu()

	// Wails 在 Windows 上隐藏最后一个窗口时会结束事件循环，
	// 因此“最小化到托盘”不隐藏窗口，而是缩小并移出屏幕，保持应用存活。
	hideToTray := func() {
		window.Restore()
		window.SetSize(320, 220)
		window.SetPosition(-32000, -32000)
	}
	showFromTray := func() {
		window.SetSize(1440, 900)
		window.Center()
		window.Show()
		window.Focus()
	}

	trayMenu.Add("显示主窗口").OnClick(func(_ *application.Context) {
		showFromTray()
	})
	trayMenu.Add("退出").OnClick(func(_ *application.Context) {
		app.Quit()
	})
	tray.SetMenu(trayMenu)
	tray.OnClick(func() {
		showFromTray()
	})

	var quitOnce sync.Once
	quitApp := func() {
		quitOnce.Do(func() {
			app.Quit()
		})
	}

	window.OnWindowEvent(events.Common.WindowClosing, func(event *application.WindowEvent) {
		switch store.Get().CloseBehavior {
		case "tray":
			event.Cancel()
			hideToTray()
		case "close":
			event.Cancel()
			quitApp()
		default:
			event.Cancel()
			dialog := app.Dialog.Question().
				SetTitle("关闭 Hyper-V 虚拟机管理助手").
				SetMessage("你是想直接关闭应用，还是最小化到系统托盘继续运行？\n\n选择后会记住，可在“设置 → 外观与关闭行为”中修改。")

			closeButton := &application.Button{Label: "直接关闭", IsCancel: true}
			closeButton.OnClick(func() {
				_, _ = store.Update(func(current *settings.Settings) {
					current.CloseBehavior = "close"
				})
				quitApp()
			})
			trayButton := &application.Button{Label: "最小化到托盘", IsDefault: true}
			trayButton.OnClick(func() {
				_, _ = store.Update(func(current *settings.Settings) {
					current.CloseBehavior = "tray"
				})
				hideToTray()
			})
			dialog.AddButtons([]*application.Button{closeButton, trayButton}).Show()
		}
	})

	if err := app.Run(); err != nil {
		log.Fatal(err)
	}
}

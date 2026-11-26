package main

import (
	"embed"
	"log"

	"github.com/wailsapp/wails/v3/pkg/application"

	"hypervvmmanager/internal/hosts"
	"hypervvmmanager/internal/httpapi"
	"hypervvmmanager/internal/service"
	"hypervvmmanager/internal/settings"
	"hypervvmmanager/internal/tasks"
)

//go:embed all:frontend/dist
var assets embed.FS

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
	api := httpapi.New(svc)
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

	window := app.Window.NewWithOptions(application.WebviewWindowOptions{
		Title:            "Hyper-V 虚拟机管理助手",
		Width:            1440,
		Height:           900,
		MinWidth:         1080,
		MinHeight:        700,
		BackgroundColour: application.NewRGB(16, 17, 20),
		URL:              "/",
	})
	window.Center()

	if err := app.Run(); err != nil {
		log.Fatal(err)
	}
}

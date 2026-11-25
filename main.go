package main

import (
	"embed"
	"log"
	"sync/atomic"

	"github.com/wailsapp/wails/v3/pkg/application"
	"github.com/wailsapp/wails/v3/pkg/events"

	"dragon/internal/appservice"
	"dragon/internal/poller"
	"dragon/internal/store"
)

//go:embed all:frontend/dist
var assets embed.FS

//go:embed assets/icon.png
var trayIcon []byte

func init() {
	application.RegisterEvent[store.PipelineRun]("dragon:run:updated")
	application.RegisterEvent[store.StageExecution]("dragon:stage:updated")
	application.RegisterEvent[store.RunLog]("dragon:log:chunk")
	application.RegisterEvent[poller.SyncSummary]("dragon:issues:synced")
	application.RegisterEvent[store.ReleaseRecord]("dragon:release:updated")
	application.RegisterEvent[appservice.NavigateEvent]("dragon:navigate")
}

func resolveBackdrop(material string) application.BackdropType {
	switch material {
	case "mica_alt":
		return application.Tabbed
	case "acrylic":
		return application.Acrylic
	default:
		return application.Mica
	}
}

func resolveTheme(themeMode string) application.Theme {
	switch themeMode {
	case "dark":
		return application.Dark
	case "light":
		return application.Light
	default:
		return application.SystemDefault
	}
}

func main() {
	core, err := appservice.Bootstrap(appservice.BootstrapOptions{})
	if err != nil {
		log.Fatalf("Dragon 初始化失败: %v", err)
	}

	settings := core.GetSettingsSnapshot()
	var quitting atomic.Bool

	app := application.New(application.Options{
		Name:        "Dragon",
		Description: "Automated Open-Source Issue-to-Release SDLC Orchestrator",
		Icon:        trayIcon,
		Services: []application.Service{
			application.NewService(appservice.NewDashboardService(core)),
			application.NewService(appservice.NewRepoService(core)),
			application.NewService(appservice.NewIssueService(core)),
			application.NewService(appservice.NewPipelineService(core)),
			application.NewService(appservice.NewAgentService(core)),
			application.NewService(appservice.NewReleaseService(core)),
			application.NewService(appservice.NewSettingsService(core)),
			application.NewService(appservice.NewSafeNotificationService(core)),
		},
		Assets: application.AssetOptions{
			Handler: application.AssetFileServerFS(assets),
		},
		Windows: application.WindowsOptions{
			WndClass:                      "DragonMainWindow",
			DisableQuitOnLastWindowClosed: true,
		},
		Mac: application.MacOptions{
			ApplicationShouldTerminateAfterLastWindowClosed: false,
		},
		OnShutdown: func() {
			core.Shutdown()
		},
	})
	core.App = app

	mainWindow := app.Window.NewWithOptions(application.WebviewWindowOptions{
		Name:             "main",
		Title:            "Dragon",
		Width:            1360,
		Height:           860,
		MinWidth:         1080,
		MinHeight:        700,
		Frameless:        true,
		BackgroundType:   application.BackgroundTypeTranslucent,
		BackgroundColour: application.NewRGBA(0, 0, 0, 0),
		URL:              "/",
		Windows: application.WindowsWindow{
			BackdropType:                    resolveBackdrop(settings.BackdropMaterial),
			Theme:                           resolveTheme(settings.ThemeMode),
			DisableFramelessWindowDecorations: false,
			DisableMenu:                     true,
		},
		Mac: application.MacWindow{
			InvisibleTitleBarHeight: 40,
			Backdrop:                application.MacBackdropTranslucent,
			TitleBar:                application.MacTitleBarHiddenInset,
		},
	})
	core.MainWindow = mainWindow

	mainWindow.RegisterHook(events.Common.WindowClosing, func(e *application.WindowEvent) {
		if !quitting.Load() && core.GetSettingsSnapshot().MinimizeToTray {
			e.Cancel()
			mainWindow.Hide()
			return
		}
		quitting.Store(true)
		app.Quit()
	})

	// Configure System Tray & Menu.
	tray := app.SystemTray.New()
	tray.SetIcon(trayIcon)
	tray.SetTooltip("Dragon — Open-Source SDLC Automation")

	trayMenu := application.NewMenu()
	trayMenu.Add("打开 Dragon 主窗口").OnClick(func(_ *application.Context) {
		mainWindow.Show().Focus()
	})
	trayMenu.Add("立即同步所有仓库").OnClick(func(_ *application.Context) {
		go func() {
			_, _ = core.Poller.SyncAllNow(core.BaseCtx)
		}()
	})
	trayMenu.AddSeparator()
	trayMenu.Add("退出 Dragon").OnClick(func(_ *application.Context) {
		quitting.Store(true)
		app.Quit()
	})

	tray.SetMenu(trayMenu)
	tray.OnClick(func() {
		mainWindow.Show().Focus()
	})

	// Kick off local CLI detection and background issue polling.
	go func() {
		_, _ = core.Registry.DetectAll(core.BaseCtx)
	}()
	core.Poller.Start(core.BaseCtx)

	if err := app.Run(); err != nil {
		log.Fatal(err)
	}
}

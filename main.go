package main

import (
	_ "embed"
	"flag"
	"image/png"
	"log"
	"os"

	"github.com/egoist/mygo"
	"github.com/egoist/mygo/ui"

	"locloom/internal/app"
	"locloom/internal/i18n"
)

//go:embed README.md
var embeddedReadmeEN string

//go:embed README.zh-CN.md
var embeddedReadmeZH string

func main() {
	headlessRender := flag.String("headless-render", "", "Render UI to a PNG file and exit (for smoke verification)")
	openHelp := flag.Bool("open-help", false, "Open usage documentation modal on startup")
	langFlag := flag.String("lang", "", "Optional UI language override: auto, zh, en")
	themeFlag := flag.String("theme", "", "Optional theme override: system, light, dark")
	srcDir := flag.String("src", "", "Optional initial localization file, directory, or .zip archive path")
	tsDir := flag.String("ts", "", "Alias for -src: optional initial localization file, directory, or .zip path")
	xlsxFile := flag.String("xlsx", "", "Optional initial .xlsx file path")
	width := flag.Int("width", 1280, "Initial window width")
	height := flag.Int("height", 820, "Initial window height")
	flag.Parse()

	application := app.New()
	if *langFlag != "" {
		application.ApplyCLILang(*langFlag)
	}
	application.SetHelpMarkdowns(embeddedReadmeEN, embeddedReadmeZH)
	if *openHelp {
		application.SetShowHelp(true)
	}
	switch *themeFlag {
	case "light":
		application.ApplyCLITheme(1)
	case "dark":
		application.ApplyCLITheme(2)
	case "system":
		application.ApplyCLITheme(0)
	}
	targetSrc := *srcDir
	if targetSrc == "" {
		targetSrc = *tsDir
	}
	if targetSrc != "" {
		_ = application.LoadProject(targetSrc)
	}
	if *xlsxFile != "" {
		_ = application.ImportFromExcelFile(*xlsxFile, "", true)
	}

	if *headlessRender != "" {
		img := ui.Render(application.View, *width, *height, 1.0)
		f, err := os.Create(*headlessRender)
		if err != nil {
			log.Fatalf("create headless png: %v", err)
		}
		if err := png.Encode(f, img); err != nil {
			f.Close()
			log.Fatalf("encode headless png: %v", err)
		}
		f.Close()
		return
	}

	application.ApplyOSTheme()
	mygo.Theme.OnUpdated(func() {
		application.SyncAppIcon()
		for _, w := range mygo.Windows() {
			w.Invalidate()
		}
	})
	mygo.App.OnOpenFile(func(path string) {
		application.OpenExternalPath(path)
	})
	mygo.App.OnLocaleChanged(func(loc string) {
		i18n.SetSystemLocale(loc)
		application.DispatchRefreshLocale()
	})
	mygo.App.WhenReady(func() {
		syncSystemLocaleFromMyGo()
		application.RefreshSystemLocale()
		win := mygo.NewWindow(mygo.WindowOptions{
			Title:    application.WindowTitle(),
			Width:    *width,
			Height:   *height,
			StateKey: "locloom-main",
			Content:  ui.View(application.View),
		})
		application.SetWindow(win)
	})

	if err := mygo.App.Run(); err != nil {
		log.Fatal(err)
	}
}

func syncSystemLocaleFromMyGo() {
	for _, lang := range mygo.App.PreferredLanguages() {
		if lang != "" {
			i18n.SetSystemLocale(lang)
			return
		}
	}
	if loc := mygo.App.Locale(); loc != "" {
		i18n.SetSystemLocale(loc)
	}
}

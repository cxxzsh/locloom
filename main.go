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
)

//go:embed README.md
var embeddedReadme string

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
		application.SetUILang(*langFlag)
	}
	application.SetHelpMarkdown(embeddedReadme)
	if *openHelp {
		application.SetShowHelp(true)
	}
	switch *themeFlag {
	case "light":
		application.SetThemeMode(1)
	case "dark":
		application.SetThemeMode(2)
	case "system":
		application.SetThemeMode(0)
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
		for _, w := range mygo.Windows() {
			w.Invalidate()
		}
	})
	mygo.App.WhenReady(func() {
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

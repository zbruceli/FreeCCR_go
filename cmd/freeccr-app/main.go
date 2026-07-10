// FreeCCR-go — a Go port of FreeCCR (https://github.com/toonoumi/FreeCCR).
// Copyright (C) 2026 Bruce Li. Licensed under AGPL-3.0-or-later; see LICENSE.

// Command freeccr-app is the native desktop build: a Wails window hosting the
// same web UI (internal/ui) served through Wails' AssetServer.Handler, so the
// entire front-end and API are reused unchanged inside a native window, with a
// native menu bar and native folder dialogs.
//
// Build (macOS/Linux need cgo for the webview; RAW needs libraw):
//
//	CGO_ENABLED=1 CGO_LDFLAGS='-framework UniformTypeIdentifiers' \
//	  CGO_LDFLAGS_ALLOW='-Xpreprocessor|-fopenmp' \
//	  go build -tags 'desktop,production,libraw' -o bin/FreeCCR ./cmd/freeccr-app
package main

import (
	"context"

	"github.com/wailsapp/wails/v2"
	"github.com/wailsapp/wails/v2/pkg/menu"
	"github.com/wailsapp/wails/v2/pkg/menu/keys"
	"github.com/wailsapp/wails/v2/pkg/options"
	"github.com/wailsapp/wails/v2/pkg/options/assetserver"
	wruntime "github.com/wailsapp/wails/v2/pkg/runtime"

	"github.com/zhengli/freeccr-go/internal/ui"
)

// App carries the Wails runtime context for native actions (dialogs, menu).
type App struct {
	ctx context.Context
}

func (a *App) startup(ctx context.Context) { a.ctx = ctx }

// PickFolder opens a native folder chooser and returns the selected path (empty
// if cancelled). Called from the front-end's Browse button when running under
// Wails; the plain-web build falls back to its in-app folder browser.
func (a *App) PickFolder() string {
	dir, err := wruntime.OpenDirectoryDialog(a.ctx, wruntime.OpenDialogOptions{
		Title: "Choose roll folder",
	})
	if err != nil {
		return ""
	}
	return dir
}

// openRollFromMenu prompts for a folder and tells the front-end to load it.
func (a *App) openRollFromMenu() {
	if dir := a.PickFolder(); dir != "" {
		wruntime.EventsEmit(a.ctx, "menu:open-roll", dir)
	}
}

func (a *App) buildMenu() *menu.Menu {
	m := menu.NewMenu()
	m.Append(menu.AppMenu()) // macOS: About / Hide / Quit

	file := m.AddSubmenu("File")
	file.AddText("Open Roll…", keys.CmdOrCtrl("o"), func(*menu.CallbackData) { a.openRollFromMenu() })
	file.AddSeparator()
	file.AddText("Export All", keys.CmdOrCtrl("e"), func(*menu.CallbackData) {
		wruntime.EventsEmit(a.ctx, "menu:export")
	})

	m.Append(menu.EditMenu()) // standard Cut/Copy/Paste/Select All
	return m
}

func main() {
	srv := ui.NewServer()
	app := &App{}
	err := wails.Run(&options.App{
		Title:            "FreeCCR-go",
		Width:            1440,
		Height:           900,
		MinWidth:         1100,
		MinHeight:        700,
		BackgroundColour: &options.RGBA{R: 30, G: 30, B: 30, A: 255},
		AssetServer:      &assetserver.Options{Handler: srv.Handler()},
		Menu:             app.buildMenu(),
		OnStartup:        app.startup,
		Bind:             []any{app},
	})
	if err != nil {
		println("FreeCCR-go failed to start:", err.Error())
	}
}

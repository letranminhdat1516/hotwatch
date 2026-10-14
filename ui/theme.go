package ui

import "github.com/egoist/mygo/ui"

// Theme returns hotwatch's light or dark theme: calm neutral surfaces, one
// teal accent, and green/amber/red for state. The Shell sets it on every
// frame from the system's appearance (ui.Theme.Dark), so toggling macOS
// appearance switches it live.
func Theme(dark bool) *ui.Theme {
	if dark {
		t := *ui.DarkTheme()
		t.Background = ui.Hex("#111416")
		t.Surface = ui.Hex("#1a1e21")
		t.SurfaceHover = ui.Hex("#22272b")
		t.SurfacePressed = ui.Hex("#2a3034")
		t.Border = ui.Hex("#2b3135")
		t.Text = ui.Hex("#e6eaec")
		t.TextMuted = ui.Hex("#929ca4")
		t.Accent = ui.Hex("#3cc4b5")
		t.AccentHover = ui.Hex("#55d0c2")
		t.AccentPressed = ui.Hex("#2fa99c")
		t.AccentText = ui.Hex("#06201d")
		t.Success = ui.Hex("#45c27a")
		t.Warning = ui.Hex("#e5a93f")
		t.Danger = ui.Hex("#f26d6d")
		t.Focus = t.Accent.Alpha(0.6)
		t.Radius = 8
		return &t
	}
	t := *ui.LightTheme()
	t.Background = ui.Hex("#f4f5f4")
	t.Surface = ui.Hex("#ffffff")
	t.SurfaceHover = ui.Hex("#eef0ef")
	t.SurfacePressed = ui.Hex("#e4e7e6")
	t.Border = ui.Hex("#e0e3e2")
	t.Text = ui.Hex("#1a2024")
	t.TextMuted = ui.Hex("#5f6b73")
	t.Accent = ui.Hex("#0f9b8e")
	t.AccentHover = ui.Hex("#0d8b7f")
	t.AccentPressed = ui.Hex("#0b7a70")
	t.AccentText = ui.Hex("#ffffff")
	t.Success = ui.Hex("#1e9e57")
	t.Warning = ui.Hex("#b7740b")
	t.Danger = ui.Hex("#d23f3f")
	t.Focus = t.Accent.Alpha(0.5)
	t.Radius = 8
	return &t
}

var themes = [2]*ui.Theme{Theme(false), Theme(true)}

func themeFor(dark bool) *ui.Theme {
	if dark {
		return themes[1]
	}
	return themes[0]
}

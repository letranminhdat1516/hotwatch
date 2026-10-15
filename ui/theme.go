package ui

import "github.com/egoist/mygo/ui"

// Theme returns hotwatch's light or dark theme from the approved design
// (UI-DESIGN.md "Tokens"): warm neutrals with a faint rose cast, one rose
// accent, green/amber/red for state, 14 pt cards. The Shell sets it on every
// frame from the system's appearance (ui.Theme.Dark), so toggling macOS
// appearance switches it live.
func Theme(dark bool) *ui.Theme {
	if dark {
		t := *ui.DarkTheme()
		t.Background = ui.Hex("#121014")
		t.Surface = ui.Hex("#1b181c")
		t.SurfaceHover = ui.Hex("#262127")
		t.SurfacePressed = ui.Hex("#302a31")
		t.Border = ui.Hex("#2a2429")
		t.Text = ui.Hex("#ede7e9")
		t.TextMuted = ui.Hex("#9a8f94")
		t.Accent = ui.Hex("#fb7185")
		t.AccentHover = ui.Hex("#fda4af")
		t.AccentPressed = ui.Hex("#f43f5e")
		t.AccentText = ui.Hex("#1c0a10")
		t.Success = ui.Hex("#4ade80")
		t.Warning = ui.Hex("#fbbf24")
		t.Danger = ui.Hex("#f87171")
		t.Focus = t.Accent.Alpha(0.6)
		t.Radius = 14
		return &t
	}
	t := *ui.LightTheme()
	t.Background = ui.Hex("#f6f3f4")
	t.Surface = ui.Hex("#ffffff")
	t.SurfaceHover = ui.Hex("#efe9eb")
	t.SurfacePressed = ui.Hex("#e6dfe2")
	t.Border = ui.Hex("#ece6e8")
	t.Text = ui.Hex("#1c1619")
	t.TextMuted = ui.Hex("#6f6469")
	t.Accent = ui.Hex("#e11d48")
	t.AccentHover = ui.Hex("#be123c")
	t.AccentPressed = ui.Hex("#9f1239")
	t.AccentText = ui.Hex("#ffffff")
	t.Success = ui.Hex("#15803d")
	t.Warning = ui.Hex("#b45309")
	t.Danger = ui.Hex("#b91c1c")
	t.Focus = t.Accent.Alpha(0.5)
	t.Radius = 14
	return &t
}

var themes = [2]*ui.Theme{Theme(false), Theme(true)}

func themeFor(dark bool) *ui.Theme {
	if dark {
		return themes[1]
	}
	return themes[0]
}

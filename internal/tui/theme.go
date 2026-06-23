// Package tui provides interactive terminal UI components for spec.
//
// The theme sub-system resolves a colour palette from user preferences or
// terminal capabilities and exposes semantic style constructors that all
// views and components reference. No hardcoded colour values outside this file.
package tui

import (
	"image/color"
	"os"
	"sync"

	"charm.land/bubbles/v2/textarea"
	"charm.land/bubbles/v2/textinput"
	tea "charm.land/bubbletea/v2"
	"charm.land/lipgloss/v2"

	catppuccin "github.com/catppuccin/go"

	"github.com/aaronl1011/spec/internal/tui/components"
)

// Theme holds the semantic colour palette for the entire TUI.
type Theme struct {
	Base    color.Color // background / deepest layer
	Surface color.Color // panels, selected rows
	Overlay color.Color // borders, separators
	Text    color.Color // primary text
	SubText color.Color // secondary, dimmed
	Accent  color.Color // highlights, active tab
	Success color.Color // done, approved
	Warning color.Color // stale, blocked
	Error   color.Color // critical, failed
	Muted   color.Color // disabled, inactive
}

// Styles holds pre-built lipgloss styles derived from the active theme.
type Styles struct {
	// Layout
	Header    lipgloss.Style
	StatusBar lipgloss.Style
	TabActive lipgloss.Style
	TabNormal lipgloss.Style
	Content   lipgloss.Style

	// Text
	Title    lipgloss.Style
	Subtitle lipgloss.Style
	Muted    lipgloss.Style
	Bold     lipgloss.Style

	// Semantic
	Success lipgloss.Style
	Warning lipgloss.Style
	Error   lipgloss.Style
	Accent  lipgloss.Style

	// Table
	RowSelected lipgloss.Style
	RowNormal   lipgloss.Style

	// Section headers within views
	SectionTitle lipgloss.Style
	Separator    lipgloss.Style
}

// NewStyles constructs all styles from a theme.
func NewStyles(t Theme) Styles {
	return Styles{
		Header: lipgloss.NewStyle().
			Foreground(t.Text).
			Background(t.Surface).
			Bold(true).
			Padding(0, 1),

		StatusBar: lipgloss.NewStyle().
			Foreground(t.SubText).
			Background(t.Surface).
			Padding(0, 1),

		TabActive: lipgloss.NewStyle().
			Foreground(t.Accent).
			Bold(true).
			Padding(0, 1),

		TabNormal: lipgloss.NewStyle().
			Foreground(t.SubText).
			Padding(0, 1),

		Content: lipgloss.NewStyle().
			Padding(1, 2),

		Title: lipgloss.NewStyle().
			Foreground(t.Text).
			Bold(true),

		Subtitle: lipgloss.NewStyle().
			Foreground(t.SubText),

		Muted: lipgloss.NewStyle().
			Foreground(t.Muted),

		Bold: lipgloss.NewStyle().
			Foreground(t.Text).
			Bold(true),

		Success: lipgloss.NewStyle().
			Foreground(t.Success),

		Warning: lipgloss.NewStyle().
			Foreground(t.Warning),

		Error: lipgloss.NewStyle().
			Foreground(t.Error),

		Accent: lipgloss.NewStyle().
			Foreground(t.Accent),

		RowSelected: lipgloss.NewStyle().
			Background(t.Surface).
			Foreground(t.Text).
			Bold(true),

		RowNormal: lipgloss.NewStyle().
			Foreground(t.Text),

		SectionTitle: lipgloss.NewStyle().
			Foreground(t.Accent).
			Bold(true).
			MarginTop(1),

		Separator: lipgloss.NewStyle().
			Foreground(t.Overlay),
	}
}

// modalStyles assembles the modal component styles from the active theme,
// including a themed text field (accent cursor, palette text) and a framed
// input box so the prompt and field read as one tidy unit.
func modalStyles(t Theme, styles Styles) components.ModalStyles {
	return components.ModalStyles{
		Border:  lipgloss.NewStyle().Border(lipgloss.RoundedBorder()).BorderForeground(t.Accent),
		Title:   styles.Title,
		Message: styles.Subtitle,
		Input: lipgloss.NewStyle().
			Foreground(t.Text),
		Hint:       styles.Muted,
		InputField: textInputStyles(t),
	}
}

// textInputStyles builds bubbles textinput styles from the active theme so
// inline fields read in the app palette (primary text, accent cursor, muted
// placeholder) rather than the library's hardcoded ANSI defaults.
func textInputStyles(t Theme) textinput.Styles {
	var s textinput.Styles
	s.Focused = textinput.StyleState{
		Text:        lipgloss.NewStyle().Foreground(t.Text),
		Placeholder: lipgloss.NewStyle().Foreground(t.Muted),
		Suggestion:  lipgloss.NewStyle().Foreground(t.Muted),
		Prompt:      lipgloss.NewStyle().Foreground(t.Accent),
	}
	s.Blurred = textinput.StyleState{
		Text:        lipgloss.NewStyle().Foreground(t.SubText),
		Placeholder: lipgloss.NewStyle().Foreground(t.Muted),
		Suggestion:  lipgloss.NewStyle().Foreground(t.Muted),
		Prompt:      lipgloss.NewStyle().Foreground(t.Accent),
	}
	s.Cursor = textinput.CursorStyle{
		Color: t.Accent,
		Shape: tea.CursorBlock,
	}
	return s
}

// textAreaStyles builds bubbles textarea styles from the active theme. The
// library's default highlights the cursor line with a near-white background;
// that reads as a glaring light band against the dark palette, so the cursor
// line is left unstyled and the whole field blends into the pane background.
// The accent block cursor alone marks the insertion point.
func textAreaStyles(t Theme) textarea.Styles {
	state := func() textarea.StyleState {
		return textarea.StyleState{
			Base:        lipgloss.NewStyle().Foreground(t.Text),
			Text:        lipgloss.NewStyle().Foreground(t.Text),
			CursorLine:  lipgloss.NewStyle().Foreground(t.Text),
			Placeholder: lipgloss.NewStyle().Foreground(t.Muted),
			Prompt:      lipgloss.NewStyle().Foreground(t.Accent),
			EndOfBuffer: lipgloss.NewStyle().Foreground(t.Base),
		}
	}
	var s textarea.Styles
	s.Focused = state()
	s.Blurred = state()
	s.Cursor = textarea.CursorStyle{
		Color: t.Accent,
		Shape: tea.CursorBlock,
	}
	return s
}

// statusStyles builds the canonical status element's per-kind styles from the
// existing "pending" element palette tokens (SPEC-016 AC-6). The pending kind
// reuses the Warning token (the colour the retired pending notice and spinner
// used); success/error/idle reuse the established semantic tokens. All kinds
// adopt the high-contrast filled treatment (theme Base on a coloured ground)
// that the prior pending/toast surfaces used, so the slot inherits the theme's
// established contrast rather than introducing new entries.
func statusStyles(t Theme) components.StatusStyles {
	// Sleek, minimal treatment: status is conveyed by glyph shape and text
	// colour alone — no background fill or padding. The element inherits the
	// status bar's surface so it reads as plain coloured text, not a chip.
	text := func(fg color.Color) lipgloss.Style {
		return lipgloss.NewStyle().Foreground(fg)
	}
	return components.StatusStyles{
		// Idle recedes in the muted tone so the resting slot stays quiet.
		Idle:    text(t.Muted),
		Pending: text(t.Warning),
		Success: text(t.Success),
		Error:   text(t.Error),
	}
}

// ThemeNames returns the ordered list of available named themes.
func ThemeNames() []string {
	return []string{
		"auto",
		"catppuccin-mocha",
		"catppuccin-latte",
		"catppuccin-macchiato",
		"catppuccin-frappe",
		"gruvbox-dark",
		"dracula",
		"tokyo-night",
		"nord",
		"solarized-dark",
		"solarized-light",
		"rose-pine",
		"kanagawa",
		"everforest-dark",
		"everforest-light",
		"github-dark",
		"github-light",
		"ayu-mirage",
		"ayu-light",
		"modus-vivendi",
		"modus-operandi",
		"graphite",
	}
}

// ResolveTheme returns a Theme for the given preference string.
// An empty string or "auto" detects from the terminal.
func ResolveTheme(pref string) Theme {
	switch pref {
	case "catppuccin-mocha":
		return catppuccinMocha()
	case "catppuccin-latte":
		return catppuccinLatte()
	case "catppuccin-macchiato":
		return catppuccinMacchiato()
	case "catppuccin-frappe":
		return catppuccinFrappe()
	case "gruvbox-dark", "gruvbox":
		return gruvboxDark()
	case "dracula":
		return dracula()
	case "tokyo-night":
		return tokyoNight()
	case "nord":
		return nord()
	case "solarized-dark":
		return solarizedDark()
	case "solarized-light":
		return solarizedLight()
	case "rose-pine", "rosé-pine":
		return rosePine()
	case "kanagawa", "kanagawa-wave":
		return kanagawa()
	case "everforest-dark", "everforest":
		return everforestDark()
	case "everforest-light":
		return everforestLight()
	case "github-dark":
		return githubDark()
	case "github-light", "github":
		return githubLight()
	case "ayu-mirage":
		return ayuMirage()
	case "ayu-light", "ayu":
		return ayuLight()
	case "modus-vivendi", "modus-dark":
		return modusVivendi()
	case "modus-operandi", "modus-light":
		return modusOperandi()
	case "graphite", "mono", "monochrome", "noir":
		return graphite()
	default:
		return autoTheme()
	}
}

// darkBackgroundOnce caches the terminal background detection. Querying the
// terminal sends an OSC escape sequence and waits for a reply; once Bubble Tea
// owns stdin that reply never arrives, so the call blocks until it times out.
// Detecting exactly once (at first resolve, before the program loop has fully
// taken over) keeps the "auto" theme from freezing every time the user cycles
// onto it.
var (
	darkBackgroundOnce sync.Once
	darkBackground     bool
)

func hasDarkBackground() bool {
	darkBackgroundOnce.Do(func() {
		darkBackground = lipgloss.HasDarkBackground(os.Stdin, os.Stdout)
	})
	return darkBackground
}

// autoTheme detects dark/light terminal and returns a neutral palette.
func autoTheme() Theme {
	if hasDarkBackground() {
		return darkDefault()
	}
	return lightDefault()
}

func darkDefault() Theme {
	return Theme{
		Base:    lipgloss.Color("#1a1b26"),
		Surface: lipgloss.Color("#24283b"),
		Overlay: lipgloss.Color("#3b4261"),
		Text:    lipgloss.Color("#c0caf5"),
		SubText: lipgloss.Color("#787c99"),
		Accent:  lipgloss.Color("#7aa2f7"),
		Success: lipgloss.Color("#9ece6a"),
		Warning: lipgloss.Color("#e0af68"),
		Error:   lipgloss.Color("#f7768e"),
		Muted:   lipgloss.Color("#565f89"),
	}
}

func lightDefault() Theme {
	return Theme{
		Base:    lipgloss.Color("#fafafa"),
		Surface: lipgloss.Color("#e8e8e8"),
		Overlay: lipgloss.Color("#c0c0c0"),
		Text:    lipgloss.Color("#343b58"),
		SubText: lipgloss.Color("#6a6f87"),
		Accent:  lipgloss.Color("#34548a"),
		Success: lipgloss.Color("#485e30"),
		Warning: lipgloss.Color("#8f5e15"),
		Error:   lipgloss.Color("#8c4351"),
		Muted:   lipgloss.Color("#9699a3"),
	}
}

// --- Named themes ---------------------------------------------------------

func catppuccinMocha() Theme {
	m := catppuccin.Mocha
	return Theme{
		Base:    lipgloss.Color(m.Base().Hex),
		Surface: lipgloss.Color(m.Surface0().Hex),
		Overlay: lipgloss.Color(m.Overlay0().Hex),
		Text:    lipgloss.Color(m.Text().Hex),
		SubText: lipgloss.Color(m.Subtext0().Hex),
		Accent:  lipgloss.Color(m.Blue().Hex),
		Success: lipgloss.Color(m.Green().Hex),
		Warning: lipgloss.Color(m.Yellow().Hex),
		Error:   lipgloss.Color(m.Red().Hex),
		Muted:   lipgloss.Color(m.Overlay1().Hex),
	}
}

func catppuccinLatte() Theme {
	m := catppuccin.Latte
	return Theme{
		Base:    lipgloss.Color(m.Base().Hex),
		Surface: lipgloss.Color(m.Surface0().Hex),
		Overlay: lipgloss.Color(m.Overlay0().Hex),
		Text:    lipgloss.Color(m.Text().Hex),
		SubText: lipgloss.Color(m.Subtext0().Hex),
		Accent:  lipgloss.Color(m.Blue().Hex),
		Success: lipgloss.Color(m.Green().Hex),
		Warning: lipgloss.Color(m.Yellow().Hex),
		Error:   lipgloss.Color(m.Red().Hex),
		Muted:   lipgloss.Color(m.Overlay1().Hex),
	}
}

func catppuccinMacchiato() Theme {
	m := catppuccin.Macchiato
	return Theme{
		Base:    lipgloss.Color(m.Base().Hex),
		Surface: lipgloss.Color(m.Surface0().Hex),
		Overlay: lipgloss.Color(m.Overlay0().Hex),
		Text:    lipgloss.Color(m.Text().Hex),
		SubText: lipgloss.Color(m.Subtext0().Hex),
		Accent:  lipgloss.Color(m.Blue().Hex),
		Success: lipgloss.Color(m.Green().Hex),
		Warning: lipgloss.Color(m.Yellow().Hex),
		Error:   lipgloss.Color(m.Red().Hex),
		Muted:   lipgloss.Color(m.Overlay1().Hex),
	}
}

func catppuccinFrappe() Theme {
	m := catppuccin.Frappe
	return Theme{
		Base:    lipgloss.Color(m.Base().Hex),
		Surface: lipgloss.Color(m.Surface0().Hex),
		Overlay: lipgloss.Color(m.Overlay0().Hex),
		Text:    lipgloss.Color(m.Text().Hex),
		SubText: lipgloss.Color(m.Subtext0().Hex),
		Accent:  lipgloss.Color(m.Blue().Hex),
		Success: lipgloss.Color(m.Green().Hex),
		Warning: lipgloss.Color(m.Yellow().Hex),
		Error:   lipgloss.Color(m.Red().Hex),
		Muted:   lipgloss.Color(m.Overlay1().Hex),
	}
}

func gruvboxDark() Theme {
	return Theme{
		Base:    lipgloss.Color("#282828"),
		Surface: lipgloss.Color("#3c3836"),
		Overlay: lipgloss.Color("#504945"),
		Text:    lipgloss.Color("#ebdbb2"),
		SubText: lipgloss.Color("#a89984"),
		Accent:  lipgloss.Color("#83a598"),
		Success: lipgloss.Color("#b8bb26"),
		Warning: lipgloss.Color("#fabd2f"),
		Error:   lipgloss.Color("#fb4934"),
		Muted:   lipgloss.Color("#665c54"),
	}
}

func dracula() Theme {
	return Theme{
		Base:    lipgloss.Color("#282a36"),
		Surface: lipgloss.Color("#44475a"),
		Overlay: lipgloss.Color("#6272a4"),
		Text:    lipgloss.Color("#f8f8f2"),
		SubText: lipgloss.Color("#bfbfbf"),
		Accent:  lipgloss.Color("#bd93f9"),
		Success: lipgloss.Color("#50fa7b"),
		Warning: lipgloss.Color("#f1fa8c"),
		Error:   lipgloss.Color("#ff5555"),
		Muted:   lipgloss.Color("#6272a4"),
	}
}

func tokyoNight() Theme {
	return Theme{
		Base:    lipgloss.Color("#1a1b26"),
		Surface: lipgloss.Color("#24283b"),
		Overlay: lipgloss.Color("#3b4261"),
		Text:    lipgloss.Color("#c0caf5"),
		SubText: lipgloss.Color("#787c99"),
		Accent:  lipgloss.Color("#7aa2f7"),
		Success: lipgloss.Color("#9ece6a"),
		Warning: lipgloss.Color("#e0af68"),
		Error:   lipgloss.Color("#f7768e"),
		Muted:   lipgloss.Color("#565f89"),
	}
}

func nord() Theme {
	return Theme{
		Base:    lipgloss.Color("#2e3440"),
		Surface: lipgloss.Color("#3b4252"),
		Overlay: lipgloss.Color("#434c5e"),
		Text:    lipgloss.Color("#eceff4"),
		SubText: lipgloss.Color("#d8dee9"),
		Accent:  lipgloss.Color("#88c0d0"),
		Success: lipgloss.Color("#a3be8c"),
		Warning: lipgloss.Color("#ebcb8b"),
		Error:   lipgloss.Color("#bf616a"),
		Muted:   lipgloss.Color("#4c566a"),
	}
}

func solarizedDark() Theme {
	return Theme{
		Base:    lipgloss.Color("#002b36"),
		Surface: lipgloss.Color("#073642"),
		Overlay: lipgloss.Color("#586e75"),
		Text:    lipgloss.Color("#839496"),
		SubText: lipgloss.Color("#657b83"),
		Accent:  lipgloss.Color("#268bd2"),
		Success: lipgloss.Color("#859900"),
		Warning: lipgloss.Color("#b58900"),
		Error:   lipgloss.Color("#dc322f"),
		Muted:   lipgloss.Color("#586e75"),
	}
}

func solarizedLight() Theme {
	return Theme{
		Base:    lipgloss.Color("#fdf6e3"),
		Surface: lipgloss.Color("#eee8d5"),
		Overlay: lipgloss.Color("#93a1a1"),
		Text:    lipgloss.Color("#657b83"),
		SubText: lipgloss.Color("#839496"),
		Accent:  lipgloss.Color("#268bd2"),
		Success: lipgloss.Color("#859900"),
		Warning: lipgloss.Color("#b58900"),
		Error:   lipgloss.Color("#dc322f"),
		Muted:   lipgloss.Color("#93a1a1"),
	}
}

func rosePine() Theme {
	return Theme{
		Base:    lipgloss.Color("#191724"),
		Surface: lipgloss.Color("#1f1d2e"),
		Overlay: lipgloss.Color("#26233a"),
		Text:    lipgloss.Color("#e0def4"),
		SubText: lipgloss.Color("#908caa"),
		Accent:  lipgloss.Color("#c4a7e7"),
		Success: lipgloss.Color("#9ccfd8"),
		Warning: lipgloss.Color("#f6c177"),
		Error:   lipgloss.Color("#eb6f92"),
		Muted:   lipgloss.Color("#6e6a86"),
	}
}

func kanagawa() Theme {
	return Theme{
		Base:    lipgloss.Color("#1f1f28"), // sumiInk3 (bg)
		Surface: lipgloss.Color("#2a2a37"), // sumiInk4 (bg_p1)
		Overlay: lipgloss.Color("#363646"), // sumiInk5 (bg_p2 / borders)
		Text:    lipgloss.Color("#dcd7ba"), // fujiWhite
		SubText: lipgloss.Color("#c8c093"), // oldWhite
		Muted:   lipgloss.Color("#727169"), // fujiGray (comment)
		Accent:  lipgloss.Color("#7e9cd8"), // crystalBlue
		Success: lipgloss.Color("#98bb6c"), // springGreen
		Warning: lipgloss.Color("#e6c384"), // carpYellow
		Error:   lipgloss.Color("#c34043"), // autumnRed
	}
}

func everforestDark() Theme {
	return Theme{
		Base:    lipgloss.Color("#2d353b"), // bg0
		Surface: lipgloss.Color("#343f44"), // bg1
		Overlay: lipgloss.Color("#3d484d"), // bg2
		Text:    lipgloss.Color("#d3c6aa"), // fg
		SubText: lipgloss.Color("#9da9a0"), // grey2
		Muted:   lipgloss.Color("#7a8478"), // grey0
		Accent:  lipgloss.Color("#7fbbb3"), // blue
		Success: lipgloss.Color("#a7c080"), // green
		Warning: lipgloss.Color("#dbbc7f"), // yellow
		Error:   lipgloss.Color("#e67e80"), // red
	}
}

func everforestLight() Theme {
	return Theme{
		Base:    lipgloss.Color("#fdf6e3"), // bg0 (light)
		Surface: lipgloss.Color("#f4f0d9"), // bg1 (light)
		Overlay: lipgloss.Color("#e6e2cc"), // bg3 (light)
		Text:    lipgloss.Color("#5c6a72"), // fg (light)
		SubText: lipgloss.Color("#939f91"), // grey1 (light)
		Muted:   lipgloss.Color("#a6b0a0"), // grey0 (light)
		Accent:  lipgloss.Color("#3a94c5"), // blue (light)
		Success: lipgloss.Color("#8da101"), // green (light)
		Warning: lipgloss.Color("#dfa000"), // yellow (light)
		Error:   lipgloss.Color("#f85552"), // red (light)
	}
}

func githubDark() Theme {
	return Theme{
		Base:    lipgloss.Color("#0d1117"), // canvas.default
		Surface: lipgloss.Color("#161b22"), // canvas.subtle
		Overlay: lipgloss.Color("#30363d"), // border.default
		Text:    lipgloss.Color("#c9d1d9"), // fg.default
		SubText: lipgloss.Color("#8b949e"), // fg.muted
		Muted:   lipgloss.Color("#6e7781"), // fg.subtle
		Accent:  lipgloss.Color("#58a6ff"), // accent.fg (blue)
		Success: lipgloss.Color("#3fb950"), // success.fg (green)
		Warning: lipgloss.Color("#d29922"), // attention.fg (yellow)
		Error:   lipgloss.Color("#f85149"), // danger.fg (red)
	}
}

func githubLight() Theme {
	return Theme{
		Base:    lipgloss.Color("#ffffff"), // canvas.default
		Surface: lipgloss.Color("#f6f8fa"), // canvas.subtle
		Overlay: lipgloss.Color("#d0d7de"), // border.default
		Text:    lipgloss.Color("#24292f"), // fg.default
		SubText: lipgloss.Color("#57606a"), // fg.muted
		Muted:   lipgloss.Color("#6e7781"), // fg.subtle
		Accent:  lipgloss.Color("#0969da"), // accent.fg (blue)
		Success: lipgloss.Color("#1a7f37"), // success.fg (green)
		Warning: lipgloss.Color("#9a6700"), // attention.fg (yellow)
		Error:   lipgloss.Color("#cf222e"), // danger.fg (red)
	}
}

func ayuMirage() Theme {
	return Theme{
		Base:    lipgloss.Color("#212733"), // bg
		Surface: lipgloss.Color("#272d38"), // panel
		Overlay: lipgloss.Color("#3d4751"), // guide
		Text:    lipgloss.Color("#d9d7ce"), // fg
		SubText: lipgloss.Color("#607080"), // fg_idle
		Muted:   lipgloss.Color("#5c6773"), // comment
		Accent:  lipgloss.Color("#ffcc66"), // accent
		Success: lipgloss.Color("#bbe67e"), // string
		Warning: lipgloss.Color("#ffae57"), // keyword
		Error:   lipgloss.Color("#ff3333"), // error
	}
}

func ayuLight() Theme {
	return Theme{
		Base:    lipgloss.Color("#fafafa"), // bg
		Surface: lipgloss.Color("#ffffff"), // panel
		Overlay: lipgloss.Color("#d9d8d7"), // guide
		Text:    lipgloss.Color("#5c6773"), // fg
		SubText: lipgloss.Color("#828c99"), // fg_idle
		Muted:   lipgloss.Color("#abb0b6"), // comment
		Accent:  lipgloss.Color("#ff6a00"), // accent
		Success: lipgloss.Color("#86b300"), // string
		Warning: lipgloss.Color("#ff7733"), // keyword
		Error:   lipgloss.Color("#ff3333"), // error
	}
}

// Accessibility-first: pure-black bg, pure-white fg, max 7:1 contrast.
func modusVivendi() Theme {
	return Theme{
		Base:    lipgloss.Color("#000000"), // bg-main
		Surface: lipgloss.Color("#1e1e1e"), // bg-dim
		Overlay: lipgloss.Color("#646464"), // border
		Text:    lipgloss.Color("#ffffff"), // fg-main
		SubText: lipgloss.Color("#989898"), // fg-dim
		Muted:   lipgloss.Color("#595959"), // mid-neutral (disabled text)
		Accent:  lipgloss.Color("#2fafff"), // blue
		Success: lipgloss.Color("#44bc44"), // green
		Warning: lipgloss.Color("#d0bc00"), // yellow
		Error:   lipgloss.Color("#ff5f59"), // red
	}
}

// Accessibility-first light theme: white bg, black fg, max 7:1 contrast.
func modusOperandi() Theme {
	return Theme{
		Base:    lipgloss.Color("#ffffff"), // bg-main
		Surface: lipgloss.Color("#f2f2f2"), // bg-dim
		Overlay: lipgloss.Color("#9f9f9f"), // border
		Text:    lipgloss.Color("#000000"), // fg-main
		SubText: lipgloss.Color("#595959"), // fg-dim
		Muted:   lipgloss.Color("#9f9f9f"), // border (light disabled text)
		Accent:  lipgloss.Color("#0031a9"), // blue
		Success: lipgloss.Color("#006800"), // green
		Warning: lipgloss.Color("#6f5500"), // yellow
		Error:   lipgloss.Color("#a60000"), // red
	}
}

func graphite() Theme {
	return Theme{
		Base:    lipgloss.Color("#0e0e0e"),
		Surface: lipgloss.Color("#161616"),
		Overlay: lipgloss.Color("#242424"),
		Text:    lipgloss.Color("#d8d8d8"),
		SubText: lipgloss.Color("#9a9a9a"),
		Muted:   lipgloss.Color("#5c5c5c"),
		Accent:  lipgloss.Color("#a6a6a6"),
		Success: lipgloss.Color("#c8c8c8"),
		Warning: lipgloss.Color("#868686"),
		Error:   lipgloss.Color("#ececec"),
	}
}

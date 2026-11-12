// Package kit is a set of styled components built on Fairy: buttons, form
// controls, menus, dialogs, toasts, tables, markdown and more. Their sizes,
// colors, states and motion follow shadcn/ui (Base UI with Tailwind v4).
// Components read their colors and sizes from the current Theme.
//
// Components that keep UI state of their own (whether a dropdown is open, the
// month a date picker shows) take an id that must be unique in the window.
// Everything else is controlled: pass the value and a function that receives
// the new one.
package kit

import (
	"github.com/myparsleycat/fairy"
	"github.com/myparsleycat/fairy/motion"
)

// Theme holds the colors and metrics components use. Its color names are
// shadcn/ui's design tokens; a ...Foreground color is for text and icons on
// the color of the same name.
type Theme struct {
	Dark bool

	Background          fairy.Color // window
	Foreground          fairy.Color // text
	Card                fairy.Color
	CardForeground      fairy.Color
	Popover             fairy.Color // menus, popovers, dialogs, toasts
	PopoverForeground   fairy.Color
	Primary             fairy.Color // default buttons, checked controls
	PrimaryForeground   fairy.Color
	Secondary           fairy.Color // secondary buttons, badges
	SecondaryForeground fairy.Color
	SecondaryHover      fairy.Color // secondary buttons while hovered
	Muted               fairy.Color // tab lists, skeletons, hovered rows
	MutedForeground     fairy.Color // descriptions, placeholders, icons
	Accent              fairy.Color
	AccentForeground    fairy.Color
	Destructive         fairy.Color
	Border              fairy.Color
	Input               fairy.Color // borders and fills of form controls
	Ring                fairy.Color // focus rings
	Chart               [5]fairy.Color

	Sidebar                  fairy.Color
	SidebarForeground        fairy.Color
	SidebarPrimary           fairy.Color
	SidebarPrimaryForeground fairy.Color
	SidebarAccent            fairy.Color // hovered and selected sidebar rows
	SidebarAccentForeground  fairy.Color
	SidebarBorder            fairy.Color
	SidebarRing              fairy.Color

	Success        fairy.Color // status colors of badges, progress bars and toasts
	Warning        fairy.Color
	Scrim          fairy.Color // behind dialogs
	CodeBackground fairy.Color
	// Syntax colors code blocks' tokens. Light and Dark derive it from
	// Foreground, MutedForeground, Success and Destructive; set it again after
	// changing those.
	Syntax SyntaxColors

	Radius    float32 // the base radius; see RadiusSm, RadiusMd, RadiusLg and RadiusXl
	FontSize  float32
	SmallSize float32
	// DialogTopInset leaves space above centered dialogs, typically for a custom title bar.
	DialogTopInset float32
	// SurfaceBlur, above zero, makes menus, popovers, dialogs and toasts
	// translucent, Popover at SurfaceAlpha over a backdrop blur of this many
	// pixels. Status toasts keep at least 0.8 of their color.
	SurfaceBlur  float32
	SurfaceAlpha float32
	// ScrimBlur is the backdrop blur of the scrim behind dialogs.
	ScrimBlur float32
	// Font is the family of the window's text; empty uses the window's font.
	// shadcn/ui apps often ship their own, which the app registers with
	// fairy.RegisterFont and names here.
	Font     string
	MonoFont string
}

// RadiusSm is the radius of small parts, such as keycaps (6 by default).
func (t *Theme) RadiusSm() float32 { return max(t.Radius-4, 0) }

// RadiusMd is the radius of menu rows and small buttons (8 by default).
func (t *Theme) RadiusMd() float32 { return max(t.Radius-2, 0) }

// RadiusLg is the radius of buttons, inputs and popups (10 by default).
func (t *Theme) RadiusLg() float32 { return t.Radius }

// RadiusXl is the radius of dialogs and cards (14 by default).
func (t *Theme) RadiusXl() float32 { return t.Radius + 4 }

// Light returns the light theme.
func Light() Theme {
	t := Theme{
		Background:          fairy.Hex(0xffffff),
		Foreground:          fairy.Hex(0x0a0a0a),
		Card:                fairy.Hex(0xffffff),
		CardForeground:      fairy.Hex(0x0a0a0a),
		Popover:             fairy.Hex(0xffffff),
		PopoverForeground:   fairy.Hex(0x0a0a0a),
		Primary:             fairy.Hex(0x171717),
		PrimaryForeground:   fairy.Hex(0xfafafa),
		Secondary:           fairy.Hex(0xf5f5f5),
		SecondaryForeground: fairy.Hex(0x171717),
		SecondaryHover:      fairy.Hex(0xe7e7e7),
		Muted:               fairy.Hex(0xf5f5f5),
		MutedForeground:     fairy.Hex(0x737373),
		Accent:              fairy.Hex(0x00aa6f),
		AccentForeground:    fairy.Hex(0x171717),
		Destructive:         fairy.Hex(0xe7000b),
		Border:              fairy.Hex(0xe5e5e5),
		Input:               fairy.Hex(0xe5e5e5),
		Ring:                fairy.Hex(0xa1a1a1),
		Chart:               [5]fairy.Color{fairy.Hex(0xf54900), fairy.Hex(0x009689), fairy.Hex(0x104e64), fairy.Hex(0xffb900), fairy.Hex(0xfe9a00)},

		Sidebar:                  fairy.Hex(0xfafafa),
		SidebarForeground:        fairy.Hex(0x0a0a0a),
		SidebarPrimary:           fairy.Hex(0x171717),
		SidebarPrimaryForeground: fairy.Hex(0xfafafa),
		SidebarAccent:            fairy.Hex(0xf5f5f5),
		SidebarAccentForeground:  fairy.Hex(0x171717),
		SidebarBorder:            fairy.Hex(0xe5e5e5),
		SidebarRing:              fairy.Hex(0xa1a1a1),

		Success:        fairy.Hex(0x16a34a),
		Warning:        fairy.Hex(0xd97706),
		Scrim:          fairy.RGBA(0, 0, 0, 0.5),
		CodeBackground: fairy.RGBA(0, 0, 0, 0.05),

		Radius:    10,
		FontSize:  14,
		SmallSize: 12,
		MonoFont:  "Cascadia Mono",
	}
	t.Syntax = syntaxColors(&t)
	return t
}

// Dark returns the dark theme.
func Dark() Theme {
	white := func(a float32) fairy.Color { return fairy.RGBA(255, 255, 255, a) }
	t := Light()
	t.Dark = true
	t.Background = fairy.Hex(0x121212)
	t.Foreground = fairy.Hex(0xfafafa)
	t.Card = fairy.Hex(0x171717)
	t.CardForeground = fairy.Hex(0xfafafa)
	t.Popover = fairy.Hex(0x171717)
	t.PopoverForeground = fairy.Hex(0xfafafa)
	t.Primary = fairy.Hex(0xe5e5e5)
	t.PrimaryForeground = fairy.Hex(0x171717)
	t.Secondary = fairy.Hex(0x262626)
	t.SecondaryForeground = fairy.Hex(0xfafafa)
	t.SecondaryHover = fairy.Hex(0x303030)
	t.Muted = fairy.Hex(0x262626)
	t.MutedForeground = fairy.Hex(0xa1a1a1)
	t.AccentForeground = fairy.Hex(0xfafafa)
	t.Destructive = fairy.Hex(0xff6467)
	t.Border = white(0.1)
	t.Input = white(0.15)
	t.Ring = fairy.Hex(0x737373)
	t.Chart = [5]fairy.Color{fairy.Hex(0x1447e6), fairy.Hex(0x00bc7d), fairy.Hex(0xfe9a00), fairy.Hex(0xad46ff), fairy.Hex(0xff2056)}

	t.Sidebar = fairy.Hex(0x171717)
	t.SidebarForeground = fairy.Hex(0xfafafa)
	t.SidebarPrimary = fairy.Hex(0x1447e6)
	t.SidebarPrimaryForeground = fairy.Hex(0xfafafa)
	t.SidebarAccent = fairy.Hex(0x262626)
	t.SidebarAccentForeground = fairy.Hex(0xfafafa)
	t.SidebarBorder = white(0.1)
	t.SidebarRing = fairy.Hex(0x737373)

	t.Success = fairy.Hex(0x22c55e)
	t.Warning = fairy.Hex(0xf59e0b)
	t.CodeBackground = white(0.07)
	t.Syntax = syntaxColors(&t)
	return t
}

var (
	current Theme
	// applied is the theme Use last applied, kept apart from current so a
	// theme edited through T and passed to Use again still counts as a change.
	applied Theme
)

func init() { Use(Light()) }

// Use makes t the theme of components built from now on, and styles the
// tooltips and scroll bars of every window to match. Call it at the top of
// View to switch themes. When t differs from the current theme, every
// window's fairy.Memo subtrees are built again; calling it with the same theme
// does nothing.
func Use(t Theme) {
	current = t
	if t == applied {
		return
	}
	applied = t
	fairy.InvalidateMemos()
	fairy.SetTooltipStyle(fairy.TooltipStyle{
		Fill:     t.Foreground,
		Text:     t.Background,
		Radius:   t.RadiusMd(),
		PadX:     12,
		PadY:     6,
		FontSize: t.SmallSize,
		Gap:      4,
		Arrow:    true,
		Motion:   motion.Tooltip,
	})
	bar := fairy.RGBA(0, 0, 0, 0.25)
	if t.Dark {
		bar = t.Border
	}
	fairy.SetScrollBarStyle(fairy.ScrollBarStyle{Thickness: 7, Inset: 1, Color: bar})
}

// T returns the current theme.
func T() *Theme { return &current }

// shadowSm, shadowMd and shadowLg are Tailwind's shadow-sm, shadow-md and
// shadow-lg.
func shadowSm(e *fairy.El) *fairy.El {
	return e.Shadow(0, 1, 3, 0, fairy.RGBA(0, 0, 0, 0.1)).Shadow(0, 1, 2, -1, fairy.RGBA(0, 0, 0, 0.1))
}

func shadowMd(e *fairy.El) *fairy.El {
	return e.Shadow(0, 4, 6, -1, fairy.RGBA(0, 0, 0, 0.1)).Shadow(0, 2, 4, -2, fairy.RGBA(0, 0, 0, 0.1))
}

func shadowLg(e *fairy.El) *fairy.El {
	return e.Shadow(0, 10, 15, -3, fairy.RGBA(0, 0, 0, 0.1)).Shadow(0, 4, 6, -4, fairy.RGBA(0, 0, 0, 0.1))
}

// Surface fills e with fill, translucent over a backdrop blur while
// SurfaceBlur is set, as the kit fills menus, popovers, dialogs and toasts.
func (t *Theme) Surface(e *fairy.El, fill fairy.Color) *fairy.El {
	return t.surface(e, fill, t.SurfaceAlpha)
}

func (t *Theme) surface(e *fairy.El, fill fairy.Color, a float32) *fairy.El {
	if t.SurfaceBlur <= 0 {
		return e.Fill(fill)
	}
	return e.Fill(alpha(fill, a)).BackdropBlur(t.SurfaceBlur)
}

// alpha returns c at a times its own opacity, as Tailwind's c/NN.
func alpha(c fairy.Color, a float32) fairy.Color { return c.Alpha(c.A * a) }

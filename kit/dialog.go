package kit

import (
	"github.com/myparsleycat/fairy"
	"github.com/myparsleycat/fairy/icons/lucide"
	"github.com/myparsleycat/fairy/motion"
)

// DialogProps describes a modal dialog.
type DialogProps struct {
	Title string
	// TitleLead is shown before the title, such as an application's logo.
	TitleLead   *fairy.El
	Description string
	Body        *fairy.El   // optional content under the description
	Actions     []*fairy.El // buttons in the footer, right-aligned; the first has focus when the dialog opens
	// Width is 384 when zero, or 320 with Size SizeSmall, as for a short
	// confirmation.
	Width float32
	Size  Size
	// OnClose runs on Escape, a click on the backdrop or the close button.
	// Without it the dialog can only be closed by its actions.
	OnClose func()
	// OnEscape runs on Escape in place of OnClose, for a dialog that only its
	// actions close otherwise or whose Escape does something else.
	OnEscape func()
	// HideCloseButton keeps Escape and backdrop dismissal without displaying an X, as in an alert dialog.
	HideCloseButton bool
	// Alert lays the dialog out as shadcn/ui's AlertDialog: the title on a 24px
	// line, 6px above the description, and no close button.
	Alert bool
	// Key identifies the dialog among overlays; set it when several dialogs
	// can be open in turn.
	Key string
}

// Dialog builds a centered panel over a dimmed backdrop. Show it with
// ShowDialog, or pass it to fairy.Cx.Overlay yourself.
func Dialog(p DialogProps) *fairy.El {
	return dialog(p, 0, 0)
}

func dialog(p DialogProps, viewWidth, viewHeight float32) *fairy.El {
	t := &current
	w := p.Width
	if w == 0 {
		w = 384
		if p.Size == SizeSmall || p.Size == SizeXS {
			w = 320
		}
	}
	if viewWidth > 0 {
		w = min(w, max(viewWidth-32, 1))
	}
	titleLine, headGap := float32(16), float32(8)
	closeButton := p.OnClose != nil && !p.HideCloseButton && !p.Alert
	if p.Alert {
		titleLine, headGap = 24, 6
	}
	title := fairy.Text(p.Title).FontSize(16).Medium().LineHeight(titleLine).Wrap()
	if p.TitleLead != nil {
		title = fairy.Row(p.TitleLead.Shrink(0), title.Grow(1).MinW(0)).Gap(8).Align(fairy.Center)
	}
	head := fairy.Col(title).Gap(headGap)
	if p.Description != "" {
		head.Child(fairy.Text(p.Description).Color(t.MutedForeground).LineHeight(20).Wrap())
	}
	if closeButton {
		head.MarginEdges(0, 28, 0, 0)
	}
	radius := t.RadiusXl()
	panel := fairy.Col(head).W(w).Pad(16).Gap(16).Radius(radius).Fill(t.Popover).Color(t.PopoverForeground).
		Ring(1, alpha(t.Foreground, 0.1)).Role(fairy.RoleDialog).Label(p.Title).
		Enter(fairy.Transition{Scale: 0.95, Motion: motion.Popup}).
		Exit(fairy.Transition{Scale: 0.95, Motion: motion.Popup}).
		OnClick(func() {}) // keeps clicks inside the panel from reaching the backdrop
	if viewHeight > 0 {
		panel.MaxH(max(viewHeight-t.DialogTopInset-32, 1)).ScrollY()
	}
	if p.Body != nil {
		panel.Child(p.Body)
	}
	if len(p.Actions) > 0 {
		// Focus starts in the dialog, on the first action unless the body
		// asked for it first, so keyboard and screen reader users are not left
		// on the control beneath it.
		p.Actions[0].AutoFocus()
		footer := fairy.Row(append([]*fairy.El{fairy.Spacer()}, p.Actions...)...).Gap(8).Pad(16).
			MarginEdges(0, -16, -16, -16).BorderEdges(1, 0, 0, 0, t.Border).Fill(alpha(t.Muted, 0.5)).
			Radii(0, 0, radius, radius)
		panel.Child(footer)
	}
	if closeButton {
		panel.Child(IconButton(lucide.X, p.OnClose, Small, Tooltip(Messages.Close)).Top(8).Right(8))
	}
	key := p.Key
	if key == "" {
		key = "kit.dialog"
	}
	backdrop := fairy.Box(panel).Key(key).Center().PadEdges(16+t.DialogTopInset, 16, 16, 16).Fill(t.Scrim).Role(fairy.RoleNone).
		Enter(fairy.Fade().With(motion.Popup)).Exit(fairy.Fade().With(motion.Popup))
	if p.OnClose != nil {
		backdrop.OnClick(p.OnClose)
	}
	return backdrop
}

// ShowDialog shows the dialog for this build. Call it from View while the
// dialog should be open.
func ShowDialog(cx *fairy.Cx, p DialogProps) {
	w, h := cx.Size()
	escape := p.OnClose
	if p.OnEscape != nil {
		escape = p.OnEscape
	}
	cx.Overlay(dialog(p, w, h), escape)
}

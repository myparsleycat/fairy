package kit

import (
	"fmt"
	"time"

	"github.com/myparsleycat/fairy"
	"github.com/myparsleycat/fairy/icons/lucide"
	"github.com/myparsleycat/fairy/motion"
)

type datePickerState struct {
	open bool
	// month is the first day of the shown month in UTC. The calendar steps
	// through days in UTC because a local midnight can be skipped or repeated
	// where DST starts or ends at 00:00.
	month time.Time
}

func sameDay(a, b time.Time) bool {
	ay, am, ad := a.Date()
	by, bm, bd := b.Date()
	return ay == by && am == bm && ad == bd
}

// DatePicker picks a day from a month calendar. A zero value shows the
// Placeholder, or Messages.PickDate. onChange receives midnight of the picked
// day in value's location (local time for a zero value). id must be unique in
// the window.
func DatePicker(id string, value time.Time, onChange func(time.Time), opts ...Option) *fairy.El {
	o := build(opts)
	t := &current
	st := fairy.Local[datePickerState]("kit.date:" + id)
	label, empty := "", value.IsZero()
	if empty {
		label = o.placeholder
		if label == "" {
			label = Messages.PickDate
		}
	} else {
		label = value.Format(Messages.DateFormat)
	}
	if o.icon == nil {
		o.icon = lucide.Calendar
	}
	trigger := selectTrigger(label, empty, o)
	if o.disabled {
		return trigger
	}
	loc := time.Local
	if !empty {
		loc = value.Location()
	}
	now := time.Now().In(loc)
	openAt := func() {
		base := now
		if !empty {
			base = value
		}
		st.month = time.Date(base.Year(), base.Month(), 1, 0, 0, 0, 0, time.UTC)
	}
	trigger.OnClick(func() {
		st.open = !st.open
		if st.open {
			openAt()
		}
	}).Expanded(st.open)
	if !st.open {
		return trigger
	}
	if st.month.IsZero() {
		openAt()
	}
	pick := func(d time.Time) {
		st.open = false
		onChange(d)
	}
	shift := func(n int) func() { return func() { st.month = st.month.AddDate(0, n, 0) } }
	nav := fairy.Row(
		IconButton(lucide.ChevronLeft, shift(-1), Small),
		fairy.Text(st.month.Format(Messages.MonthFormat)).Medium().Grow(1).TextAlign(fairy.TextCenter),
		IconButton(lucide.ChevronRight, shift(1), Small),
	).Align(fairy.Center)
	week := fairy.Row().Role(fairy.RoleHidden)
	for _, d := range Messages.Weekdays {
		week.Child(fairy.Text(d).W(28).TextAlign(fairy.TextCenter).FontSize(12.8).Color(t.MutedForeground))
	}
	grid := fairy.Col().Gap(8)
	day := st.month.AddDate(0, 0, -int(st.month.Weekday()))
	for range 6 {
		row := fairy.Row().Gap(0)
		for range 7 {
			d := day
			day = day.AddDate(0, 0, 1)
			local := time.Date(d.Year(), d.Month(), d.Day(), 0, 0, 0, 0, loc)
			cell := fairy.Box(fairy.Text(fmt.Sprint(d.Day()))).Size(28, 28).Center().
				Radius(t.RadiusMd()).Border(1, fairy.Transparent).Animate(motion.Fast).Cursor(fairy.CursorPointer).
				OnClick(func() { pick(local) }).Role(fairy.RoleButton).Label(local.Format(Messages.DateFormat))
			hover := t.Muted
			if t.Dark {
				hover = alpha(t.Muted, 0.5)
			}
			switch {
			case !empty && sameDay(d, value):
				cell.Fill(t.Primary).Color(t.PrimaryForeground)
			case sameDay(d, now):
				cell.Fill(t.Muted).Color(t.Foreground)
			default:
				cell.HoverFill(hover).HoverColor(t.Foreground)
				if d.Month() != st.month.Month() {
					cell.Color(t.MutedForeground)
				}
			}
			row.Child(cell)
		}
		grid.Child(row)
	}
	today := time.Date(now.Year(), now.Month(), now.Day(), 0, 0, 0, 0, loc)
	panel := popupSurface(fairy.Col(nav, week, grid,
		fairy.Row(Button(Messages.Today, func() { pick(today) }, Ghost, Small)).Justify(fairy.JustifyEnd),
	).Gap(8).Pad(8), false)
	return trigger.Popover(panel, fairy.PopoverOptions{OnDismiss: func() { st.open = false }})
}

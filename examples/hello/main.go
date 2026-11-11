// Command hello is the smallest Fairy program: a window with a greeting, a text
// input and a click counter.
package main

import (
	"fmt"
	"log"

	"github.com/myparsleycat/fairy"
)

type state struct {
	count int
	name  string
}

func main() {
	s := &state{}
	err := fairy.Run(fairy.Options{
		Title:  "Hello",
		Width:  480,
		Height: 320,
		View: func(cx *fairy.Cx) *fairy.El {
			greeting := "Hello, world!"
			if s.name != "" {
				greeting = fmt.Sprintf("Hello, %s!", s.name)
			}
			return fairy.Col(
				fairy.Text(greeting).FontSize(22).SemiBold(),
				fairy.Input(s.name, func(v string) { s.name = v }).
					Placeholder("Enter your name").
					W(240).PadXY(10, 8).Radius(6).
					Border(1, fairy.Hex(0xd0d0d0)).FocusBorder(fairy.Hex(0x3b82f6)),
				fairy.Row(
					fairy.Text(fmt.Sprintf("Clicked %d times", s.count)),
					fairy.Text("+1").Color(fairy.Hex(0xffffff)).Medium().
						PadXY(14, 6).Radius(6).Fill(fairy.Hex(0x3b82f6)).
						HoverFill(fairy.Hex(0x2563eb)).PressFill(fairy.Hex(0x1d4ed8)).
						Cursor(fairy.CursorPointer).
						OnClick(func() { s.count++ }),
				).Gap(12).Align(fairy.Center),
			).WFull().HFull().Center().Gap(16)
		},
	})
	if err != nil {
		log.Fatal(err)
	}
}

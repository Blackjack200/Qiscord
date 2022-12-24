package main

import (
	"gioui.org/unit"
	"gioui.org/widget"
	"gioui.org/widget/material"
	"log"
	"os"

	"gioui.org/app"
	"gioui.org/font/gofont"
	"gioui.org/io/system"
	"gioui.org/layout"
	"gioui.org/op"
)

func main() {
	go func() {
		w := app.NewWindow(app.Title("Qiscord"))
		err := run(w)
		if err != nil {
			log.Fatal(err)
		}
		os.Exit(0)
	}()
	app.Main()
}

func run(w *app.Window) error {
	th := material.NewTheme(gofont.Collection())

	flex := &layout.Flex{}
	flex.Alignment = layout.Middle
	flex.Axis = layout.Vertical
	flex.Spacing = layout.SpaceAround

	l := &widget.Editor{}
	lw := material.Editor(th, l, "ff")

	b := &widget.Clickable{}
	bw := material.Button(th, b, "Start")
	margin := &layout.Inset{
		Top:    unit.Dp(25),
		Bottom: unit.Dp(25),
		Left:   unit.Dp(25),
		Right:  unit.Dp(25),
	}
	var ops op.Ops
	for {
		e := <-w.Events()
		switch e := e.(type) {
		case system.DestroyEvent:
			return e.Err
		case system.FrameEvent:
			gtx := layout.NewContext(&ops, e)
			flex.Layout(gtx, layout.Rigid(func(gtx layout.Context) layout.Dimensions {
				return lw.Layout(gtx)
			}), layout.Rigid(func(gtx layout.Context) layout.Dimensions {
				return margin.Layout(gtx, func(gtx layout.Context) layout.Dimensions {
					return bw.Layout(gtx)
				})
			}))
			e.Frame(gtx.Ops)
		}
	}
}

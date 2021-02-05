package main

import (
	"context"
	"flag"
	"log"
	"os"
	"strings"
	"time"

	"gioui.org/app"
	"gioui.org/io/system"
	"gioui.org/layout"
	"gioui.org/op"
	"gioui.org/unit"
	"gioui.org/widget"
	"gioui.org/widget/material"

	"gioui.org/font/gofont"
	"github.com/prometheus/client_golang/api"
	v1 "github.com/prometheus/client_golang/api/prometheus/v1"
	"github.com/prometheus/common/config"
)

func main() {
	promURL := flag.String("addr", "", "fully-qualified URL of prometheus instance")
	flag.Parse()
	client, err := api.NewClient(api.Config{
		Address:      *promURL,
		RoundTripper: config.NewBearerAuthRoundTripper(config.Secret(os.Getenv("PROM_TOKEN")), api.DefaultRoundTripper),
	})
	if err != nil {
		log.Fatal("Could not configure prom client", err)
	}

	go func() {
		w := app.NewWindow(app.Title("c-eye"))
		if err := loop(w, client); err != nil {
			log.Fatal(err)
		}
		os.Exit(0)
	}()
	app.Main()
}

type (
	C = layout.Context
	D = layout.Dimensions
)

func format(ed *widget.Editor) {
	defer func() {
		recover()
	}()
	start, end := ed.Selection()
	forward := true
	if end < start {
		forward = false
		start, end = end, start
	}
	text := ed.Text()
	before := text[:start]
	selected := text[start:end]
	after := text[end:]

	depth := 0
	for _, slice := range []*string{&before, &selected, &after} {
		var result strings.Builder
		for i, line := range strings.Split(*slice, "\n") {
			var leadingCloseParens int
			newLine := strings.TrimRight(strings.TrimLeft(line, " \t"), "\t\n")
			strings.TrimLeftFunc(newLine, func(r rune) bool {
				if r == ')' {
					leadingCloseParens++
					return true
				}
				return false
			})
			prefix := strings.Repeat("  ", depth-leadingCloseParens)
			depth += strings.Count(line, "(") - strings.Count(line, ")")
			if i > 0 {
				result.Write([]byte("\n"))
				result.Write([]byte(prefix))
			}
			result.Write([]byte(newLine))
		}
		*slice = result.String()
	}

	endStart := len(before)
	endEnd := len(selected) + endStart
	if !forward {
		endStart, endEnd = endEnd, endStart
	}
	finalText := before + selected + after
	if finalText != text {
		ed.SetText(finalText)
		ed.SetCaret(endStart, endEnd)
	}
}

func loop(w *app.Window, client api.Client) error {
	v1api := v1.NewAPI(client)
	queryResults := make(chan []string)

	th := material.NewTheme(gofont.Collection())
	var (
		ops      op.Ops
		editor   widget.Editor
		dataList layout.List
		data     []string
		inset    = layout.UniformInset(unit.Dp(4))
	)
	dataList.Axis = layout.Vertical
	for {
		select {
		case e := <-w.Events():
			switch e := e.(type) {
			case system.DestroyEvent:
				return e.Err
			case system.FrameEvent:
				gtx := layout.NewContext(&ops, e)
				var editorChanged = false
				for _, e := range editor.Events() {
					switch e.(type) {
					case widget.ChangeEvent:
						editorChanged = true
					}
				}
				if editorChanged {
					format(&editor)
					text := editor.Text()
					log.Println("query", text)
					go func() {
						ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
						defer cancel()
						log.Println("starting query")
						result, warnings, err := v1api.Query(ctx, text, time.Now())
						if err != nil {
							log.Println("failed querying", err)
							return
						}
						log.Println("query results", result)
						if len(warnings) > 0 {
							log.Println("warnings:", warnings)
						}
						queryResults <- strings.Split(result.String(), "\n")
						log.Println("query results sent")
					}()
				}
				layout.Flex{Axis: layout.Vertical}.Layout(gtx,
					layout.Rigid(func(gtx C) D {
						return inset.Layout(gtx, func(gtx C) D {
							return widget.Border{
								Width: unit.Dp(2),
								Color: th.Fg,
							}.Layout(gtx, func(gtx C) D {
								return inset.Layout(gtx, func(gtx C) D {
									gtx.Constraints.Min.X = gtx.Constraints.Max.X
									gtx.Constraints.Min.Y = 0
									ed := material.Editor(th, &editor, "query")
									ed.Font.Variant = "Mono"
									return ed.Layout(gtx)
								})
							})
						})
					}),
					layout.Flexed(1.0, func(gtx C) D {
						return inset.Layout(gtx, func(gtx C) D {
							return dataList.Layout(gtx, len(data), func(gtx C, index int) D {
								label := material.Body1(th, data[index])
								label.Font.Variant = "Mono"
								return label.Layout(gtx)
							})
						})
					}),
				)
				e.Frame(gtx.Ops)
			}
		case data = <-queryResults:
			w.Invalidate()
		}
	}
}

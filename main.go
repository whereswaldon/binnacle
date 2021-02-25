package main

import (
	"bytes"
	"context"
	"flag"
	"fmt"
	"image/color"
	"log"
	"os"
	"sort"
	"strings"
	"text/template"
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
	"github.com/prometheus/common/model"
	"github.com/whereswaldon/binnacle/latest"

	"gonum.org/v1/plot"
	"gonum.org/v1/plot/palette/moreland"
	"gonum.org/v1/plot/plotter"
	"gonum.org/v1/plot/vg"
	"gonum.org/v1/plot/vg/draw"
	"gonum.org/v1/plot/vg/vggio"
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
		w := app.NewWindow(app.Title("Binnacle"))
		if err := loop(w, client); err != nil {
			log.Fatal(err)
		}
		os.Exit(0)
	}()
	app.Main()
}

type Backend struct {
	v1.API

	Timeout time.Duration
	latest.Worker
}

func NewBackend(client api.Client) *Backend {
	b := &Backend{
		API:     v1.NewAPI(client),
		Timeout: time.Second * 10,
	}
	b.Worker = latest.NewWorker(func(in interface{}) interface{} {
		return b.Query(in.(string))
	})
	return b
}

func (b *Backend) Query(text string) queryResult {
	templ, err := template.New("query").Parse(text)
	if err != nil {
		return queryResult{
			error: fmt.Errorf("query is not a valid go template: %w", err),
		}
	}
	var buf bytes.Buffer
	err = templ.Execute(&buf, nil)
	if err != nil {
		return queryResult{
			error: fmt.Errorf("could not execute query template: %w", err),
		}
	}
	text = buf.String()
	var ctx context.Context
	ctx, _ = context.WithTimeout(context.Background(), b.Timeout)
	result, warnings, err := b.API.Query(ctx, text, time.Now())
	return queryResult{
		data:     result,
		warnings: warnings,
		error:    err,
	}
}

type (
	C = layout.Context
	D = layout.Dimensions
)

func format(ed *widget.Editor) {
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

type queryResult struct {
	data     model.Value
	warnings []string
	error
}

type Renderer struct {
	model.Value
	*material.Theme

	textDirty bool
	text      []string

	vizInit  bool
	vizDirty bool
	vizResult
	vizWorker latest.Worker
}

func NewRenderer(th *material.Theme) *Renderer {
	r := &Renderer{
		Theme: th,
	}
	render := func(input interface{}) interface{} {
		return RenderVizData(input.(vizData))
	}
	r.vizWorker = latest.NewWorker(render)
	return r
}

type vizResult struct {
	ops         op.Ops
	call        op.CallOp
	constraints layout.Constraints
	dims        layout.Dimensions
}

type vizData struct {
	model.Value
	layout.Context
}

func (r *Renderer) SetData(m model.Value) {
	r.Value = m
	r.textDirty = true
	r.vizDirty = true
}

func (r *Renderer) RenderText() []string {
	if !r.textDirty {
		return r.text
	}
	r.textDirty = false
	r.text = strings.Split(r.Value.String(), "\n")
	sort.SliceStable(r.text, func(i, j int) bool {
		return strings.Compare(r.text[i], r.text[j]) < 0
	})
	return r.text
}

func (r *Renderer) RenderViz(gtx C) D {
	select {
	case result := <-r.vizWorker.Raw():
		r.vizResult = result.(vizResult)
		r.vizInit = true
	default:
	}
	if gtx.Constraints != r.constraints {
		r.vizDirty = true
	}
	if r.vizInit && !r.vizDirty {
		r.call.Add(gtx.Ops)
		return r.dims
	}
	if r.vizDirty {
		r.vizDirty = false
		r.vizWorker.Push(vizData{
			Value:   r.Value,
			Context: gtx,
		})
	}
	op.InvalidateOp{}.Add(gtx.Ops)
	return layout.Center.Layout(gtx, func(gtx C) D {
		return material.Loader(r.Theme).Layout(gtx)
	})
}

func min(in []*model.Sample) float64 {
	m := in[0].Value
	for i := range in {
		if i == 0 {
			continue
		}
		if in[i].Value < m {
			m = in[i].Value
		}
	}
	return float64(m)
}

func max(in []*model.Sample) float64 {
	m := in[0].Value
	for i := range in {
		if i == 0 {
			continue
		}
		if in[i].Value > m {
			m = in[i].Value
		}
	}
	return float64(m)
}

func RenderVizData(data vizData) vizResult {
	switch value := data.Value.(type) {
	case model.Vector:
		return RenderVector(data.Context, value)
	case *model.Scalar:
		log.Println("scalar visualization is not yet supported")
	case model.Matrix:
		log.Println("matrix visualization is not yet supported")
	case *model.String:
		log.Println("string visualization is not yet supported")
	default:
		log.Println("no data to visualize")
	}
	return vizResult{}
}

func RenderVector(gtx C, data model.Vector) vizResult {
	if len(data) < 1 {
		return vizResult{}
	}
	sort.SliceStable(data, func(i, j int) bool {
		return strings.Compare(data[i].Metric.String(), data[j].Metric.String()) < 0
	})
	p, err := plot.New()
	if err != nil {
		log.Printf("Failed constructing plot: %v", err)
		return vizResult{}
	}
	var result vizResult
	l := moreland.BlackBody()
	minData := min([]*model.Sample(data))
	maxData := max([]*model.Sample(data))
	l.SetMin(minData)
	l.SetMax(maxData)
	values := make([]plotter.Values, len(data))
	labels := make([]string, len(data))
	for i := range values {
		labels[i] = data[i].Metric.String()
		values[i] = make(plotter.Values, len(data))
		values[i][i] = float64(data[i].Value)
		chart, err := plotter.NewBarChart(values[i], 0.5*vg.Centimeter)
		if err != nil {
			log.Printf("Failed creating bar chart: %v", err)
			return vizResult{}
		}
		chart.Color, _ = l.At(values[i][i])
		chart.Horizontal = true
		p.Add(chart)
	}
	p.NominalY(labels...)
	oldOps := gtx.Ops
	gtx.Ops = &result.ops

	macro := op.Record(gtx.Ops)
	cnv := vggio.New(gtx, vg.Points(float64(gtx.Constraints.Max.X*3/4)), vg.Points(float64(gtx.Constraints.Max.Y*3/4)))
	p.Draw(draw.New(cnv))
	gtx.Ops = cnv.Paint()
	call := macro.Stop()
	result.constraints = gtx.Constraints
	result.call = call
	result.dims = D{Size: gtx.Constraints.Max}
	gtx.Ops = oldOps
	return result
}

func loop(w *app.Window, client api.Client) error {
	th := material.NewTheme(gofont.Collection())
	backEnd := NewBackend(client)
	renderer := NewRenderer(th)
	var (
		ops          op.Ops
		editor       widget.Editor
		dataList     layout.List
		warnings     []string
		warningsList layout.List
		errorText    string
		inset        = layout.UniformInset(unit.Dp(4))
	)
	dataList.Axis = layout.Vertical
	warningsList.Axis = layout.Vertical
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
					backEnd.Push(editor.Text())
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
					layout.Rigid(func(gtx C) D {
						if len(errorText) == 0 {
							return D{}
						}
						return inset.Layout(gtx, func(gtx C) D {
							label := material.Body1(th, errorText)
							label.Font.Variant = "Mono"
							label.Color = color.NRGBA{R: 0x6e, G: 0x0a, B: 0x1e, A: 255}
							return label.Layout(gtx)
						})
					}),
					layout.Rigid(func(gtx C) D {
						if len(warnings) == 0 {
							return D{}
						}
						return inset.Layout(gtx, func(gtx C) D {
							return warningsList.Layout(gtx, len(warnings), func(gtx C, index int) D {
								label := material.Body1(th, warnings[index])
								label.Font.Variant = "Mono"
								label.Color = color.NRGBA{R: 0xd4, G: 0xaf, B: 0x37, A: 255}
								return label.Layout(gtx)
							})
						})
					}),
					layout.Flexed(1.0, func(gtx C) D {
						return layout.Flex{}.Layout(gtx,
							layout.Flexed(.5, func(gtx C) D {
								return inset.Layout(gtx, func(gtx C) D {
									data := renderer.RenderText()
									return dataList.Layout(gtx, len(data), func(gtx C, index int) D {
										label := material.Body1(th, data[index])
										label.Font.Variant = "Mono"
										return label.Layout(gtx)
									})
								})
							}),
							layout.Flexed(.5, func(gtx C) D {
								return inset.Layout(gtx, func(gtx C) D {
									return renderer.RenderViz(gtx)
								})
							}),
						)
					}),
				)
				e.Frame(gtx.Ops)
			}
		case data := <-backEnd.Raw():
			result := data.(queryResult)
			if result.error != nil {
				errorText = result.Error()
				warnings = nil
			} else {
				renderer.SetData(result.data)
				warnings = result.warnings
				errorText = ""
			}
			w.Invalidate()
		}
	}
}

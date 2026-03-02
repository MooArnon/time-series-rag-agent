package plot

import (
	"fmt"
	"image/color"
	"math"
	"os"
	"time"

	"gonum.org/v1/plot"
	"gonum.org/v1/plot/plotter"
	"gonum.org/v1/plot/vg"
	"gonum.org/v1/plot/vg/draw"
	"gonum.org/v1/plot/vg/vgimg"

	"time-series-rag-agent/internal/ai"
)

// --- Binance Colors ---
var (
	BgDark      = color.RGBA{R: 22, G: 26, B: 37, A: 255}    // #161a25
	GridDark    = color.RGBA{R: 43, G: 47, B: 58, A: 255}    // #2b2f3a
	TextLight   = color.RGBA{R: 183, G: 189, B: 198, A: 255} // #b7bdc6
	BinanceUp   = color.RGBA{R: 14, G: 203, B: 129, A: 255}  // #0ecb81 (Green)
	BinanceDown = color.RGBA{R: 246, G: 70, B: 93, A: 255}   // #f6465d (Red)
	Ma7Color    = color.RGBA{R: 240, G: 185, B: 11, A: 255}  // Yellow
	Ma25Color   = color.RGBA{R: 160, G: 32, B: 240, A: 255}  // Purple
	Ma99Color   = color.RGBA{R: 216, G: 64, B: 174, A: 255}  // Pink
)

// --- 1. Custom Candlestick Plotter ---
type OHLC struct {
	Open, High, Low, Close float64
}

type V struct {
	Volume float64
	Up     bool
}

type Candles struct {
	Data []OHLC
}

type VolumeCandle struct {
	Data []V
}

// Plot implements the Plotter interface to draw volume bars
func (vc *VolumeCandle) Plot(canvas draw.Canvas, plt *plot.Plot) {
	trX, trY := plt.Transforms(&canvas)

	w := canvas.Rectangle.Max.X - canvas.Rectangle.Min.X
	barWidth := (w / vg.Length(len(vc.Data))) * 0.6 // Use 60% of available slot

	for i, v := range vc.Data {
		x := trX(float64(i))

		// Choose color based on up/down
		col := BinanceDown
		if v.Up {
			col = BinanceUp
		}

		// Draw volume bar from 0 to the volume value
		rect := vg.Rectangle{
			Min: vg.Point{X: x - barWidth/2, Y: trY(0)},
			Max: vg.Point{X: x + barWidth/2, Y: trY(v.Volume)},
		}

		canvas.SetColor(col)
		canvas.Fill(rect.Path())
	}
}

// Plot implements the Plotter interface to draw candles manually
func (c *Candles) Plot(canvas draw.Canvas, plt *plot.Plot) {
	trX, trY := plt.Transforms(&canvas)

	// Width of the candle body (in points)
	// We calculate it dynamically based on chart width to prevent overlap
	w := canvas.Rectangle.Max.X - canvas.Rectangle.Min.X
	barWidth := (w / vg.Length(len(c.Data))) * 0.6 // Use 60% of available slot

	for i, d := range c.Data {
		x := trX(float64(i))

		// 1. Determine Color
		col := BinanceDown
		if d.Close >= d.Open {
			col = BinanceUp
		}

		// 2. Draw Wick (High to Low line)
		lineStyle := draw.LineStyle{Color: col, Width: vg.Points(1)}
		canvas.StrokeLine2(lineStyle, x, trY(d.Low), x, trY(d.High))

		// 3. Draw Body (Open to Close rect)
		// Handle the case where Open == Close (Doji)
		top := math.Max(d.Open, d.Close)
		bottom := math.Min(d.Open, d.Close)
		if top == bottom {
			top += 0.00001 // Ensure at least a thin line exists
		}

		rect := vg.Rectangle{
			Min: vg.Point{X: x - barWidth/2, Y: trY(bottom)},
			Max: vg.Point{X: x + barWidth/2, Y: trY(top)},
		}
		// 1. Set the color state on the canvas
		canvas.SetColor(col)

		// 2. Fill the path (uses the active color)
		canvas.Fill(rect.Path())
	}
}

// DataRange returns the bounding box of the data
func (c *Candles) DataRange() (xmin, xmax, ymin, ymax float64) {
	ymin = math.Inf(1)
	ymax = math.Inf(-1)
	for _, d := range c.Data {
		if d.Low < ymin {
			ymin = d.Low
		}
		if d.High > ymax {
			ymax = d.High
		}
	}
	return 0, float64(len(c.Data)), ymin, ymax
}

// DataRange returns the bounding box of the volume data
func (vc *VolumeCandle) DataRange() (xmin, xmax, ymin, ymax float64) {
	ymin = 0 // Always start at 0 - volume bars grow from baseline
	ymax = 0
	for _, v := range vc.Data {
		if v.Volume > ymax {
			ymax = v.Volume
		}
	}
	return 0, float64(len(vc.Data)), ymin, ymax
}
func (vc *VolumeCandle) VDataRange() (xmin, xmax, ymin, ymax float64) {
	ymin = 0
	ymax = 0
	for _, v := range vc.Data {
		if v.Volume > ymax {
			ymax = v.Volume
		}
	}
	return 0, float64(len(vc.Data)), ymin, ymax
}

// GlyphBoxes returns nil (no glyphs for volume bars)
func (vc *VolumeCandle) GlyphBoxes(plt *plot.Plot) []plot.GlyphBox {
	return nil
}

func (c *Candles) GlyphBoxes(plt *plot.Plot) []plot.GlyphBox { return nil }

// --- 2. Helper: Simple Moving Average ---
func calculateSMA(data []float64, period int) []float64 {
	sma := make([]float64, len(data))
	for i := 0; i < len(data); i++ {
		if i < period-1 {
			sma[i] = math.NaN() // Not enough data yet
			continue
		}
		sum := 0.0
		for j := 0; j < period; j++ {
			sum += data[i-j]
		}
		sma[i] = sum / float64(period)
	}
	return sma
}

// --- 3. Main Chart Generation Function ---
// ... (Imports and Ticker struct remain the same) ...

func GenerateCandleChart(candles []ai.InputData, filename string) error {

	pricePlot := plot.New()
	volumePlot := plot.New()

	pricePlot.BackgroundColor = BgDark
	pricePlot.Title.Text = fmt.Sprintf("Price Action [%s]", time.Now().Format("15:04"))
	pricePlot.Title.TextStyle.Color = TextLight

	pricePlot.X.Tick.Label.Color = TextLight
	pricePlot.Y.Tick.Label.Color = TextLight
	pricePlot.X.Tick.LineStyle.Color = TextLight
	pricePlot.Y.Tick.LineStyle.Color = TextLight

	grid := plotter.NewGrid()
	grid.Vertical.Color = GridDark
	grid.Horizontal.Color = GridDark
	pricePlot.Add(grid)

	volumePlot.BackgroundColor = BgDark
	volumePlot.X.Tick.Label.Color = TextLight
	volumePlot.Y.Tick.Label.Color = TextLight
	volumePlot.X.Tick.LineStyle.Color = TextLight
	volumePlot.Y.Tick.LineStyle.Color = TextLight

	ohlcData := make([]OHLC, len(candles))
	vData := make([]V, len(candles))
	closePrices := make([]float64, len(candles))

	for i, c := range candles {
		ohlcData[i] = OHLC{
			Open:  c.Open,
			High:  c.High,
			Low:   c.Low,
			Close: c.Close,
		}
		vData[i] = V{
			Volume: c.Volume,
			Up:     c.Close >= c.Open,
		}
		closePrices[i] = c.Close
	}

	candlePlot := &Candles{Data: ohlcData}
	volumeBars := &VolumeCandle{Data: vData}

	pricePlot.Add(candlePlot)
	volumePlot.Add(volumeBars)

	// Sync X range
	pricePlot.X.Min = 0
	pricePlot.X.Max = float64(len(candles))

	volumePlot.X.Min = 0
	volumePlot.X.Max = float64(len(candles))

	// Volume should start at 0
	volumePlot.Y.Min = 0

	addMA := func(period int, col color.RGBA) {
		maData := calculateSMA(closePrices, period)
		pts := make(plotter.XYs, 0)

		for i, v := range maData {
			if !math.IsNaN(v) {
				pts = append(pts, plotter.XY{
					X: float64(i),
					Y: v,
				})
			}
		}

		line, _ := plotter.NewLine(pts)
		line.LineStyle.Color = col
		line.LineStyle.Width = vg.Points(1.5)

		pricePlot.Add(line)
		pricePlot.Legend.Add(fmt.Sprintf("MA(%d)", period), line)
	}

	addMA(7, Ma7Color)
	addMA(25, Ma25Color)
	addMA(99, Ma99Color)

	pricePlot.Legend.Top = true
	pricePlot.Legend.Left = true
	pricePlot.Legend.TextStyle.Color = TextLight

	img := vgimg.New(12*vg.Inch, 6*vg.Inch)
	dc := draw.New(img)

	tiles := draw.Tiles{
		Rows: 2,
		Cols: 1,
	}

	plots := [][]*plot.Plot{
		{pricePlot},
		{volumePlot},
	}

	canvases := plot.Align(plots, tiles, dc)

	pricePlot.Draw(canvases[0][0])
	volumePlot.Draw(canvases[1][0])

	w, err := os.Create(filename)
	if err != nil {
		return err
	}
	defer w.Close()

	png := vgimg.PngCanvas{Canvas: img}
	_, err = png.WriteTo(w)

	return err
}

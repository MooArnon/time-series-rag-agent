package plot

import (
	"fmt"
	"image/color"
	"math"
	"time"

	"gonum.org/v1/plot"
	"gonum.org/v1/plot/plotter"
	"gonum.org/v1/plot/vg"
	"gonum.org/v1/plot/vg/draw"

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

type Candles struct {
	Data []OHLC
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
	p := plot.New()

	// 1. Styling
	p.BackgroundColor = BgDark
	p.Title.Text = fmt.Sprintf("Price Action [%s]", time.Now().Format("15:04"))
	p.Title.TextStyle.Color = TextLight
	p.X.Label.TextStyle.Color = TextLight
	p.Y.Label.TextStyle.Color = TextLight
	p.X.Tick.Label.Color = TextLight
	p.Y.Tick.Label.Color = TextLight
	p.X.Tick.LineStyle.Color = TextLight
	p.Y.Tick.LineStyle.Color = TextLight

	// Grid
	grid := plotter.NewGrid()
	grid.Vertical.Color = GridDark
	grid.Horizontal.Color = GridDark
	p.Add(grid)

	// 2. Prepare Data
	ohlcData := make([]OHLC, len(candles))
	closePrices := make([]float64, len(candles))

	for i, c := range candles {
		ohlcData[i] = OHLC{
			Open:  c.Open,
			High:  c.High,
			Low:   c.Low,
			Close: c.Close,
		}
		closePrices[i] = c.Close
	}

	// 3. Add Candles
	candlePlot := &Candles{Data: ohlcData}
	p.Add(candlePlot)

	// 4. Add Moving Averages & Legend
	addMA := func(period int, col color.RGBA) {
		maData := calculateSMA(closePrices, period)
		pts := make(plotter.XYs, 0)
		for i, v := range maData {
			if !math.IsNaN(v) {
				pts = append(pts, plotter.XY{X: float64(i), Y: v})
			}
		}
		line, _ := plotter.NewLine(pts)
		line.LineStyle.Color = col
		line.LineStyle.Width = vg.Points(1.5)
		p.Add(line)

		// --- ADD LABEL HERE ---
		// This adds the entry to the legend box
		p.Legend.Add(fmt.Sprintf("MA(%d)", period), line)
	}

	addMA(7, Ma7Color)   // Yellow
	addMA(25, Ma25Color) // Purple
	addMA(99, Ma99Color) // Pink

	// --- Configure Legend Style ---
	p.Legend.Top = true
	p.Legend.Left = true
	p.Legend.Padding = vg.Points(5)
	p.Legend.TextStyle.Color = TextLight
	p.Legend.TextStyle.Font.Size = vg.Points(10)
	// Transparent background for the legend box so it doesn't block candles
	p.Legend.ThumbnailWidth = vg.Points(20)

	// 6. Save
	if err := p.Save(12*vg.Inch, 6*vg.Inch, filename); err != nil {
		return err
	}
	return nil
}

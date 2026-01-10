package plot

import (
	"fmt"
	"image/color"
	"math"
	"time"

	"gonum.org/v1/plot"
	"gonum.org/v1/plot/plotter"
	"gonum.org/v1/plot/vg"

	"time-series-rag-agent/internal/ai"
)

// Helper: Cumulative Sum
func cumSum(data []float64) []float64 {
	res := make([]float64, len(data))
	sum := 0.0
	for i, v := range data {
		sum += v
		res[i] = sum
	}
	return res
}

func GeneratePredictionChart(currentEmbedding []float64, matches []ai.PatternLabel, filename string) error {
	p := plot.New()

	p.Title.Text = fmt.Sprintf("AI Pattern Projection [%s]", time.Now().Format("15:04"))
	p.X.Label.Text = "Time Steps (Left=History | Right=Future)"
	p.Y.Label.Text = "Cumulative Z-Score"
	p.BackgroundColor = color.White

	// Colors
	colGreen := color.RGBA{R: 46, G: 204, B: 113, A: 255}
	colRed := color.RGBA{R: 231, G: 76, B: 60, A: 255}
	colBlack := color.RGBA{R: 0, G: 0, B: 0, A: 255}
	colBlue := color.RGBA{R: 52, G: 152, B: 219, A: 255}

	// Grid
	grid := plotter.NewGrid()
	grid.Vertical.Color = color.Gray{220}
	grid.Horizontal.Color = color.Gray{220}
	p.Add(grid)

	// Settings
	lookback := float64(len(currentEmbedding)) - 1
	futureSteps := 15.0
	const slopeScale = 2000.0

	// Track Min/Max for Autoscaling
	yMin, yMax := math.Inf(1), math.Inf(-1)

	// Helper to update limits
	updateLimits := func(y float64) {
		if y < yMin {
			yMin = y
		}
		if y > yMax {
			yMax = y
		}
	}

	// --- 1. Plot Matches ---
	for _, m := range matches {
		if len(m.Embedding) == 0 {
			continue
		}

		shapeData := cumSum(m.Embedding)

		// Update limits based on history
		for _, v := range shapeData {
			updateLimits(v)
		}

		// Plot Shape (Left)
		shapePts := make(plotter.XYs, len(shapeData))
		for i, v := range shapeData {
			shapePts[i].X = float64(i)
			shapePts[i].Y = v
		}
		lineLeft, _ := plotter.NewLine(shapePts)
		lineLeft.LineStyle.Width = vg.Points(1.5)

		// Plot Projection (Right)
		lastY := shapeData[len(shapeData)-1]
		endY := lastY + (m.NextSlope3 * slopeScale)

		// Update limits based on projection
		updateLimits(endY)

		lineRight, _ := plotter.NewLine(plotter.XYs{
			{X: lookback, Y: lastY},
			{X: lookback + futureSteps, Y: endY},
		})
		lineRight.LineStyle.Width = vg.Points(1.5)
		lineRight.LineStyle.Dashes = []vg.Length{vg.Points(4), vg.Points(2)}

		// Color Logic
		if m.NextSlope3 > 0 {
			lineLeft.LineStyle.Color = colGreen
			lineRight.LineStyle.Color = colGreen
		} else {
			lineLeft.LineStyle.Color = colRed
			lineRight.LineStyle.Color = colRed
		}
		p.Add(lineLeft, lineRight)
	}

	// --- 2. Plot Current Market ---
	currentShape := cumSum(currentEmbedding)
	currentPts := make(plotter.XYs, len(currentShape))
	for i, v := range currentShape {
		currentPts[i].X = float64(i)
		currentPts[i].Y = v
		updateLimits(v) // Check current market limits too
	}

	lineCurrent, _ := plotter.NewLine(currentPts)
	lineCurrent.LineStyle.Width = vg.Points(3)
	lineCurrent.LineStyle.Color = colBlack
	p.Add(lineCurrent)

	// --- 3. Dynamic Vertical Line ---
	// Add 10% padding to the limits so lines don't touch the edge
	yRange := yMax - yMin
	if yRange == 0 {
		yRange = 1
	} // prevent div/0
	pad := yRange * 0.1
	plotMin := yMin - pad
	plotMax := yMax + pad

	cutoffLine, _ := plotter.NewLine(plotter.XYs{
		{X: lookback, Y: plotMin},
		{X: lookback, Y: plotMax},
	})
	cutoffLine.LineStyle.Color = colBlue
	cutoffLine.LineStyle.Width = vg.Points(1.5)
	cutoffLine.LineStyle.Dashes = []vg.Length{vg.Points(2), vg.Points(2)}
	p.Add(cutoffLine)

	// --- 4. Final Scale ---
	p.X.Min = 0
	p.X.Max = lookback + futureSteps + 2
	p.Y.Min = plotMin
	p.Y.Max = plotMax

	if err := p.Save(12*vg.Inch, 6*vg.Inch, filename); err != nil {
		return err
	}
	return nil
}

package layout

import "math"

type Grid struct {
	Rows, Cols int
}

type CellSize struct {
	Width, Height int
}

// Compute returns a near-square grid layout for n panels.
func Compute(n int) Grid {
	if n <= 0 {
		return Grid{1, 1}
	}
	cols := int(math.Ceil(math.Sqrt(float64(n))))
	rows := int(math.Ceil(float64(n) / float64(cols)))
	return Grid{Rows: rows, Cols: cols}
}

// CellSizes returns the width and height for each cell in the grid,
// distributing any remainder pixels to the last column/row.
func CellSizes(totalWidth, totalHeight, rows, cols int) CellSize {
	if cols <= 0 || rows <= 0 {
		return CellSize{totalWidth, totalHeight}
	}
	return CellSize{
		Width:  totalWidth / cols,
		Height: totalHeight / rows,
	}
}

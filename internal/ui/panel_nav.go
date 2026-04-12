// SPDX-License-Identifier: MIT
package ui

import (
	"fmt"

	"muxedo/internal/layout"
)

// panelIndexFromDigit maps '1'..'9' to panel indices 0..8. Returns ok false if out of range.
func panelIndexFromDigit(r rune, nPanels int) (idx int, ok bool) {
	if nPanels <= 0 || r < '1' || r > '9' {
		return 0, false
	}
	idx = int(r - '1')
	if idx >= nPanels {
		return 0, false
	}
	return idx, true
}

// neighborPanelIndex returns an adjacent panel index for hjkl in row-major grid order.
func neighborPanelIndex(grid layout.Grid, nPanels, idx int, r rune) (next int, ok bool) {
	if nPanels <= 0 || idx < 0 || idx >= nPanels {
		return 0, false
	}
	cols := grid.Cols
	rows := grid.Rows
	if cols <= 0 || rows <= 0 {
		return 0, false
	}
	row := idx / cols
	col := idx % cols
	switch r {
	case 'h':
		if col <= 0 {
			return 0, false
		}
		return idx - 1, true
	case 'l':
		if col+1 >= cols {
			return 0, false
		}
		next = idx + 1
		if next >= nPanels {
			return 0, false
		}
		return next, true
	case 'k':
		if row <= 0 {
			return 0, false
		}
		next = idx - cols
		if next < 0 || next >= nPanels {
			return 0, false
		}
		return next, true
	case 'j':
		if row+1 >= rows {
			return 0, false
		}
		next = idx + cols
		if next >= nPanels {
			return 0, false
		}
		return next, true
	default:
		return 0, false
	}
}

func (m *Model) applyPanelFocus(next int) {
	n := len(m.panels)
	if n == 0 || next < 0 || next >= n {
		return
	}
	m.activePanel = next
	m.panelInsertMode = false
	m.panelScrollMode = false
	m.panelSelectMode = false
	m.clearActiveSelection()
	if m.maximizedPanel >= 0 {
		m.maximizedPanel = next
		m.resizePanels()
	}
}

func formatPanelTitle(idx int, name string) string {
	return fmt.Sprintf("[%d] %s", idx+1, name)
}

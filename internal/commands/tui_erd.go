package commands

import (
	"fmt"
	"strings"

	"github.com/charmbracelet/lipgloss"
	"github.com/charmbracelet/x/ansi"
)

type erdStyle uint8

const (
	erdStyleNormal erdStyle = iota
	erdStyleMuted
	erdStyleAccent
	erdStylePrimary
	erdStyleHighlight
)

type erdRect struct {
	x, y, w, h int
}

type erdCanvas struct {
	w, h   int
	runes  [][]rune
	styles [][]erdStyle
}

func newERDCanvas(w, h int) *erdCanvas {
	if w < 1 {
		w = 1
	}
	if h < 1 {
		h = 1
	}
	runes := make([][]rune, h)
	styles := make([][]erdStyle, h)
	for y := 0; y < h; y++ {
		row := make([]rune, w)
		for x := 0; x < w; x++ {
			row[x] = ' '
		}
		runes[y] = row
		styles[y] = make([]erdStyle, w)
	}
	return &erdCanvas{w: w, h: h, runes: runes, styles: styles}
}

func (c *erdCanvas) set(x, y int, ch rune, st erdStyle) {
	if x < 0 || y < 0 || x >= c.w || y >= c.h {
		return
	}
	c.runes[y][x] = ch
	c.styles[y][x] = st
}

func (c *erdCanvas) drawText(x, y int, s string, st erdStyle) {
	if y < 0 || y >= c.h {
		return
	}
	col := x
	for _, r := range s {
		if col >= c.w {
			break
		}
		if col >= 0 {
			c.runes[y][col] = r
			c.styles[y][col] = st
		}
		col++
	}
}

func (c *erdCanvas) drawBox(r erdRect, title string, lines []string, borderSt, titleSt, bodySt erdStyle) {
	if r.w < 4 || r.h < 3 {
		return
	}
	x0, y0, x1, y1 := r.x, r.y, r.x+r.w-1, r.y+r.h-1
	c.set(x0, y0, '╭', borderSt)
	c.set(x1, y0, '╮', borderSt)
	c.set(x0, y1, '╰', borderSt)
	c.set(x1, y1, '╯', borderSt)
	for x := x0 + 1; x < x1; x++ {
		c.set(x, y0, '─', borderSt)
		c.set(x, y1, '─', borderSt)
	}
	for y := y0 + 1; y < y1; y++ {
		c.set(x0, y, '│', borderSt)
		c.set(x1, y, '│', borderSt)
	}

	innerW := r.w - 4
	if innerW < 1 {
		innerW = 1
	}
	t := title
	if lipgloss.Width(t) > innerW {
		t = ansi.Truncate(t, innerW-1, "…")
	}
	c.drawText(x0+2, y0+1, t, titleSt)

	if len(lines) > 0 && r.h >= 5 {
		c.set(x0, y0+2, '├', borderSt)
		c.set(x1, y0+2, '┤', borderSt)
		for x := x0 + 1; x < x1; x++ {
			c.set(x, y0+2, '─', borderSt)
		}
		for i, line := range lines {
			ly := y0 + 3 + i
			if ly >= y1 {
				break
			}
			if lipgloss.Width(line) > innerW {
				line = ansi.Truncate(line, innerW-1, "…")
			}
			c.drawText(x0+2, ly, line, bodySt)
		}
	}
}

func mergeBoxRune(existing, incoming rune) rune {
	if existing == ' ' || existing == incoming {
		return incoming
	}
	mask := func(r rune) int {
		switch r {
		case '─':
			return 0b0011 // L, R
		case '│':
			return 0b1100 // U, D
		case '┌', '╭':
			return 0b0101 // D, R
		case '┐', '╮':
			return 0b0110 // D, L
		case '└', '╰':
			return 0b1001 // U, R
		case '┘', '╯':
			return 0b1010 // U, L
		case '├':
			return 0b1101 // U, D, R
		case '┤':
			return 0b1110 // U, D, L
		case '┬':
			return 0b0111 // D, L, R
		case '┴':
			return 0b1011 // U, L, R
		case '┼':
			return 0b1111
		default:
			return 0
		}
	}
	m1, m2 := mask(existing), mask(incoming)
	if m1 == 0 || m2 == 0 {
		return incoming
	}
	switch m1 | m2 {
	case 0b0011:
		return '─'
	case 0b1100:
		return '│'
	case 0b0101:
		return '┌'
	case 0b0110:
		return '┐'
	case 0b1001:
		return '└'
	case 0b1010:
		return '┘'
	case 0b1101:
		return '├'
	case 0b1110:
		return '┤'
	case 0b0111:
		return '┬'
	case 0b1011:
		return '┴'
	case 0b1111:
		return '┼'
	default:
		return incoming
	}
}

func (c *erdCanvas) drawLineRune(x, y int, ch rune, st erdStyle) {
	if x < 0 || y < 0 || x >= c.w || y >= c.h {
		return
	}
	ex := c.runes[y][x]
	// Don't overwrite box borders/text if something is already there unless it's a line rune.
	if ex != ' ' && ex != '─' && ex != '│' && ex != '┌' && ex != '┐' && ex != '└' && ex != '┘' && ex != '├' && ex != '┤' && ex != '┬' && ex != '┴' && ex != '┼' {
		return
	}
	c.runes[y][x] = mergeBoxRune(ex, ch)
	if st > c.styles[y][x] {
		c.styles[y][x] = st
	}
}

func (c *erdCanvas) drawHLine(x1, x2, y int, st erdStyle) {
	if x1 > x2 {
		x1, x2 = x2, x1
	}
	for x := x1; x <= x2; x++ {
		c.drawLineRune(x, y, '─', st)
	}
}

func (c *erdCanvas) drawVLine(x, y1, y2 int, st erdStyle) {
	if y1 > y2 {
		y1, y2 = y2, y1
	}
	for y := y1; y <= y2; y++ {
		c.drawLineRune(x, y, '│', st)
	}
}

func (c *erdCanvas) renderWindow(xOffset, yOffset, viewW, viewH int) string {
	if viewW < 1 {
		viewW = c.w
	}
	if viewH < 1 {
		viewH = c.h
	}
	if xOffset < 0 {
		xOffset = 0
	}
	if yOffset < 0 {
		yOffset = 0
	}
	endY := min(c.h, yOffset+viewH)
	endX := min(c.w, xOffset+viewW)

	sMuted := styleMutedText
	sAccent := lipgloss.NewStyle().Bold(true).Foreground(colorAccent)
	sPrimary := lipgloss.NewStyle().Bold(true).Foreground(colorPrimary)
	sHighlight := lipgloss.NewStyle().Foreground(colorAccent)

	renderSeg := func(text string, st erdStyle) string {
		switch st {
		case erdStyleMuted:
			return sMuted.Render(text)
		case erdStyleAccent:
			return sAccent.Render(text)
		case erdStylePrimary:
			return sPrimary.Render(text)
		case erdStyleHighlight:
			return sHighlight.Render(text)
		default:
			return text
		}
	}

	var b strings.Builder
	for y := yOffset; y < endY; y++ {
		if y > yOffset {
			b.WriteByte('\n')
		}
		if xOffset >= c.w {
			continue
		}
		// Trim trailing spaces on each visible row for cleaner output.
		rowEnd := endX
		for rowEnd > xOffset && c.runes[y][rowEnd-1] == ' ' {
			rowEnd--
		}
		if rowEnd <= xOffset {
			continue
		}
		curStyle := c.styles[y][xOffset]
		var seg strings.Builder
		for x := xOffset; x < rowEnd; x++ {
			st := c.styles[y][x]
			if st != curStyle {
				b.WriteString(renderSeg(seg.String(), curStyle))
				seg.Reset()
				curStyle = st
			}
			seg.WriteRune(c.runes[y][x])
		}
		if seg.Len() > 0 {
			b.WriteString(renderSeg(seg.String(), curStyle))
		}
	}
	return b.String()
}

type erdLayoutResult struct {
	canvas     *erdCanvas
	rects      map[string]erdRect
	cursorRect erdRect
}

func orderTablesForERD(tables []string, graph *tableSchemaGraph) []string {
	if graph == nil || len(graph.relations) == 0 || len(tables) <= 2 {
		return tables
	}
	adj := make(map[string][]string, len(tables))
	inDeg := make(map[string]int, len(tables))
	for _, t := range tables {
		inDeg[t] = 0
	}
	for _, r := range graph.relations {
		adj[r.toTable] = append(adj[r.toTable], r.fromTable)
		adj[r.fromTable] = append(adj[r.fromTable], r.toTable)
		inDeg[r.fromTable]++
	}

	visited := make(map[string]bool, len(tables))
	ordered := make([]string, 0, len(tables))

	// Visit connected tables starting from roots (tables with relationships and lowest in-degree).
	bfs := func(start string) {
		queue := []string{start}
		visited[start] = true
		for len(queue) > 0 {
			cur := queue[0]
			queue = queue[1:]
			ordered = append(ordered, cur)
			for _, nb := range adj[cur] {
				if !visited[nb] {
					visited[nb] = true
					queue = append(queue, nb)
				}
			}
		}
	}

	for _, t := range tables {
		if !visited[t] && len(adj[t]) > 0 && inDeg[t] == 0 {
			bfs(t)
		}
	}
	for _, t := range tables {
		if !visited[t] && len(adj[t]) > 0 {
			bfs(t)
		}
	}
	for _, t := range tables {
		if !visited[t] {
			ordered = append(ordered, t)
		}
	}
	return ordered
}

func buildTableOverviewERD(tables []string, graph *tableSchemaGraph, cursor int, focused bool, showFields bool) *erdLayoutResult {
	return buildTableOverviewERDWithZoom(tables, graph, cursor, focused, showFields, 1)
}

func buildTableOverviewERDWithZoom(tables []string, graph *tableSchemaGraph, cursor int, focused bool, showFields bool, zoom int) *erdLayoutResult {
	if len(tables) == 0 {
		return &erdLayoutResult{canvas: newERDCanvas(1, 1), rects: map[string]erdRect{}}
	}
	if zoom < 0 {
		zoom = 0
	} else if zoom > 2 {
		zoom = 2
	}

	ordered := orderTablesForERD(tables, graph)
	selectedName := ""
	if cursor >= 0 && cursor < len(tables) {
		selectedName = tables[cursor]
	}

	boxW := 32
	compactW := 26
	colGap := 12
	rowGap := 3
	colsPerRow := 2
	if len(ordered) >= 7 {
		colsPerRow = 3
	}

	switch zoom {
	case 0: // Zoom out (overview minimap)
		boxW = 22
		compactW = 22
		colGap = 6
		rowGap = 2
		colsPerRow = 3
		if len(ordered) >= 10 {
			colsPerRow = 4
		}
	case 2: // Zoom in (detailed)
		boxW = 46
		compactW = 32
		colGap = 14
		rowGap = 4
		colsPerRow = 2
	}
	if len(ordered) == 1 {
		colsPerRow = 1
	}

	effectiveShowFields := showFields && zoom > 0

	type cardData struct {
		name  string
		title string
		lines []string
		w, h  int
	}

	cards := make([]cardData, len(ordered))
	for i, tName := range ordered {
		var cols []tableColumnInfo
		if graph != nil {
			if ov := graph.byName[tName]; ov != nil {
				cols = ov.columns
			}
		}
		title := tName
		if focused && tName == selectedName {
			title = "▸ " + tName
		}

		var lines []string
		if effectiveShowFields {
			if len(cols) == 0 {
				lines = []string{"(no columns)"}
			} else {
				innerW := boxW - 4
				if zoom == 2 {
					nameW := max(10, innerW-22)
					for _, col := range cols {
						keyTag := "  "
						if strings.Contains(col.key, "PK") {
							keyTag = "PK"
						} else if strings.Contains(col.key, "FK") {
							keyTag = "FK"
						} else if col.key == "UNI" {
							keyTag = "UQ"
						}
						cName := col.name
						if lipgloss.Width(cName) > nameW {
							cName = ansi.Truncate(cName, nameW-1, "…")
						}
						dType := col.dataType
						if lipgloss.Width(dType) > 12 {
							dType = ansi.Truncate(dType, 11, "…")
						}
						nullTag := "NN"
						if col.nullable {
							nullTag = "NULL"
						}
						lines = append(lines, fmt.Sprintf("%s %-*s %-12s %s", keyTag, nameW, cName, dType, nullTag))
					}
				} else {
					nameW := max(8, innerW-12)
					for _, col := range cols {
						keyTag := "  "
						if strings.Contains(col.key, "PK") {
							keyTag = "PK"
						} else if strings.Contains(col.key, "FK") {
							keyTag = "FK"
						} else if col.key == "UNI" {
							keyTag = "UQ"
						}
						cName := col.name
						if lipgloss.Width(cName) > nameW {
							cName = ansi.Truncate(cName, nameW-1, "…")
						}
						dType := col.dataType
						if lipgloss.Width(dType) > 8 {
							dType = ansi.Truncate(dType, 7, "…")
						}
						lines = append(lines, fmt.Sprintf("%s %-*s %s", keyTag, nameW, cName, dType))
					}
				}
			}
			// Header (3 lines: top, title, sep) + len(lines) + bottom border (1)
			cards[i] = cardData{name: tName, title: title, lines: lines, w: boxW, h: 4 + len(lines)}
		} else {
			sub := fmt.Sprintf("(%d fields)", len(cols))
			lines = []string{sub}
			cards[i] = cardData{name: tName, title: title, lines: lines, w: compactW, h: 5}
		}
	}

	numRows := (len(cards) + colsPerRow - 1) / colsPerRow
	rowHeights := make([]int, numRows)
	colWidths := make([]int, colsPerRow)
	for i, cd := range cards {
		r := i / colsPerRow
		c := i % colsPerRow
		if cd.h > rowHeights[r] {
			rowHeights[r] = cd.h
		}
		if cd.w > colWidths[c] {
			colWidths[c] = cd.w
		}
	}

	const marginX = 2
	const marginY = 1
	colX := make([]int, colsPerRow)
	curX := marginX
	for c := 0; c < colsPerRow; c++ {
		colX[c] = curX
		curX += colWidths[c] + colGap
	}
	rowY := make([]int, numRows)
	curY := marginY
	for r := 0; r < numRows; r++ {
		rowY[r] = curY
		curY += rowHeights[r] + rowGap
	}

	canvasW := curX + 4
	canvasH := curY + 2
	canvas := newERDCanvas(canvasW, canvasH)
	rects := make(map[string]erdRect, len(cards))
	var cursorRect erdRect

	relatedTables := make(map[string]bool)
	if focused && selectedName != "" && graph != nil {
		for _, rel := range graph.relations {
			if rel.fromTable == selectedName && rel.toTable != selectedName {
				relatedTables[rel.toTable] = true
			} else if rel.toTable == selectedName && rel.fromTable != selectedName {
				relatedTables[rel.fromTable] = true
			}
		}
	}

	for i, cd := range cards {
		r := i / colsPerRow
		c := i % colsPerRow
		rect := erdRect{x: colX[c], y: rowY[r], w: cd.w, h: cd.h}
		rects[cd.name] = rect
		if cd.name == selectedName {
			cursorRect = rect
		}

		borderSt := erdStyleMuted
		titleSt := erdStyleAccent
		bodySt := erdStyleNormal
		if focused && cd.name == selectedName {
			borderSt = erdStylePrimary
			titleSt = erdStylePrimary
		} else if focused && relatedTables[cd.name] {
			borderSt = erdStyleHighlight
			titleSt = erdStyleHighlight
		}
		if !effectiveShowFields {
			bodySt = erdStyleMuted
		}
		canvas.drawBox(rect, cd.title, cd.lines, borderSt, titleSt, bodySt)
	}

	// Route orthogonal ERD connectors with cardinality markers.
	if graph != nil && len(graph.relations) > 0 {
		colIndexOf := func(tName, colName string) int {
			if !effectiveShowFields {
				return -1
			}
			ov := graph.byName[tName]
			if ov == nil {
				return -1
			}
			for idx, col := range ov.columns {
				if col.name == colName {
					return idx
				}
			}
			return -1
		}

		portY := func(tName, colName string, rect erdRect) int {
			idx := colIndexOf(tName, colName)
			if idx >= 0 && rect.y+3+idx < rect.y+rect.h-1 {
				return rect.y + 3 + idx
			}
			return rect.y + rect.h/2
		}

		seen := make(map[string]bool)
		for edgeIdx, rel := range graph.relations {
			key := fmt.Sprintf("%s.%s->%s.%s", rel.fromTable, rel.fromCol, rel.toTable, rel.toCol)
			if seen[key] {
				continue
			}
			seen[key] = true

			fromRect, ok1 := rects[rel.fromTable]
			toRect, ok2 := rects[rel.toTable]
			if !ok1 || !ok2 {
				continue
			}

			st := erdStyleMuted
			if focused && (rel.fromTable == selectedName || rel.toTable == selectedName) {
				st = erdStyleHighlight
			}

			fy := portY(rel.fromTable, rel.fromCol, fromRect)
			ty := portY(rel.toTable, rel.toCol, toRect)

			lane := 2 + (edgeIdx%4)*2

			// Choose horizontal exit/entry sides based on relative box positions.
			if fromRect.x+fromRect.w <= toRect.x {
				// fromTable is to the left of toTable
				fx := fromRect.x + fromRect.w
				tx := toRect.x - 1
				midX := min(tx-2, fx+lane)
				if midX <= fx {
					midX = (fx + tx) / 2
				}
				canvas.drawHLine(fx, midX, fy, st)
				canvas.drawVLine(midX, fy, ty, st)
				canvas.drawHLine(midX, tx, ty, st)

				// Cardinality: from (FK) side on left, to (PK) side on right
				if rel.isOneToOne {
					canvas.set(fx, fy, '|', st)
					canvas.set(fx+1, fy, '|', st)
				} else {
					canvas.set(fx, fy, '<', st)
				}
				canvas.set(tx, ty, '|', st)
				if tx-1 > midX {
					canvas.set(tx-1, ty, '|', st)
				}
			} else if toRect.x+toRect.w <= fromRect.x {
				// toTable is to the left of fromTable
				fx := fromRect.x - 1
				tx := toRect.x + toRect.w
				midX := max(tx+2, fx-lane)
				if midX >= fx {
					midX = (fx + tx) / 2
				}
				canvas.drawHLine(midX, fx, fy, st)
				canvas.drawVLine(midX, fy, ty, st)
				canvas.drawHLine(tx, midX, ty, st)

				if rel.isOneToOne {
					canvas.set(fx, fy, '|', st)
					canvas.set(fx-1, fy, '|', st)
				} else {
					canvas.set(fx, fy, '>', st)
				}
				canvas.set(tx, ty, '|', st)
				if tx+1 < midX {
					canvas.set(tx+1, ty, '|', st)
				}
			} else {
				// Same column (stacked vertically): route through right gutter channel.
				fx := fromRect.x + fromRect.w
				tx := toRect.x + toRect.w
				chanX := max(fx, tx) + lane
				if chanX >= canvas.w-1 {
					chanX = canvas.w - 2
				}
				canvas.drawHLine(fx, chanX, fy, st)
				canvas.drawVLine(chanX, fy, ty, st)
				canvas.drawHLine(tx, chanX, ty, st)

				if rel.isOneToOne {
					canvas.set(fx, fy, '|', st)
					canvas.set(fx+1, fy, '|', st)
				} else {
					canvas.set(fx, fy, '<', st)
				}
				canvas.set(tx, ty, '|', st)
				if tx+1 < chanX {
					canvas.set(tx+1, ty, '|', st)
				}
			}
		}
	}

	return &erdLayoutResult{
		canvas:     canvas,
		rects:      rects,
		cursorRect: cursorRect,
	}
}

func erdHitTest(layout *erdLayoutResult, cx, cy int) string {
	if layout == nil {
		return ""
	}
	for name, r := range layout.rects {
		if cx >= r.x && cx < r.x+r.w && cy >= r.y && cy < r.y+r.h {
			return name
		}
	}
	return ""
}

func formatTableRelationsSummary(tableName string, graph *tableSchemaGraph, maxWidth int) string {
	if tableName == "" || graph == nil || len(graph.relations) == 0 {
		return ""
	}
	var parts []string
	seen := make(map[string]bool)
	for _, rel := range graph.relations {
		var label string
		if rel.fromTable == tableName {
			label = fmt.Sprintf("→ %s (%s→%s)", rel.toTable, rel.fromCol, rel.toCol)
		} else if rel.toTable == tableName {
			label = fmt.Sprintf("← %s (%s→%s)", rel.fromTable, rel.fromCol, rel.toCol)
		}
		if label != "" && !seen[label] {
			seen[label] = true
			parts = append(parts, label)
		}
	}
	if len(parts) == 0 {
		return styleMutedText.Render(fmt.Sprintf("FKs (%s): none", tableName))
	}
	line := fmt.Sprintf("FKs (%s): %s", tableName, strings.Join(parts, "  •  "))
	if maxWidth > 0 && lipgloss.Width(line) > maxWidth {
		line = ansi.Truncate(line, max(10, maxWidth-1), "…")
	}
	return lipgloss.NewStyle().Foreground(colorAccent).Render(line)
}

func formatTableOverviewDiagram(tables []string, graph *tableSchemaGraph, cursor int, focused bool, showFields bool, maxWidth int) (string, int) {
	res := buildTableOverviewERD(tables, graph, cursor, focused, showFields)
	if res == nil || res.canvas == nil {
		return "", 0
	}
	return res.canvas.renderWindow(0, 0, max(maxWidth, res.canvas.w), res.canvas.h), res.cursorRect.y
}

func joinWithGap(items []string, gap string) []string {
	if len(items) <= 1 {
		return items
	}
	res := make([]string, 0, len(items)*2-1)
	for i, it := range items {
		if i > 0 {
			res = append(res, gap)
		}
		res = append(res, it)
	}
	return res
}

func formatTableDetailDiagram(detail *tableDetail, graph *tableSchemaGraph, showFields bool, maxWidth int) string {
	if detail == nil {
		return ""
	}
	boxW := min(max(32, maxWidth), 76)
	innerW := max(20, boxW-4)

	var cols []tableColumnInfo
	if graph != nil {
		if ov := graph.byName[detail.tableName]; ov != nil {
			cols = ov.columns
		}
	}
	// Fallback to schemaRows if graph wasn't populated for this table.
	if len(cols) == 0 && len(detail.schemaRows) > 0 {
		for _, r := range detail.schemaRows {
			if len(r) >= 2 {
				cols = append(cols, tableColumnInfo{
					name:     r[0],
					dataType: r[1],
					nullable: len(r) > 2 && strings.EqualFold(r[2], "YES"),
				})
			}
		}
	}

	var body strings.Builder
	header := fmt.Sprintf("%s (%d fields)", detail.tableName, len(cols))
	body.WriteString(lipgloss.NewStyle().Bold(true).Foreground(colorAccent).Render(header))

	if showFields {
		if len(cols) == 0 {
			body.WriteString("\n" + styleMutedText.Render("(no fields)"))
		} else {
			for _, col := range cols {
				tag := "   "
				if col.key != "" {
					tag = fmt.Sprintf("%-3s", col.key)
				}
				nullStr := "NOT NULL"
				if col.nullable {
					nullStr = "NULL"
				}
				line := fmt.Sprintf("%s %-18s %-14s %s", tag, col.name, col.dataType, nullStr)
				if lipgloss.Width(line) > innerW {
					line = ansi.Truncate(line, innerW-1, "…")
				}
				body.WriteString("\n" + line)
			}
		}
	} else {
		body.WriteString("\n" + styleMutedText.Render("(fields hidden — press 'f' to show)"))
	}

	box := lipgloss.NewStyle().
		Border(lipgloss.RoundedBorder()).
		BorderForeground(colorPrimary).
		Width(boxW-2).
		Padding(0, 1).
		Render(body.String())

	var out strings.Builder
	out.WriteString(box + "\n")

	if graph != nil {
		var relLines []string
		seen := make(map[string]bool)
		for _, rel := range graph.relations {
			if rel.fromTable != detail.tableName && rel.toTable != detail.tableName {
				continue
			}
			var line string
			if showFields {
				if rel.fromTable == detail.tableName {
					line = fmt.Sprintf("  %s.%s ──▶ %s.%s", rel.fromTable, rel.fromCol, rel.toTable, rel.toCol)
				} else {
					line = fmt.Sprintf("  %s.%s ◀── %s.%s", detail.tableName, rel.toCol, rel.fromTable, rel.fromCol)
				}
			} else {
				if rel.fromTable == detail.tableName {
					line = fmt.Sprintf("  %s ──▶ %s", rel.fromTable, rel.toTable)
				} else {
					line = fmt.Sprintf("  %s ◀── %s", detail.tableName, rel.fromTable)
				}
			}
			if !seen[line] {
				seen[line] = true
				relLines = append(relLines, line)
			}
		}
		if len(relLines) > 0 {
			out.WriteString("\n" + lipgloss.NewStyle().Bold(true).Foreground(colorAccent).Render("Relationships") + "\n")
			for _, l := range relLines {
				out.WriteString(styleMutedText.Render(l) + "\n")
			}
		}
	}

	return out.String()
}

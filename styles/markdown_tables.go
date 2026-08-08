package styles

import (
	"bytes"
	"sort"
	"strings"

	xansi "github.com/charmbracelet/x/ansi"
	"github.com/yuin/goldmark"
	goldmarkast "github.com/yuin/goldmark/ast"
	"github.com/yuin/goldmark/extension"
	tableast "github.com/yuin/goldmark/extension/ast"
	"github.com/yuin/goldmark/text"
)

const (
	narrativeMinimumWords  = 4
	narrativeMinimumWidth  = 28
	minimumGridColumnWidth = 3
	narrativeSoftFloor     = 16
)

type markdownTableCell struct {
	raw   string
	plain string
}

type markdownTable struct {
	start   int
	end     int
	prefix  string
	headers []markdownTableCell
	rows    [][]markdownTableCell
}

type markdownColumnMetrics struct {
	maxWidth  int
	words     int
	width     int
	nonEmpty  int
	narrative bool
}

func parseResponsiveTables(markdown string, width int) []markdownTable {
	tables := extractMarkdownTables(markdown)
	selected := make([]markdownTable, 0, len(tables))
	for _, table := range tables {
		if tableNeedsResponsiveLayout(table, width) {
			selected = append(selected, table)
		}
	}
	return selected
}

func extractMarkdownTables(markdown string) []markdownTable {
	source := []byte(markdown)
	parser := goldmark.New(
		goldmark.WithExtensions(extension.GFM, extension.DefinitionList),
	).Parser()
	document := parser.Parse(text.NewReader(source))

	var tables []markdownTable
	_ = goldmarkast.Walk(document, func(node goldmarkast.Node, entering bool) (goldmarkast.WalkStatus, error) {
		tableNode, ok := node.(*tableast.Table)
		if !entering || !ok {
			return goldmarkast.WalkContinue, nil
		}
		if table, ok := extractMarkdownTable(tableNode, source); ok {
			tables = append(tables, table)
		}
		return goldmarkast.WalkSkipChildren, nil
	})
	return tables
}

func extractMarkdownTable(node *tableast.Table, source []byte) (markdownTable, bool) {
	columns := len(node.Alignments)
	if columns == 0 {
		return markdownTable{}, false
	}

	var header *tableast.TableHeader
	var bodyRows []*tableast.TableRow
	for child := node.FirstChild(); child != nil; child = child.NextSibling() {
		switch typed := child.(type) {
		case *tableast.TableHeader:
			header = typed
		case *tableast.TableRow:
			bodyRows = append(bodyRows, typed)
		}
	}
	if header == nil {
		return markdownTable{}, false
	}

	lineStart := sourceLineStart(source, header.Pos())
	endOffset := sourceLineEnd(source, header.Pos())
	if len(bodyRows) > 0 {
		endOffset = sourceLineEnd(source, bodyRows[len(bodyRows)-1].Pos())
	} else if nextLine := endOffset; nextLine < len(source) {
		endOffset = sourceLineEnd(source, nextLine)
	}

	table := markdownTable{
		start:   lineStart,
		end:     endOffset,
		prefix:  string(source[lineStart:header.Pos()]),
		headers: tableCells(header, columns, source),
		rows:    make([][]markdownTableCell, 0, len(bodyRows)),
	}
	for _, row := range bodyRows {
		table.rows = append(table.rows, tableCells(row, columns, source))
	}
	return table, true
}

func tableCells(row goldmarkast.Node, columns int, source []byte) []markdownTableCell {
	cells := make([]markdownTableCell, 0, columns)
	for child := row.FirstChild(); child != nil && len(cells) < columns; child = child.NextSibling() {
		cell, ok := child.(*tableast.TableCell)
		if !ok {
			continue
		}
		var raw string
		if cell.Lines().Len() > 0 {
			segment := cell.Lines().At(0)
			raw = string(segment.Value(source))
		}
		cells = append(cells, markdownTableCell{
			raw:   strings.TrimSpace(raw),
			plain: strings.TrimSpace(tableCellPlainText(cell, source)),
		})
	}
	for len(cells) < columns {
		cells = append(cells, markdownTableCell{})
	}
	return cells
}

func tableCellPlainText(cell *tableast.TableCell, source []byte) string {
	var plain strings.Builder
	_ = goldmarkast.Walk(cell, func(node goldmarkast.Node, entering bool) (goldmarkast.WalkStatus, error) {
		if !entering {
			return goldmarkast.WalkContinue, nil
		}
		switch typed := node.(type) {
		case *goldmarkast.Text:
			plain.Write(typed.Value(source))
			if typed.SoftLineBreak() || typed.HardLineBreak() {
				plain.WriteByte('\n')
			}
		case *goldmarkast.String:
			plain.Write(typed.Value)
		}
		return goldmarkast.WalkContinue, nil
	})
	return plain.String()
}

func tableNeedsResponsiveLayout(table markdownTable, width int) bool {
	metrics := tableColumnMetrics(table)
	if len(metrics) == 0 {
		return false
	}

	const cellPadding = 2
	gridOverhead := len(metrics)*(cellPadding+1) + 1
	intrinsicWidth := gridOverhead
	for _, metric := range metrics {
		intrinsicWidth += metric.maxWidth
	}
	if intrinsicWidth <= width {
		return false
	}
	if width-gridOverhead < len(metrics)*minimumGridColumnWidth {
		return true
	}

	allocations := allocateGridColumns(metrics, width-gridOverhead)
	for column, metric := range metrics {
		if metric.narrative && metric.maxWidth > allocations[column] {
			return true
		}
	}
	return false
}

func tableColumnMetrics(table markdownTable) []markdownColumnMetrics {
	metrics := make([]markdownColumnMetrics, len(table.headers))
	for column, header := range table.headers {
		metrics[column].maxWidth = xansi.StringWidth(header.plain)
	}
	for _, row := range table.rows {
		for column, cell := range row {
			cellWidth := xansi.StringWidth(cell.plain)
			metrics[column].maxWidth = max(metrics[column].maxWidth, cellWidth)
			if cell.plain == "" {
				continue
			}
			metrics[column].nonEmpty++
			metrics[column].words += len(strings.Fields(cell.plain))
			metrics[column].width += cellWidth
		}
	}
	for column := range metrics {
		metric := &metrics[column]
		if metric.nonEmpty == 0 {
			continue
		}
		metric.narrative = metric.words >= narrativeMinimumWords*metric.nonEmpty ||
			metric.width >= narrativeMinimumWidth*metric.nonEmpty
	}
	return metrics
}

func allocateGridColumns(metrics []markdownColumnMetrics, available int) []int {
	allocations := make([]int, len(metrics))
	narrativeColumns := 0
	compactColumns := 0
	for _, metric := range metrics {
		if metric.narrative {
			narrativeColumns++
		} else {
			compactColumns++
		}
	}
	if narrativeColumns == 0 {
		for column, metric := range metrics {
			allocations[column] = metric.maxWidth
		}
		return allocations
	}

	minimumCompactBudget := compactColumns * minimumGridColumnWidth
	narrativeBudget := min(narrativeColumns*narrativeSoftFloor, max(0, available-minimumCompactBudget))
	compactBudget := max(0, available-narrativeBudget)
	remainingCompact := compactColumns
	for column, metric := range metrics {
		if metric.narrative {
			continue
		}
		maximumHere := max(0, compactBudget-(remainingCompact-1)*minimumGridColumnWidth)
		allocation := min(max(metric.maxWidth, minimumGridColumnWidth), maximumHere)
		allocations[column] = allocation
		compactBudget -= allocation
		remainingCompact--
	}

	narrativeBudget = max(0, available)
	for column, metric := range metrics {
		if !metric.narrative {
			narrativeBudget -= allocations[column]
		}
	}
	perColumn := narrativeBudget / narrativeColumns
	remainder := narrativeBudget % narrativeColumns
	for column, metric := range metrics {
		if !metric.narrative {
			continue
		}
		allocations[column] = perColumn
		if remainder > 0 {
			allocations[column]++
			remainder--
		}
	}
	return allocations
}

// markTableBodyBoundaries inserts a one-cell-per-column marker row between adjacent
// body rows reported by Goldmark's GFM table parser. Using the same parser as Glamour
// keeps pipe-less rows, escaped pipes, code fences, and nested tables in lockstep.
func markTableBodyBoundaries(markdown string) (string, string) {
	marker := unusedTableMarker(markdown)
	if marker == "" {
		return markdown, ""
	}

	source := []byte(markdown)
	parser := goldmark.New(goldmark.WithExtensions(extension.GFM, extension.DefinitionList)).Parser()
	document := parser.Parse(text.NewReader(source))
	insertions := tableBoundaryInsertions(document, source, marker)
	if len(insertions) == 0 {
		return markdown, ""
	}

	var marked strings.Builder
	start := 0
	for _, insertion := range insertions {
		marked.Write(source[start:insertion.offset])
		marked.WriteString(insertion.line)
		start = insertion.offset
	}
	marked.Write(source[start:])
	return marked.String(), marker
}

type tableBoundaryInsertion struct {
	offset int
	line   string
}

func tableBoundaryInsertions(document goldmarkast.Node, source []byte, marker string) []tableBoundaryInsertion {
	var insertions []tableBoundaryInsertion
	_ = goldmarkast.Walk(document, func(node goldmarkast.Node, entering bool) (goldmarkast.WalkStatus, error) {
		table, ok := node.(*tableast.Table)
		if !entering || !ok {
			return goldmarkast.WalkContinue, nil
		}
		bodyRow := 0
		for child := table.FirstChild(); child != nil; child = child.NextSibling() {
			row, body := child.(*tableast.TableRow)
			if !body {
				continue
			}
			if bodyRow > 0 {
				lineStart := sourceLineStart(source, row.Pos())
				prefix := string(source[lineStart:row.Pos()]) + sourceLeadingWhitespace(source[row.Pos():])
				insertions = append(insertions, tableBoundaryInsertion{
					offset: lineStart,
					line:   prefix + markerTableRow(len(table.Alignments), marker) + sourceLineEndingAt(source, row.Pos()),
				})
			}
			bodyRow++
		}
		return goldmarkast.WalkSkipChildren, nil
	})
	sort.Slice(insertions, func(i, j int) bool { return insertions[i].offset < insertions[j].offset })
	return insertions
}

func sourceLineStart(source []byte, offset int) int {
	if offset > len(source) {
		offset = len(source)
	}
	if index := bytes.LastIndexByte(source[:offset], '\n'); index >= 0 {
		return index + 1
	}
	return 0
}

func sourceLineEnd(source []byte, offset int) int {
	if offset < 0 {
		offset = 0
	}
	if offset > len(source) {
		offset = len(source)
	}
	if newline := bytes.IndexByte(source[offset:], '\n'); newline >= 0 {
		return offset + newline + 1
	}
	return len(source)
}

func sourceLeadingWhitespace(source []byte) string {
	end := 0
	for end < len(source) && (source[end] == ' ' || source[end] == '\t') {
		end++
	}
	return string(source[:end])
}

func sourceLineEndingAt(source []byte, offset int) string {
	if offset < 0 {
		offset = 0
	}
	if offset > len(source) {
		offset = len(source)
	}
	if newline := bytes.IndexByte(source[offset:], '\n'); newline > 0 && source[offset+newline-1] == '\r' {
		return "\r\n"
	}
	return "\n"
}

func unusedTableMarker(markdown string) string {
	for marker := rune(0xE000); marker <= 0xF8FF; marker++ {
		if !strings.ContainsRune(markdown, marker) {
			return string(marker)
		}
	}
	for marker := rune(0xF0000); marker <= 0xFFFFD; marker++ {
		if !strings.ContainsRune(markdown, marker) {
			return string(marker)
		}
	}
	return ""
}

func markerTableRow(columns int, marker string) string {
	cells := make([]string, columns)
	for i := range cells {
		cells[i] = marker
	}
	return "| " + strings.Join(cells, " | ") + " |"
}

func replaceMarkedTableRows(rendered, marker string) string {
	lines := strings.Split(rendered, "\n")
	for i, line := range lines {
		if strings.Contains(line, marker) {
			lines[i] = tableRowSeparator(line, marker)
		}
	}
	return strings.Join(lines, "\n")
}

func tableRowSeparator(line, marker string) string {
	plain := xansi.Strip(line)
	markerOffset := strings.Index(plain, marker)
	if markerOffset < 0 {
		return line
	}
	tableStart := max(0, xansi.StringWidth(plain[:markerOffset])-1)
	prefix := xansi.Cut(line, 0, tableStart)
	table := xansi.Cut(plain, tableStart, xansi.StringWidth(plain))

	var separator strings.Builder
	separator.WriteString(prefix)
	for _, char := range table {
		width := xansi.StringWidth(string(char))
		if width < 1 {
			continue
		}
		glyph := "─"
		if char == '│' || char == '|' {
			glyph = "┼"
		}
		separator.WriteString(strings.Repeat(glyph, width))
	}
	return separator.String()
}

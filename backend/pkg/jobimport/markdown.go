package jobimport

import (
	"strconv"
	"strings"

	"golang.org/x/net/html"
	"golang.org/x/net/html/atom"
)

const maxOverviewRunes = 60000

var skippedTags = map[atom.Atom]bool{
	atom.Script: true, atom.Style: true, atom.Noscript: true, atom.Iframe: true, atom.Svg: true,
	atom.Img: true, atom.Form: true, atom.Button: true, atom.Template: true, atom.Head: true,
}

var headingLevels = map[atom.Atom]int{
	atom.H1: 1, atom.H2: 2, atom.H3: 3, atom.H4: 4, atom.H5: 5, atom.H6: 6,
}

var blockTags = map[atom.Atom]bool{
	atom.P: true, atom.Div: true, atom.Section: true, atom.Article: true, atom.Main: true,
	atom.Header: true, atom.Footer: true, atom.Table: true, atom.Tbody: true, atom.Thead: true,
	atom.Tr: true, atom.Dl: true, atom.Dt: true, atom.Dd: true, atom.Pre: true, atom.Hr: true,
	atom.Ul: true, atom.Ol: true, atom.Li: true, atom.Blockquote: true,
	atom.H1: true, atom.H2: true, atom.H3: true, atom.H4: true, atom.H5: true, atom.H6: true,
}

// htmlToMarkdown converts untrusted posting HTML into the subset the
// frontend's renderMarkdown supports. Nested lists are flattened because the
// renderer has no nesting.
func htmlToMarkdown(fragment string) string {
	nodes, err := html.ParseFragment(strings.NewReader(fragment), &html.Node{Type: html.ElementNode, DataAtom: atom.Div, Data: "div"})
	if err != nil {
		return ""
	}
	w := &markdownWriter{}
	for _, node := range nodes {
		w.node(node)
	}
	w.flush()
	return truncateRunes(strings.Join(w.blocks, "\n\n"), maxOverviewRunes)
}

type markdownWriter struct {
	blocks []string
	line   strings.Builder
}

func (w *markdownWriter) flush() {
	if text := collapseSpace(w.line.String()); text != "" {
		w.blocks = append(w.blocks, text)
	}
	w.line.Reset()
}

func (w *markdownWriter) node(n *html.Node) {
	switch {
	case n.Type == html.TextNode:
		w.line.WriteString(n.Data)
	case n.Type != html.ElementNode:
		w.children(n)
	case skippedTags[n.DataAtom]:
	case blockTags[n.DataAtom]:
		w.flush()
		w.block(n)
		w.flush()
	default:
		w.line.WriteString(inlineMarkdown(n, true))
	}
}

func (w *markdownWriter) children(n *html.Node) {
	for child := n.FirstChild; child != nil; child = child.NextSibling {
		w.node(child)
	}
}

func (w *markdownWriter) block(n *html.Node) {
	switch n.DataAtom {
	case atom.Ul, atom.Ol:
		w.list(n)
	case atom.Blockquote:
		w.quote(n)
	case atom.Hr:
	default:
		if level := headingLevels[n.DataAtom]; level > 0 {
			w.heading(n, level)
			return
		}
		w.children(n)
	}
}

func (w *markdownWriter) heading(n *html.Node, level int) {
	if text := collapseSpace(childrenMarkdown(n, false)); text != "" {
		w.blocks = append(w.blocks, strings.Repeat("#", level)+" "+text)
	}
}

func (w *markdownWriter) quote(n *html.Node) {
	inner := &markdownWriter{}
	inner.children(n)
	inner.flush()
	for _, block := range inner.blocks {
		w.blocks = append(w.blocks, "> "+strings.ReplaceAll(block, "\n", " "))
	}
}

// list keeps a list's items adjacent so they render as one list.
func (w *markdownWriter) list(n *html.Node) {
	var items []string
	index := 0
	for item := n.FirstChild; item != nil; item = item.NextSibling {
		if item.DataAtom != atom.Li {
			continue
		}
		index++
		items = append(items, listItemLines(item, listMarker(n, index))...)
	}
	if len(items) > 0 {
		w.blocks = append(w.blocks, strings.Join(items, "\n"))
	}
}

func listMarker(list *html.Node, index int) string {
	if list.DataAtom == atom.Ol {
		return strconv.Itoa(index) + ". "
	}
	return "- "
}

func listItemLines(item *html.Node, marker string) []string {
	var text strings.Builder
	var nested []string
	for child := item.FirstChild; child != nil; child = child.NextSibling {
		if child.DataAtom == atom.Ul || child.DataAtom == atom.Ol {
			inner := &markdownWriter{}
			inner.list(child)
			nested = append(nested, inner.blocks...)
			continue
		}
		text.WriteString(inlineMarkdown(child, true) + " ")
	}
	lines := nested
	if value := collapseSpace(text.String()); value != "" {
		lines = append([]string{marker + value}, nested...)
	}
	return lines
}

func childrenMarkdown(n *html.Node, emphasis bool) string {
	var text strings.Builder
	for child := n.FirstChild; child != nil; child = child.NextSibling {
		text.WriteString(inlineMarkdown(child, emphasis))
	}
	return text.String()
}

// inlineMarkdown renders emphasis and safe links; block tags inside an inline
// context collapse to their text.
func inlineMarkdown(n *html.Node, emphasis bool) string {
	if n.Type == html.TextNode {
		return n.Data
	}
	if n.Type != html.ElementNode || skippedTags[n.DataAtom] {
		return ""
	}
	inner := childrenMarkdown(n, emphasis)
	switch n.DataAtom {
	case atom.Br:
		return " "
	case atom.Strong, atom.B:
		return wrapInline(inner, "**", emphasis)
	case atom.Em, atom.I:
		return wrapInline(inner, "*", emphasis)
	case atom.A:
		return linkMarkdown(n, inner)
	}
	return inner
}

// wrapInline moves surrounding spaces outside the markers, which would
// otherwise stop the emphasis from rendering.
func wrapInline(text, marker string, emphasis bool) string {
	trimmed := strings.TrimSpace(text)
	if !emphasis || trimmed == "" {
		return text
	}
	start := text[:strings.Index(text, trimmed)]
	end := text[len(start)+len(trimmed):]
	return start + marker + strings.ReplaceAll(trimmed, marker, "") + marker + end
}

func linkMarkdown(n *html.Node, text string) string {
	href := attribute(n, "href")
	label := collapseSpace(text)
	lower := strings.ToLower(href)
	safe := strings.HasPrefix(lower, "https://") || strings.HasPrefix(lower, "http://")
	if !safe || label == "" || strings.ContainsAny(href, " ()") || strings.ContainsAny(label, "[]") {
		return text
	}
	return "[" + label + "](" + href + ")"
}

func attribute(n *html.Node, key string) string {
	for _, attr := range n.Attr {
		if attr.Key == key {
			return attr.Val
		}
	}
	return ""
}

func collapseSpace(value string) string {
	return strings.Join(strings.Fields(value), " ")
}

func truncateRunes(value string, limit int) string {
	runes := []rune(value)
	if len(runes) <= limit {
		return value
	}
	return string(runes[:limit])
}

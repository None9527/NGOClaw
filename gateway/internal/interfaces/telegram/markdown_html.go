// Copyright 2026 NGOClaw Authors
// SPDX-License-Identifier: Apache-2.0

package telegram

import (
	"bytes"
	"fmt"
	"html"
	"regexp"
	"strings"

	"github.com/yuin/goldmark"
	"github.com/yuin/goldmark/ast"
	"github.com/yuin/goldmark/text"
)

// MarkdownToTelegramHTML converts Markdown text to Telegram-safe HTML.
// Telegram HTML supports: <b>, <i>, <s>, <code>, <pre>, <a href="">.
// This guarantees well-formed tags, unlike raw Markdown parse_mode.
func MarkdownToTelegramHTML(markdown string) string {
	if markdown == "" {
		return ""
	}

	src := []byte(markdown)
	md := goldmark.New()
	reader := text.NewReader(src)
	doc := md.Parser().Parse(reader)

	var buf bytes.Buffer
	r := &tgHTMLRenderer{src: src}
	r.render(&buf, doc)

	result := buf.String()
	// Trim trailing newlines for cleaner TG output
	return strings.TrimRight(result, "\n")
}

// tgHTMLRenderer walks the goldmark AST and emits Telegram-compatible HTML.
type tgHTMLRenderer struct {
	src []byte
}

func (r *tgHTMLRenderer) render(w *bytes.Buffer, node ast.Node) {
	for child := node.FirstChild(); child != nil; child = child.NextSibling() {
		r.renderNode(w, child)
	}
}

func (r *tgHTMLRenderer) renderNode(w *bytes.Buffer, node ast.Node) {
	switch n := node.(type) {
	case *ast.Paragraph:
		r.renderChildren(w, n)
		w.WriteString("\n\n")

	case *ast.Heading:
		// TG has no heading tags — render as bold
		w.WriteString("<b>")
		r.renderChildren(w, n)
		w.WriteString("</b>\n\n")

	case *ast.ThematicBreak:
		w.WriteString("———\n\n")

	case *ast.Blockquote:
		// TG has no blockquote — prefix lines
		var inner bytes.Buffer
		r.renderChildren(&inner, n)
		for _, line := range strings.Split(strings.TrimRight(inner.String(), "\n"), "\n") {
			w.WriteString("▎")
			w.WriteString(line)
			w.WriteString("\n")
		}
		w.WriteString("\n")

	case *ast.FencedCodeBlock:
		lang := string(n.Language(r.src))
		if lang != "" {
			w.WriteString("<pre><code class=\"language-")
			w.WriteString(html.EscapeString(lang))
			w.WriteString("\">")
		} else {
			w.WriteString("<pre><code>")
		}
		lines := n.Lines()
		for i := 0; i < lines.Len(); i++ {
			line := lines.At(i)
			w.WriteString(html.EscapeString(string(line.Value(r.src))))
		}
		w.WriteString("</code></pre>\n\n")

	case *ast.CodeBlock:
		w.WriteString("<pre><code>")
		lines := n.Lines()
		for i := 0; i < lines.Len(); i++ {
			line := lines.At(i)
			w.WriteString(html.EscapeString(string(line.Value(r.src))))
		}
		w.WriteString("</code></pre>\n\n")

	case *ast.List:
		r.renderList(w, n)

	case *ast.ListItem:
		r.renderChildren(w, n)

	// Inline nodes
	case *ast.Text:
		w.WriteString(html.EscapeString(string(n.Segment.Value(r.src))))
		if n.SoftLineBreak() {
			w.WriteString("\n")
		}
		if n.HardLineBreak() {
			w.WriteString("\n")
		}

	case *ast.String:
		w.WriteString(html.EscapeString(string(n.Value)))

	case *ast.CodeSpan:
		w.WriteString("<code>")
		r.renderCodeSpanText(w, n)
		w.WriteString("</code>")

	case *ast.Emphasis:
		if n.Level == 2 {
			w.WriteString("<b>")
			r.renderChildren(w, n)
			w.WriteString("</b>")
		} else {
			w.WriteString("<i>")
			r.renderChildren(w, n)
			w.WriteString("</i>")
		}

	case *ast.Link:
		w.WriteString("<a href=\"")
		w.WriteString(html.EscapeString(string(n.Destination)))
		w.WriteString("\">")
		r.renderChildren(w, n)
		w.WriteString("</a>")

	case *ast.AutoLink:
		url := string(n.URL(r.src))
		w.WriteString("<a href=\"")
		w.WriteString(html.EscapeString(url))
		w.WriteString("\">")
		w.WriteString(html.EscapeString(url))
		w.WriteString("</a>")

	case *ast.Image:
		// TG doesn't support inline images — show as link
		w.WriteString("[图片: ")
		w.WriteString(html.EscapeString(string(n.Destination)))
		w.WriteString("]")

	case *ast.RawHTML:
		// Pass through raw HTML segments
		segs := n.Segments
		for i := 0; i < segs.Len(); i++ {
			seg := segs.At(i)
			w.Write(seg.Value(r.src))
		}

	case *ast.HTMLBlock:
		lines := n.Lines()
		for i := 0; i < lines.Len(); i++ {
			line := lines.At(i)
			w.Write(line.Value(r.src))
		}
		w.WriteString("\n")

	default:
		// Unknown node — render children
		r.renderChildren(w, node)
	}
}

func (r *tgHTMLRenderer) renderChildren(w *bytes.Buffer, node ast.Node) {
	for child := node.FirstChild(); child != nil; child = child.NextSibling() {
		r.renderNode(w, child)
	}
}

func (r *tgHTMLRenderer) renderCodeSpanText(w *bytes.Buffer, node ast.Node) {
	for child := node.FirstChild(); child != nil; child = child.NextSibling() {
		if t, ok := child.(*ast.Text); ok {
			w.WriteString(html.EscapeString(string(t.Segment.Value(r.src))))
		} else {
			r.renderCodeSpanText(w, child)
		}
	}
}

func (r *tgHTMLRenderer) renderList(w *bytes.Buffer, list *ast.List) {
	idx := list.Start
	for child := list.FirstChild(); child != nil; child = child.NextSibling() {
		if list.IsOrdered() {
			w.WriteString(strings.Repeat(" ", 0))
			w.WriteString(itoa(idx))
			w.WriteString(". ")
			idx++
		} else {
			w.WriteString("• ")
		}
		// Render list item inline (strip trailing paragraph breaks)
		var itemBuf bytes.Buffer
		r.renderChildren(&itemBuf, child)
		item := strings.TrimRight(itemBuf.String(), "\n")
		// Indent continuation lines
		lines := strings.Split(item, "\n")
		for i, line := range lines {
			if i > 0 {
				w.WriteString("\n  ")
			}
			w.WriteString(line)
		}
		w.WriteString("\n")
	}
	w.WriteString("\n")
}

func itoa(n int) string {
	return fmt.Sprintf("%d", n)
}

// StripMarkdownForPlaintext removes all Markdown formatting, leaving plain text.
// Used as fallback when HTML also fails.
var reStripMD = regexp.MustCompile("(?s)```[^`]*```|`[^`]+`|\\*\\*|__|\\*|_|~~|#{1,6} |\\[([^]]+)\\]\\([^)]+\\)|!\\[[^]]*\\]\\([^)]+\\)")

func StripMarkdownForPlaintext(md string) string {
	result := reStripMD.ReplaceAllStringFunc(md, func(match string) string {
		// Keep link text
		if strings.HasPrefix(match, "[") {
			idx := strings.Index(match, "](")
			if idx > 0 {
				return match[1:idx]
			}
		}
		// Keep code content
		if strings.HasPrefix(match, "```") {
			inner := strings.TrimPrefix(match, "```")
			inner = strings.TrimSuffix(inner, "```")
			// Remove language tag on first line
			if idx := strings.Index(inner, "\n"); idx >= 0 {
				inner = inner[idx+1:]
			}
			return inner
		}
		if strings.HasPrefix(match, "`") {
			return strings.Trim(match, "`")
		}
		// Strip formatting markers
		if match == "**" || match == "__" || match == "*" || match == "_" || match == "~~" {
			return ""
		}
		// Strip heading markers
		if strings.HasPrefix(match, "#") {
			return ""
		}
		// Strip images
		if strings.HasPrefix(match, "![") {
			return ""
		}
		return match
	})
	return result
}

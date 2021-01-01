// Copyright 2019 The Hugo Authors. All rights reserved.
//
// Licensed under the Apache License, Version 2.0 (the "License");
// you may not use this file except in compliance with the License.
// You may obtain a copy of the License at
// http://www.apache.org/licenses/LICENSE-2.0
//
// Unless required by applicable law or agreed to in writing, software
// distributed under the License is distributed on an "AS IS" BASIS,
// WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
// See the License for the specific language governing permissions and
// limitations under the License.

// Package helpers implements general utility functions that work with
// and on content.  The helper functions defined here lay down the
// foundation of how Hugo works with files and filepaths, and perform
// string operations on content.
package helpers

import (
	"bytes"
	"fmt" // only for CD's testing
	"html/template"
	"unicode"
	"unicode/utf8"

	"github.com/gohugoio/hugo/common/loggers"

	"github.com/spf13/afero"

	"github.com/gohugoio/hugo/markup/converter"

	"github.com/gohugoio/hugo/markup"

	bp "github.com/gohugoio/hugo/bufferpool"
	"github.com/gohugoio/hugo/config"

	"strings"
)

// SummaryDivider denotes where content summarization should end. The default is "<!--more-->".
var SummaryDivider = []byte("<!--more-->")

var (
	openingPTag        = []byte("<p>")
	closingPTag        = []byte("</p>")
	paragraphIndicator = []byte("<p")
	closingIndicator   = []byte("</")
)

// ContentSpec provides functionality to render markdown content.
type ContentSpec struct {
	Converters          markup.ConverterProvider
	MardownConverter    converter.Converter // Markdown converter with no document context
	anchorNameSanitizer converter.AnchorNameSanitizer

	// SummaryLength is the length of the summary that Hugo extracts from a content.
	summaryLength     int
	SummaryExclusions []string

	BuildFuture  bool
	BuildExpired bool
	BuildDrafts  bool

	Cfg config.Provider
}

// NewContentSpec returns a ContentSpec initialized
// with the appropriate fields from the given config.Provider.
func NewContentSpec(cfg config.Provider, logger *loggers.Logger, contentFs afero.Fs) (*ContentSpec, error) {

	spec := &ContentSpec{
		summaryLength:     cfg.GetInt("summaryLength"),
		SummaryExclusions: cfg.GetStringSlice("summaryExclusions"),
		BuildFuture:       cfg.GetBool("buildFuture"),
		BuildExpired:      cfg.GetBool("buildExpired"),
		BuildDrafts:       cfg.GetBool("buildDrafts"),

		Cfg: cfg,
	}

	converterProvider, err := markup.NewConverterProvider(converter.ProviderConfig{
		Cfg:       cfg,
		ContentFs: contentFs,
		Logger:    logger,
	})

	if err != nil {
		return nil, err
	}

	spec.Converters = converterProvider
	p := converterProvider.Get("markdown")
	conv, err := p.New(converter.DocumentContext{})
	if err != nil {
		return nil, err
	}
	spec.MardownConverter = conv
	if as, ok := conv.(converter.AnchorNameSanitizer); ok {
		spec.anchorNameSanitizer = as
	} else {
		// Use Goldmark's sanitizer
		p := converterProvider.Get("goldmark")
		conv, err := p.New(converter.DocumentContext{})
		if err != nil {
			return nil, err
		}
		spec.anchorNameSanitizer = conv.(converter.AnchorNameSanitizer)
	}
	return spec, nil
}

var stripHTMLReplacer = strings.NewReplacer("\n", " ", "</p>", "\n", "<br>", "\n", "<br />", "\n")

/*
// CD had to add these two functions -- performance issues?
// TODO just ToLower the main string once
func CIHasPrefix(s, substr string) bool { // Assumes substr is already lower case
	s2 := strings.ToLower(s)
	return strings.HasPrefix(s2, substr)
}
func CIIndex(s, substr string) int {
	s2 := strings.ToLower(s)
	return strings.Index(s2, substr)
}
*/

// StripHTML accepts a string, strips out all HTML tags and returns it.
// NOTES re exclusions:
//        * could do full HTML parsing -- but that's probably unnecessary and expensive
//        * could convert the string to []rune -- but that should be unnecessary because HTML tags are ASCII only,
//          and converting runes to lower case might affect the number of bytes in the string.
//        * searching for '<' and '>' seems to be safe, because those characters in text
//          have already been replaced by &lt; and &gt; by the time the string gets here.
//func StripHTML(s string) string {
func StripHTML(s string, exclusions []string) string {

	//fmt.Printf("\n\n========================== StripHTML: s=%q\n", s)
	// Shortcut strings with no tags in them
	if !strings.ContainsAny(s, "<>") {
		return s
	}
	s = stripHTMLReplacer.Replace(s)
	//fmt.Printf("\nStripHTML: replaced s=%q\n", s)

	// Walk through the string removing all tags
	b := bp.GetBuffer()
	defer bp.PutBuffer(b)
	var inTag, isSpace, wasSpace bool
	var sLower string // lower-case copy of s, only needed if there are tags to exclude
	// IMPORTANT: we use i to index both s and sLower -- this would break if we converted to runes first, I think
	if exclusions != nil {
		sLower = strings.ToLower(s)
	}
	//for i, r := range s {
	for i := 0; i < len(s); i += 1 {
		if !inTag {
			isSpace = false
		}

		switch {
		case s[i] == '<':
			inTag = true
			//fmt.Printf("next bit is %q\n", s[i:])
			// TODO get list of tags to skip from options.. !! Ignore any void tags
			//if s[i:(i+len("<figcaption"))] == "<figcaption" {
			for _, tagname := range exclusions {
				//fmt.Printf("\nStripHTML, found <, considering tag %s\n", tagname)
				tagname = strings.ToLower(tagname)
				// Need to find end of tag in case of substring matches (e.g. 'fig' vs 'figcaption')
				if strings.HasPrefix(sLower[i+1:], tagname) && (sLower[i+1+len(tagname)] == ' ' || sLower[i+1+len(tagname)] == '>') {
					// skip up to the end tag
					endtag := "</" + tagname
					endpos := strings.Index(sLower[i:], endtag) + len(endtag)
					endpos2 := 0
					if endpos > -1 {
						// find the closing ">"
						endpos2 = strings.Index(sLower[i+endpos:], ">")
						//fmt.Printf("\nSH: i=%d endpos=%d endpos2=%d s1=%s s2=%s\n", i, endpos, endpos2, sLower[i:i+endpos], sLower[i+endpos:i+endpos+8])
					}
					if endpos < 0 || endpos2 < 0 {
						//fmt.Printf("\nStripHTML: failed to find closing endtag, so ignoring this tag\n")
					} else {
						// Found complete tag -- skip over it
						//fmt.Printf("\nStripHTML: found %s, skipped %d characters: %s (endpos=%d endpos2=%d)\n", tagname, endpos+endpos2+1, sLower[i:i+endpos+endpos2+1], endpos, endpos2)
						i += endpos + endpos2 // i will get an extra +1 at the top of the loop
						inTag = false         // we've dealt with '>'
						// Don't need to try any other exclusions at this position
						break
					}
				}
			}
		case s[i] == '>':
			inTag = false
		case unicode.IsSpace(rune(s[i])): // FIXME is this the one place where we need runes?
			isSpace = true
			fallthrough
		default:
			if !inTag && (!isSpace || (isSpace && !wasSpace)) {
				b.WriteByte(s[i])
			}
		}

		wasSpace = isSpace

	}
	//fmt.Printf("\nStripHTML: returning: %s\n", b.String())
	return b.String()
}

/* from play:

package main

import (
	"fmt"
	bp "github.com/gohugoio/hugo/bufferpool"
	"strings"
)

func main() {
	b := bp.GetBuffer()
	defer bp.PutBuffer(b)
	fmt.Println("Hello, playground")
	s := "abcd:<figcaption blah  >This is the caption</figcaption>;more"
	for i := 0; i < len(s); i += 1 {
		fmt.Printf("char %d is %c\n", i, s[i])
		if s[i] == '<' {
			fmt.Printf("next bit is %q\n", s[i:])
			// TODO get list of tags to skip from options.. !! Ignore any void tags
			//if s[i:(i+len("<figcaption"))] == "<figcaption" {
			if strings.HasPrefix(s[i+1:], "figcaption") {
				// skip up to first '</figcaption[\s]*>'
				endpos := strings.Index(s[i:], "</figcaption") + len("</figcaption")
				endpos2 := 0
				if endpos > -1 {
					// find the closing ">"
					endpos2 = strings.Index(s[i+endpos:], ">")
				}
				if endpos < 0 || endpos2 < 0 {
					fmt.Printf("\nStripHTML: failed to find closing </figcaption>\n")
				} else {
					// Found complete figcaption -- skip over it
					fmt.Printf("\nStripHTML: !!! skipped %d characters: %q (endpos=%d endpos2=%d)\n", endpos+endpos2, s[i:i+endpos+endpos2+1], endpos, endpos2)
					i += endpos + endpos2
				}
			}
		} else {
			b.WriteByte(s[i])
		}
	}
	fmt.Printf("\nStripHTML: returning: %s\n", b.String())
}

*/

// StripHTML accepts a string, strips out all HTML tags and returns it.
func StripHTMLOld(s string) string {

	// Shortcut strings with no tags in them
	if !strings.ContainsAny(s, "<>") {
		return s
	}
	s = stripHTMLReplacer.Replace(s)

	// Walk through the string removing all tags
	b := bp.GetBuffer()
	defer bp.PutBuffer(b)
	var inTag, isSpace, wasSpace bool
	for _, r := range s {
		if !inTag {
			isSpace = false
		}

		switch {
		case r == '<':
			inTag = true
		case r == '>':
			inTag = false
		case unicode.IsSpace(r):
			isSpace = true
			fallthrough
		default:
			if !inTag && (!isSpace || (isSpace && !wasSpace)) {
				b.WriteRune(r)
			}
		}

		wasSpace = isSpace

	}
	return b.String()
}

// stripEmptyNav strips out empty <nav> tags from content.
func stripEmptyNav(in []byte) []byte {
	return bytes.Replace(in, []byte("<nav>\n</nav>\n\n"), []byte(``), -1)
}

// BytesToHTML converts bytes to type template.HTML.
func BytesToHTML(b []byte) template.HTML {
	return template.HTML(string(b))
}

// ExtractTOC extracts Table of Contents from content.
func ExtractTOC(content []byte) (newcontent []byte, toc []byte) {
	if !bytes.Contains(content, []byte("<nav>")) {
		return content, nil
	}
	origContent := make([]byte, len(content))
	copy(origContent, content)
	first := []byte(`<nav>
<ul>`)

	last := []byte(`</ul>
</nav>`)

	replacement := []byte(`<nav id="TableOfContents">
<ul>`)

	startOfTOC := bytes.Index(content, first)

	peekEnd := len(content)
	if peekEnd > 70+startOfTOC {
		peekEnd = 70 + startOfTOC
	}

	if startOfTOC < 0 {
		return stripEmptyNav(content), toc
	}
	// Need to peek ahead to see if this nav element is actually the right one.
	correctNav := bytes.Index(content[startOfTOC:peekEnd], []byte(`<li><a href="#`))
	if correctNav < 0 { // no match found
		return content, toc
	}
	lengthOfTOC := bytes.Index(content[startOfTOC:], last) + len(last)
	endOfTOC := startOfTOC + lengthOfTOC

	newcontent = append(content[:startOfTOC], content[endOfTOC:]...)
	toc = append(replacement, origContent[startOfTOC+len(first):endOfTOC]...)
	return
}

func (c *ContentSpec) RenderMarkdown(src []byte) ([]byte, error) {
	b, err := c.MardownConverter.Convert(converter.RenderContext{Src: src})
	if err != nil {
		return nil, err
	}
	return b.Bytes(), nil
}

func (c *ContentSpec) SanitizeAnchorName(s string) string {
	return c.anchorNameSanitizer.SanitizeAnchorName(s)
}

func (c *ContentSpec) ResolveMarkup(in string) string {
	in = strings.ToLower(in)
	switch in {
	case "md", "markdown", "mdown":
		return "markdown"
	case "html", "htm":
		return "html"
	default:
		if in == "mmark" {
			Deprecated("Markup type mmark", "See https://gohugo.io//content-management/formats/#list-of-content-formats", false)
		}
		if conv := c.Converters.Get(in); conv != nil {
			return conv.Name()
		}
	}
	return ""
}

// TotalWords counts instance of one or more consecutive white space
// characters, as defined by unicode.IsSpace, in s.
// This is a cheaper way of word counting than the obvious len(strings.Fields(s)).
func TotalWords(s string) int {
	n := 0
	inWord := false
	for _, r := range s {
		wasInWord := inWord
		inWord = !unicode.IsSpace(r)
		if inWord && !wasInWord {
			n++
		}
	}
	return n
}

// TruncateWordsByRune truncates words by runes.
func (c *ContentSpec) TruncateWordsByRune(in []string) (string, bool) {
	words := make([]string, len(in))
	copy(words, in)

	count := 0
	for index, word := range words {
		if count >= c.summaryLength {
			return strings.Join(words[:index], " "), true
		}
		runeCount := utf8.RuneCountInString(word)
		if len(word) == runeCount {
			count++
		} else if count+runeCount < c.summaryLength {
			count += runeCount
		} else {
			for ri := range word {
				if count >= c.summaryLength {
					truncatedWords := append(words[:index], word[:ri])
					return strings.Join(truncatedWords, " "), true
				}
				count++
			}
		}
	}

	return strings.Join(words, " "), false
}

// TruncateWordsToWholeSentence takes content and truncates to whole sentence
// limited by max number of words. It also returns whether it is truncated.
// Issues:
//  s not trimmed if early return
//  isEndOfSentence includes \n !!  (and ").  But not 'foreign' language punctuation
func (c *ContentSpec) TruncateWordsToWholeSentence(s string) (string, bool) {
	var (
		wordCount     = 0
		lastWordIndex = -1
	)

	for i, r := range s {
		if unicode.IsSpace(r) {
			wordCount++
			lastWordIndex = i

			if wordCount >= c.summaryLength {
				break
			}

		}
	}

	if lastWordIndex == -1 {
		fmt.Println("TWTWS: returning with lastWordIndex == -1")
		return s, false
	}

	endIndex := -1
	fmt.Printf("TWTWS: s='%s', len=%d, endIndex=%d, s[lWI:]=%s\n", s, len(s), endIndex, s[lastWordIndex:])

	for j, r := range s[lastWordIndex:] {
		if isEndOfSentence(r) {
			endIndex = j + lastWordIndex + utf8.RuneLen(r)
			fmt.Printf("TWTWS: ... r=%c lastWordIndex=%d j=%d endIndex=%d\n", r, lastWordIndex, j, endIndex)
			break
		}
	}

	if endIndex == -1 {
		fmt.Println("TWTWS: returning with endIndex == -1")
		return s, false
	}

	fmt.Printf("TWTWS: returning %q trimmed to %q\n", s[:endIndex], strings.TrimSpace(s[:endIndex]))
	return strings.TrimSpace(s[:endIndex]), endIndex < len(s)
}

// TruncateWordsToWholeSentence takes content and truncates to whole sentence
// limited by max number of words. It also returns whether it is truncated.
func (c *ContentSpec) NewTruncateWordsToWholeSentence(s string) (string, bool) {
	var (
		wordCount     = 0
		lastWordIndex = -1
		runeString    = []rune(strings.TrimSpace(s))
	)

	for i, r := range runeString {
		if unicode.IsSpace(r) {
			wordCount++
			lastWordIndex = i

			if wordCount >= c.summaryLength {
				break
			}

		}
	}

	if lastWordIndex == -1 {
		return s, false
	}

	endIndex := -1
	fmt.Printf("TWTWS: s='%s', len=%d, endIndex=%d, s[lWI:]=%s\n", s, len(s), endIndex, s[lastWordIndex:])

	for j, r := range runeString[lastWordIndex:] {
		if isEndOfSentence(r) {
			endIndex = j + lastWordIndex + 1 // utf8.RuneLen(r)
			fmt.Printf("TWTWS: ... r=%c lastWordIndex=%d j=%d endIndex=%d\n", r, lastWordIndex, j, endIndex)
			break
		}
	}

	if endIndex == -1 {
		return s, false
	}

	return strings.TrimSpace(string(runeString[:endIndex])), endIndex < len(runeString)
}

// TruncateWordsWithEllipsis takes content and truncates to whole sentence
// limited by max number of words. It also returns whether it is truncated.
func (c *ContentSpec) TruncateWordsWithEllipsis(s string) (string, bool) {
	const (
		UnicodeEllipsis = '\u2026'
		HTMLEllipsis    = "&#8230;"
		ASCIIEllipsis   = "..."
	)
	var (
		wordCount     = 0
		lastWordIndex = -1
		runes         = []rune(strings.TrimSpace(s))
	)

	// Awkward edge cases
	if len(runes) == 0 {
		return "", false
	}
	if c.summaryLength < 1 {
		return "", true
	}

	for i, r := range runes {
		if unicode.IsSpace(r) {
			wordCount++
			lastWordIndex = i
			if wordCount >= c.summaryLength {
				break
			}
		}
	}
	//fmt.Printf("TWWE: s='%s', len=%d, n=%d, wordCount=%d, lWI=%d\n", s, len(runes), c.summaryLength, wordCount, lastWordIndex)

	if wordCount < c.summaryLength {
		return string(runes), false
	}

	// Need to truncate.
	ellipsis := ""
	// lWI is the index of the space after the last word.
	//fmt.Printf("TWWE: runes[lWI-1] is %q.  Punct? %v\n", runes[lastWordIndex-1], unicode.IsPunct(runes[lastWordIndex-1]))
	if unicode.IsPunct(runes[lastWordIndex-1]) {
		//fmt.Printf("TWWE: trailing ...? %q\n", string(runes[lastWordIndex-len(ASCIIEllipsis):lastWordIndex]))
		if runes[lastWordIndex-1] == UnicodeEllipsis {
			// edge case -- existing Unicode Ellipsis -- replace it with an HTML one
			lastWordIndex -= 1
			ellipsis = HTMLEllipsis
		} else if string(runes[lastWordIndex-len(HTMLEllipsis):lastWordIndex]) == HTMLEllipsis {
			// edge case -- last word ends in HTMLEllipsis -- don't add another one
		} else if string(runes[lastWordIndex-len(ASCIIEllipsis):lastWordIndex]) == ASCIIEllipsis {
			// edge case -- last word ends with '...' -- replace with HTML ellipsis
			lastWordIndex -= len(ASCIIEllipsis)
			ellipsis = HTMLEllipsis
		} else {
			// add a space and then the ellipsis
			ellipsis = " " + HTMLEllipsis
		}
	} else {
		// breaking in the middle of a sentence -- no space before ellipsis
		ellipsis = HTMLEllipsis
	}
	return string(runes[:lastWordIndex]) + ellipsis, true
}

// TrimShortHTML removes the <p>/</p> tags from HTML input in the situation
// where said tags are the only <p> tags in the input and enclose the content
// of the input (whitespace excluded).
func (c *ContentSpec) TrimShortHTML(input []byte) []byte {
	firstOpeningP := bytes.Index(input, paragraphIndicator)
	lastOpeningP := bytes.LastIndex(input, paragraphIndicator)

	lastClosingP := bytes.LastIndex(input, closingPTag)
	lastClosing := bytes.LastIndex(input, closingIndicator)

	if firstOpeningP == lastOpeningP && lastClosingP == lastClosing {
		input = bytes.TrimSpace(input)
		input = bytes.TrimPrefix(input, openingPTag)
		input = bytes.TrimSuffix(input, closingPTag)
		input = bytes.TrimSpace(input)
	}
	return input
}

// http://www.unicode.org/reports/tr29/tr29-4.html#Sentence_Boundaries
func isEndOfSentence(r rune) bool {
	return r == '.' || r == '?' || r == '!' || r == '"' || r == '\n'
}

// Kept only for benchmark.
func (c *ContentSpec) truncateWordsToWholeSentenceOld(content string) (string, bool) {
	words := strings.Fields(content)

	if c.summaryLength >= len(words) {
		return strings.Join(words, " "), false
	}

	for counter, word := range words[c.summaryLength:] {
		if strings.HasSuffix(word, ".") ||
			strings.HasSuffix(word, "?") ||
			strings.HasSuffix(word, ".\"") ||
			strings.HasSuffix(word, "!") {
			upper := c.summaryLength + counter + 1
			return strings.Join(words[:upper], " "), (upper < len(words))
		}
	}

	return strings.Join(words[:c.summaryLength], " "), true
}

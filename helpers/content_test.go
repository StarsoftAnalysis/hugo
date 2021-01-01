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

package helpers

import (
	"bytes"
	"fmt" // CD only
	"html/template"
	"strings"
	"testing"

	"github.com/spf13/afero"

	"github.com/gohugoio/hugo/common/loggers"

	"github.com/spf13/viper"

	qt "github.com/frankban/quicktest"
)

const tstHTMLContent = "<!DOCTYPE html><html><head><script src=\"http://two/foobar.js\"></script></head><body><nav><ul><li hugo-nav=\"section_0\"></li><li hugo-nav=\"section_1\"></li></ul></nav><article>content <a href=\"http://two/foobar\">foobar</a>. Follow up</article><p>This is some text.<br>And some more.</p></body></html>"

func TestTrimShortHTML(t *testing.T) {
	tests := []struct {
		input, output []byte
	}{
		{[]byte(""), []byte("")},
		{[]byte("Plain text"), []byte("Plain text")},
		{[]byte("  \t\n Whitespace text\n\n"), []byte("Whitespace text")},
		{[]byte("<p>Simple paragraph</p>"), []byte("Simple paragraph")},
		{[]byte("\n  \n \t  <p> \t Whitespace\nHTML  \n\t </p>\n\t"), []byte("Whitespace\nHTML")},
		{[]byte("<p>Multiple</p><p>paragraphs</p>"), []byte("<p>Multiple</p><p>paragraphs</p>")},
		{[]byte("<p>Nested<p>paragraphs</p></p>"), []byte("<p>Nested<p>paragraphs</p></p>")},
		{[]byte("<p>Hello</p>\n<ul>\n<li>list1</li>\n<li>list2</li>\n</ul>"), []byte("<p>Hello</p>\n<ul>\n<li>list1</li>\n<li>list2</li>\n</ul>")},
	}

	c := newTestContentSpec()
	for i, test := range tests {
		output := c.TrimShortHTML(test.input)
		if !bytes.Equal(test.output, output) {
			t.Errorf("Test %d failed. Expected %q got %q", i, test.output, output)
		}
	}
}

func TestStripHTML(t *testing.T) {
	type test struct {
		input      string
		exclusions []string
		expected   string
	}
	data := []test{
		// Tests with no exclusions
		{"<h1>strip h1 tag <h1>", nil, "strip h1 tag "},
		{"<p> strip p tag </p>", []string{}, " strip p tag "}, // alternate way to specify empty slice
		{"</br> strip br<br>", nil, " strip br\n"},
		{"</br> strip br2<br />", nil, " strip br2\n"},
		{"This <strong>is</strong> a\nnewline", nil, "This is a newline"},
		{"No Tags", nil, "No Tags"},
		{"<quote>γνῶθι σεαυτόν.</quote>", nil, "γνῶθι σεαυτόν."},   // multi-byte characters
		{"\xe2<hr>\x8c\x98", nil, "⌘"},                             // multi-byte character split by tag
		{"Unclosed tag: <input foo bar", nil, "Unclosed tag: "},    // unclosed tag -- further text lost
		{"h2>Head 2</h2><p>Rhubarb...", nil, "h2Head 2Rhubarb..."}, // unopened tag -- treated as text  FIXME ?? should there be a space between Head 2 and Rhubarb ??
		{`<p>Summary Next Line.
<figure >

        <img src="/not/real" />


</figure>
.
More text here.</p>

<p>Some more text</p>`, nil, "Summary Next Line.  . More text here.\nSome more text\n"},
		// Tests with exclusions
		{"Text: <figure><img src=\"xyz.png\"><figcaption>This is a caption</figcaption></fig> More text", []string{"figcaption"}, "Text:  More text"},
		{"A<h1>Head1</H1>B<H2>Head2</h2>C<H3>Head3</h3>D", []string{"h1", "H3"}, "ABHead2CD"},                     // multiple tags, mixed case
		{"Lorem <table border=1><tr><td>ipsum</td></tr></table > dolor", []string{"table"}, "Lorem  dolor"},       // complex tag with attribute
		{"<ul><li>Item1<li><ul><li>Item2a</ul><li>Item3</ul>", []string{"ul"}, "Item3"},                           // nested complex tag -- fails to remove whole outer UL !!
		{"₤<₧>₭</₧>€", []string{"₧"}, "₤€"},                                                                       // multi-byte characters in text and tag (not currently valid HTML)
		{"<quote>γνῶθι σεαυτόν.</quote>", []string{"quote"}, ""},                                                  // multi-byte characters within tag
		{"Abc <figcaption>Caption for the fig</figc> Xyz", []string{"figcaption"}, "Abc Caption for the fig Xyz"}, // poorly ended tag - contents left alone
		{"Abc <p>blurb", []string{"p"}, "Abc blurb"},                                                              // unended tag - contents left alone
		{"1<input type=button>2", []string{"input"}, "12"},                                                        // void tag, so exclusion is superfluous but still works
		{"A<i>i<b>bold italic</i>?</b>Z", []string{"i", "b"}, "A?Z"},                                              // wrongly nested tags -- </b> gets stripped anyway
		{"ABC<figcaption>Wo>r</ds</figcaption foo=bar>XYZ", []string{"figcaption"}, "ABCXYZ"},                     // <,> in caption, spurious attribute in end tag
	}
	for i, d := range data {
		output := StripHTML(d.input, d.exclusions)
		if d.expected != output {
			t.Errorf("Test %d failed. Expected %q got %q", i, d.expected, output)
		}
	}
}

func BenchmarkStripHTML(b *testing.B) {
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		StripHTML(tstHTMLContent, nil)
	}
}

func BenchmarkStripHTMLWithExclusions(b *testing.B) {
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		StripHTML(tstHTMLContent, []string{"nav"})
	}
}

func TestStripEmptyNav(t *testing.T) {
	c := qt.New(t)
	cleaned := stripEmptyNav([]byte("do<nav>\n</nav>\n\nbedobedo"))
	c.Assert(cleaned, qt.DeepEquals, []byte("dobedobedo"))
}

func TestBytesToHTML(t *testing.T) {
	c := qt.New(t)
	c.Assert(BytesToHTML([]byte("dobedobedo")), qt.Equals, template.HTML("dobedobedo"))
}

func TestNewContentSpec(t *testing.T) {
	cfg := viper.New()
	c := qt.New(t)

	cfg.Set("summaryLength", 32)
	cfg.Set("buildFuture", true)
	cfg.Set("buildExpired", true)
	cfg.Set("buildDrafts", true)

	spec, err := NewContentSpec(cfg, loggers.NewErrorLogger(), afero.NewMemMapFs())

	c.Assert(err, qt.IsNil)
	c.Assert(spec.summaryLength, qt.Equals, 32)
	c.Assert(spec.BuildFuture, qt.Equals, true)
	c.Assert(spec.BuildExpired, qt.Equals, true)
	c.Assert(spec.BuildDrafts, qt.Equals, true)

}

var benchmarkTruncateString = strings.Repeat("This is a sentence about nothing.", 20)

func BenchmarkTestTruncateWordsToWholeSentence(b *testing.B) {
	c := newTestContentSpec()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		c.TruncateWordsToWholeSentence(benchmarkTruncateString)
	}
}

func BenchmarkTestTruncateWordsToWholeSentenceOld(b *testing.B) {
	c := newTestContentSpec()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		c.truncateWordsToWholeSentenceOld(benchmarkTruncateString)
	}
}

func TestTruncateWordsToWholeSentence(t *testing.T) {
	c := newTestContentSpec()
	type test struct {
		input, expected string
		max             int
		truncated       bool
	}
	data := []test{
		//{"a b c", "a b c", 12, false},
		//{"a b c", "a b c", 3, false},
		//{"a", "a", 1, false},
		//{"This is a sentence.", "This is a sentence.", 5, false}, // Short, not trimmed
		//{"This is also a sentence!", "This is also a sentence!", 1, false},
		//{"To be. Or not to be. That's the question.", "To be.", 1, true},
		//{" \nThis is not a sentence\nAnd this is another", "This is not a sentence", 4, true},
		//{"", "", 10, false},
		//{"This... is a more difficult test?", "This... is a more difficult test?", 1, false},

		//{" ἀλλὰ τί ἦ μοι ταῦτα περὶ δρῦν ἢ περὶ πέτρην;? ", "ἀλλὰ τί ἦ μοι ταῦτα περὶ δρῦν ἢ περὶ πέτρην;?", 2, false}, // Greek question mark at end
		//{"    Our plans to use the building in Fordingbridge Recreation Ground are moving forward at last. The Salisbury Journal reports that the Town Council are backing the project, and are keen to put a lease in place so that Avon Valley Shed will finally have a place to use.", "Our plans to use the building in Fordingbridge Recreation Ground are moving forward at last. The Salisbury Journal reports that the Town Council are backing the project, and are keen to put a lease in place so that Avon Valley Shed will finally have a place to use.", 70, false},
		{"Off by one\nerror.", "Off by one\nerror.", 2, false},
		{"New line\nin text.", "New line\nin text.", 2, false},
	}
	for i, d := range data {
		c.summaryLength = d.max
		output, truncated := c.TruncateWordsToWholeSentence(d.input)
		fmt.Printf("%q => %q, %v, %v\n", d.input, output, d.truncated, truncated)
		if d.expected != output {
			t.Errorf("Test %d (%q) failed. Expected %q got %q", i, d.input, d.expected, output)
		}

		if d.truncated != truncated {
			t.Errorf("Test %d (%q) failed. Expected truncated=%t got %t", i, d.input, d.truncated, truncated)
		}
	}
}

func TestTruncateWordsWithEllipsis(t *testing.T) {
	c := newTestContentSpec()
	type test struct {
		input, expected string
		max             int
		truncated       bool
	}
	data := []test{
		{"", "", 3, false},                // Null case
		{"", "", 0, false},                // Null case
		{"\t", "", 44, false},             // White space only
		{"Anything at all.", "", 0, true}, // No words required
		{"So shaken as we are, so wan with care", "So shaken&#8230;", 2, true},                             // Ellipsis with no space
		{"So shaken as we are, so wan", "So shaken as we are, &#8230;", 5, true},                           // Ellipsis after punctuation and space
		{"Short sentence.  More text.", "Short sentence. &#8230;", 2, true},                                // Ditto
		{"No worries, eh?", "No worries, eh?", 3, false},                                                   // Exact number of words, no truncation
		{"  Trim my spaces. ", "Trim my spaces.", 99, false},                                               // Extra word allowance, no truncation
		{" ἀλλὰ τί ἦ μοι ταῦτα περὶ δρῦν ἢ περὶ πέτρην; ", "ἀλλὰ τί ἦ μοι&#8230;", 4, true},                // Unicode
		{"Archimedes shouted \"εὕρηκα!\", allegedly.", "Archimedes shouted \"εὕρηκα!\", &#8230;", 3, true}, // Mixed ASCII and Unicode
		{"To be continued&#8230;  Same time, same channel.", "To be continued&#8230;", 3, true},            // Edge cases with existing ellipses
		{"To be continued\u2026  Same time, same channel.", "To be continued&#8230;", 3, true},
		{"To be continued...  Same time, same channel.", "To be continued&#8230;", 3, true},
		{"...", "...", 1, false}, // No truncation, so don't replace ... with &#8230;
	}
	for i, d := range data {
		c.summaryLength = d.max
		output, truncated := c.TruncateWordsWithEllipsis(d.input)
		//fmt.Printf("%q => %q, %v, %v\n", d.input, output, d.truncated, truncated)
		if d.expected != output {
			t.Errorf("Test %d (%q) failed. Expected %q got %q", i, d.input, d.expected, output)
		}
		if d.truncated != truncated {
			t.Errorf("Test %d (%q) failed. Expected truncated=%t got %t", i, d.input, d.truncated, truncated)
		}
	}
}

func TestTruncateWordsByRune(t *testing.T) {
	c := newTestContentSpec()
	type test struct {
		input, expected string
		max             int
		truncated       bool
	}
	data := []test{
		{"", "", 1, false},
		{"a b c", "a b c", 12, false},
		{"a b c", "a b c", 3, false},
		{"a", "a", 1, false},
		{"Hello 中国", "", 0, true},
		{"这是中文，全中文。", "这是中文，", 5, true},
		{"Hello 中国", "Hello 中", 2, true},
		{"Hello 中国", "Hello 中国", 3, false},
		{"Hello中国 Good 好的", "Hello中国 Good 好", 9, true},
		{"This is a sentence.", "This is", 2, true},
		{"This is also a sentence!", "This", 1, true},
		{"To be. Or not to be. That's the question.", "To be. Or not", 4, true},
		{" \nThis is    not a sentence\n ", "This is not", 3, true},
	}
	for i, d := range data {
		c.summaryLength = d.max
		output, truncated := c.TruncateWordsByRune(strings.Fields(d.input))
		if d.expected != output {
			t.Errorf("Test %d failed. Expected %q got %q", i, d.expected, output)
		}

		if d.truncated != truncated {
			t.Errorf("Test %d failed. Expected truncated=%t got %t", i, d.truncated, truncated)
		}
	}
}

func TestExtractTOCNormalContent(t *testing.T) {
	content := []byte("<nav>\n<ul>\nTOC<li><a href=\"#")

	actualTocLessContent, actualToc := ExtractTOC(content)
	expectedTocLess := []byte("TOC<li><a href=\"#")
	expectedToc := []byte("<nav id=\"TableOfContents\">\n<ul>\n")

	if !bytes.Equal(actualTocLessContent, expectedTocLess) {
		t.Errorf("Actual tocless (%s) did not equal expected (%s) tocless content", actualTocLessContent, expectedTocLess)
	}

	if !bytes.Equal(actualToc, expectedToc) {
		t.Errorf("Actual toc (%s) did not equal expected (%s) toc content", actualToc, expectedToc)
	}
}

func TestExtractTOCGreaterThanSeventy(t *testing.T) {
	content := []byte("<nav>\n<ul>\nTOC This is a very long content which will definitely be greater than seventy, I promise you that.<li><a href=\"#")

	actualTocLessContent, actualToc := ExtractTOC(content)
	//Because the start of Toc is greater than 70+startpoint of <li> content and empty TOC will be returned
	expectedToc := []byte("")

	if !bytes.Equal(actualTocLessContent, content) {
		t.Errorf("Actual tocless (%s) did not equal expected (%s) tocless content", actualTocLessContent, content)
	}

	if !bytes.Equal(actualToc, expectedToc) {
		t.Errorf("Actual toc (%s) did not equal expected (%s) toc content", actualToc, expectedToc)
	}
}

func TestExtractNoTOC(t *testing.T) {
	content := []byte("TOC")

	actualTocLessContent, actualToc := ExtractTOC(content)
	expectedToc := []byte("")

	if !bytes.Equal(actualTocLessContent, content) {
		t.Errorf("Actual tocless (%s) did not equal expected (%s) tocless content", actualTocLessContent, content)
	}

	if !bytes.Equal(actualToc, expectedToc) {
		t.Errorf("Actual toc (%s) did not equal expected (%s) toc content", actualToc, expectedToc)
	}
}

var totalWordsBenchmarkString = strings.Repeat("Hugo Rocks ", 200)

func TestTotalWords(t *testing.T) {

	for i, this := range []struct {
		s     string
		words int
	}{
		{"Two, Words!", 2},
		{"Word", 1},
		{"", 0},
		{"One, Two,      Three", 3},
		{totalWordsBenchmarkString, 400},
	} {
		actualWordCount := TotalWords(this.s)

		if actualWordCount != this.words {
			t.Errorf("[%d] Actual word count (%d) for test string (%s) did not match %d", i, actualWordCount, this.s, this.words)
		}
	}
}

func BenchmarkTotalWords(b *testing.B) {
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		wordCount := TotalWords(totalWordsBenchmarkString)
		if wordCount != 400 {
			b.Fatal("Wordcount error")
		}
	}
}

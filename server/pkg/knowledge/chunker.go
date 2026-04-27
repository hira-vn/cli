package knowledge

import (
	"regexp"
	"strings"
)

// maxChunkChars caps each chunk to keep embedding and display reasonable.
const maxChunkChars = 1500

var headingRe = regexp.MustCompile(`(?m)^(#{1,3})\s+(.+?)\s*$`)

// ChunkMarkdown splits markdown into retrievable chunks. Splits on H1/H2/H3
// headings, then further splits any resulting section larger than
// maxChunkChars on paragraph boundaries so each chunk fits the embedding
// window comfortably.
//
// The first chunk of a doc keeps an empty heading when the doc starts with
// prose before any heading.
func ChunkMarkdown(body string) []Chunk {
	body = strings.ReplaceAll(body, "\r\n", "\n")
	body = strings.TrimSpace(body)
	if body == "" {
		return nil
	}

	// Find heading positions.
	headingLocs := headingRe.FindAllStringIndex(body, -1)
	headingGroups := headingRe.FindAllStringSubmatch(body, -1)

	type rawSection struct {
		heading string
		content string
	}
	var sections []rawSection

	if len(headingLocs) == 0 {
		sections = append(sections, rawSection{heading: "", content: body})
	} else {
		// Prefix before first heading.
		if headingLocs[0][0] > 0 {
			prefix := strings.TrimSpace(body[:headingLocs[0][0]])
			if prefix != "" {
				sections = append(sections, rawSection{heading: "", content: prefix})
			}
		}
		for i, loc := range headingLocs {
			heading := strings.TrimSpace(headingGroups[i][2])
			start := loc[1] // after the heading line
			var end int
			if i+1 < len(headingLocs) {
				end = headingLocs[i+1][0]
			} else {
				end = len(body)
			}
			content := strings.TrimSpace(body[start:end])
			if content == "" && heading == "" {
				continue
			}
			sections = append(sections, rawSection{heading: heading, content: content})
		}
	}

	var out []Chunk
	ordinal := 0
	for _, s := range sections {
		for _, piece := range splitBySize(s.content, maxChunkChars) {
			out = append(out, Chunk{
				Ordinal: ordinal,
				Heading: s.heading,
				Content: piece,
			})
			ordinal++
		}
	}
	return out
}

// splitBySize breaks a long text into <= max char pieces on paragraph
// boundaries (blank lines), falling back to sentence boundaries, then to a
// hard cut if a single block is still too large.
func splitBySize(text string, max int) []string {
	text = strings.TrimSpace(text)
	if len(text) <= max {
		if text == "" {
			return nil
		}
		return []string{text}
	}

	paragraphs := strings.Split(text, "\n\n")
	var out []string
	var buf strings.Builder

	flush := func() {
		if buf.Len() > 0 {
			out = append(out, strings.TrimSpace(buf.String()))
			buf.Reset()
		}
	}

	for _, p := range paragraphs {
		p = strings.TrimSpace(p)
		if p == "" {
			continue
		}
		if len(p) > max {
			flush()
			// Split oversized paragraph at sentence boundaries.
			for _, piece := range splitSentences(p, max) {
				out = append(out, piece)
			}
			continue
		}
		if buf.Len()+len(p)+2 > max {
			flush()
		}
		if buf.Len() > 0 {
			buf.WriteString("\n\n")
		}
		buf.WriteString(p)
	}
	flush()
	return out
}

var sentenceRe = regexp.MustCompile(`[^.!?\n]+[.!?]+|\S+$`)

func splitSentences(text string, max int) []string {
	matches := sentenceRe.FindAllString(text, -1)
	if len(matches) == 0 {
		return hardCut(text, max)
	}
	var out []string
	var buf strings.Builder
	for _, s := range matches {
		s = strings.TrimSpace(s)
		if s == "" {
			continue
		}
		if len(s) > max {
			if buf.Len() > 0 {
				out = append(out, strings.TrimSpace(buf.String()))
				buf.Reset()
			}
			out = append(out, hardCut(s, max)...)
			continue
		}
		if buf.Len()+len(s)+1 > max {
			out = append(out, strings.TrimSpace(buf.String()))
			buf.Reset()
		}
		if buf.Len() > 0 {
			buf.WriteByte(' ')
		}
		buf.WriteString(s)
	}
	if buf.Len() > 0 {
		out = append(out, strings.TrimSpace(buf.String()))
	}
	return out
}

func hardCut(text string, max int) []string {
	var out []string
	for len(text) > max {
		out = append(out, text[:max])
		text = text[max:]
	}
	if text != "" {
		out = append(out, text)
	}
	return out
}

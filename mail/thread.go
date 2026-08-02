package mail

import (
	"fmt"
	"html"
	"regexp"
	"strings"
)

var (
	subjectPrefixRe = regexp.MustCompile(`(?i)^(Re|Fwd|FW)\s*:\s*`)
	htmlTagRe       = regexp.MustCompile(`<[^>]*>`)

	// Elements whose content is not text at all. Stripping only the tags
	// would leave a stylesheet or a script sitting in the message. Spelled out
	// per element because RE2 has no backreference to match the closing tag.
	htmlDropRe = regexp.MustCompile(`(?is)` +
		`<script\b[^>]*>.*?</script\s*>` +
		`|<style\b[^>]*>.*?</style\s*>` +
		`|<head\b[^>]*>.*?</head\s*>`)

	// Markup that stands for a line ending.
	htmlBreakRe = regexp.MustCompile(`(?i)<br\s*/?>|</(p|div|tr|li|h[1-6]|blockquote)\s*>`)

	htmlRunOfSpaceRe = regexp.MustCompile(`[^\S\n]+`)
	htmlBlankRunRe   = regexp.MustCompile(`\n{3,}`)
)

func BuildReplyHeaders(original *Message) (inReplyTo string, references []string) {
	inReplyTo = original.MessageID
	references = make([]string, 0, len(original.References)+1)
	references = append(references, original.References...)
	if original.MessageID != "" {
		references = append(references, original.MessageID)
	}
	return
}

func StripSubjectPrefix(subject string) string {
	s := subject
	for {
		trimmed := subjectPrefixRe.ReplaceAllString(s, "")
		if trimmed == s {
			return strings.TrimSpace(s)
		}
		s = trimmed
	}
}

func ReplySubject(original string) string {
	return "Re: " + StripSubjectPrefix(original)
}

func ForwardSubject(original string) string {
	return "Fwd: " + StripSubjectPrefix(original)
}

func QuoteBody(original *Message) string {
	body := original.TextBody
	if body == "" && original.HTMLBody != "" {
		body = htmlToText(original.HTMLBody)
	}

	var b strings.Builder
	b.WriteString("\n--- Original Message ---\n")
	b.WriteString(fmt.Sprintf("From: %s\n", original.From.String()))
	b.WriteString(fmt.Sprintf("Date: %s\n", original.Date.Format("Mon, 02 Jan 2006 15:04:05 -0700")))
	b.WriteString(fmt.Sprintf("Subject: %s\n\n", original.Subject))
	for _, line := range strings.Split(body, "\n") {
		b.WriteString("> " + line + "\n")
	}
	return b.String()
}

// QuoteBodyHTML is QuoteBody for the HTML half of a reply or forward. An
// original that was HTML is quoted as itself rather than flattened to text and
// escaped back, which would discard the formatting its sender chose. Script
// and style blocks are dropped: they are not prose, and passing them on in a
// message you are signing is not something to do by accident.
func QuoteBodyHTML(original *Message) string {
	var quoted string
	if original.HTMLBody != "" {
		quoted = htmlDropRe.ReplaceAllString(original.HTMLBody, "")
	} else {
		quoted = "<pre>" + html.EscapeString(original.TextBody) + "</pre>"
	}

	var b strings.Builder
	b.WriteString("\n<hr>\n<p>--- Original Message ---<br>\n")
	b.WriteString(fmt.Sprintf("From: %s<br>\n", html.EscapeString(original.From.String())))
	b.WriteString(fmt.Sprintf("Date: %s<br>\n",
		html.EscapeString(original.Date.Format("Mon, 02 Jan 2006 15:04:05 -0700"))))
	b.WriteString(fmt.Sprintf("Subject: %s</p>\n", html.EscapeString(original.Subject)))
	b.WriteString("<blockquote>\n")
	b.WriteString(quoted)
	b.WriteString("\n</blockquote>\n")
	return b.String()
}

// htmlToText renders HTML as the plain-text version of the same message. Not a
// browser: it drops the elements that carry no prose, breaks a line where the
// markup implies one, and resolves entities. Enough that a reader without HTML
// gets the message rather than a wall of markup or somebody's stylesheet.
//
// Entities are resolved after tags are stripped, so an escaped &lt;b&gt; in
// the source cannot come back as a tag that then goes unstripped.
func htmlToText(s string) string {
	s = htmlDropRe.ReplaceAllString(s, "")
	s = htmlBreakRe.ReplaceAllString(s, "\n")
	s = htmlTagRe.ReplaceAllString(s, "")
	s = html.UnescapeString(s)

	s = htmlRunOfSpaceRe.ReplaceAllString(s, " ")
	s = htmlBlankRunRe.ReplaceAllString(s, "\n\n")

	lines := strings.Split(s, "\n")
	for i, line := range lines {
		lines[i] = strings.TrimSpace(line)
	}
	return strings.TrimSpace(strings.Join(lines, "\n"))
}

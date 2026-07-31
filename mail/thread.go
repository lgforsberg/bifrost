package mail

import (
	"fmt"
	"regexp"
	"strings"
)

var (
	subjectPrefixRe = regexp.MustCompile(`(?i)^(Re|Fwd|FW)\s*:\s*`)
	htmlTagRe       = regexp.MustCompile(`<[^>]*>`)
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
		body = stripHTMLTags(original.HTMLBody)
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

func stripHTMLTags(s string) string {
	return strings.TrimSpace(htmlTagRe.ReplaceAllString(s, ""))
}

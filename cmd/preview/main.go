// Command preview serves the git-gud README as a styled HTML page on port
// 3000. It exists only to give the Base44 development environment a reachable
// preview for this CLI-only project; it is not part of the published tool.
package main

import (
	"fmt"
	"net/http"
	"os"
	"strings"

	gitgud "github.com/igor-makarov/git-gud"
)

func main() {
	addr := ":" + getenv("PORT", "3000")
	mux := http.NewServeMux()
	mux.HandleFunc("/", serve)
	fmt.Fprintf(os.Stderr, "preview listening on %s\n", addr)
	if err := http.ListenAndServe(addr, mux); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}

func serve(w http.ResponseWriter, r *http.Request) {
	if r.URL.Path != "/" {
		http.NotFound(w, r)
		return
	}
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	fmt.Fprint(w, page(gitgud.README))
}

func getenv(key, fallback string) string {
	if v := os.Getenv(key); v != "" {
		return v
	}
	return fallback
}

func page(md string) string {
	body := renderMarkdown(md)
	return `<!DOCTYPE html>
<html lang="en">
<head>
<meta charset="utf-8">
<meta name="viewport" content="width=device-width, initial-scale=1">
<title>git-gud</title>
<style>
  :root { --bg:#0d1117; --fg:#e6edf3; --code:#161b22; --border:#30363d;
           --accent:#58a6ff; --muted:#8b949e; --green:#7ee787; }
  * { box-sizing: border-box; }
  body { margin:0; background:var(--bg); color:var(--fg);
         font-family:-apple-system,BlinkMacSystemFont,"Segoe UI",Helvetica,Arial,sans-serif;
         line-height:1.6; }
  .wrap { max-width:880px; margin:0 auto; padding:48px 24px 80px; }
  h1,h2,h3 { margin-top:1.8em; line-height:1.25; }
  h1 { font-size:2.2em; border-bottom:1px solid var(--border); padding-bottom:.3em; }
  h2 { font-size:1.5em; border-bottom:1px solid var(--border); padding-bottom:.3em; }
  h3 { font-size:1.2em; }
  a { color:var(--accent); text-decoration:none; }
  a:hover { text-decoration:underline; }
  code { background:var(--code); padding:.15em .4em; border-radius:6px;
         font-size:.9em; font-family:"SFMono-Regular",Consolas,monospace; }
  pre { background:var(--code); border:1px solid var(--border); border-radius:8px;
        padding:16px; overflow-x:auto; }
  pre code { background:none; padding:0; font-size:.875em; }
  ul,ol { padding-left:1.5em; }
  hr { border:none; border-top:1px solid var(--border); margin:2em 0; }
  .badge { display:inline-block; background:var(--code); color:var(--green);
           border:1px solid var(--border); border-radius:999px;
           padding:2px 12px; font-size:.8em; margin-bottom:1em; }
</style>
</head>
<body>
<div class="wrap">
<span class="badge">● build passing</span>
` + body + `
</div>
</body>
</html>`
}

// renderMarkdown is a minimal Markdown-to-HTML converter covering the
// subset used by the git-gud README: headings, fenced code blocks, inline
// code, links, lists, and horizontal rules.
func renderMarkdown(md string) string {
	lines := strings.Split(md, "\n")
	var b strings.Builder
	inCode := false
	inList := false

	flush := func() {
		if inList {
			b.WriteString("</ul>\n")
			inList = false
		}
	}

	for _, line := range lines {
		// fenced code blocks
		if strings.HasPrefix(line, "```") {
			if inCode {
				b.WriteString("</code></pre>\n")
				inCode = false
			} else {
				flush()
				b.WriteString("<pre><code>")
				inCode = true
			}
			continue
		}
		if inCode {
			b.WriteString(htmlEscape(line) + "\n")
			continue
		}

		// headings
		if h := heading(line); h != "" {
			flush()
			b.WriteString(h + "\n")
			continue
		}
		// horizontal rule
		if strings.TrimSpace(line) == "---" || strings.TrimSpace(line) == "***" {
			flush()
			b.WriteString("<hr>\n")
			continue
		}
		// list items
		if strings.HasPrefix(strings.TrimSpace(line), "- ") {
			if !inList {
				b.WriteString("<ul>\n")
				inList = true
			}
			b.WriteString("<li>" + inline(strings.TrimSpace(line)[2:]) + "</li>\n")
			continue
		}
		// blank or plain text
		if strings.TrimSpace(line) == "" {
			flush()
			continue
		}
		flush()
		b.WriteString("<p>" + inline(line) + "</p>\n")
	}
	if inCode {
		b.WriteString("</code></pre>\n")
	}
	flush()
	return b.String()
}

func heading(line string) string {
	s := strings.TrimSpace(line)
	switch {
	case strings.HasPrefix(s, "### "):
		return "<h3>" + inline(s[4:]) + "</h3>"
	case strings.HasPrefix(s, "## "):
		return "<h2>" + inline(s[3:]) + "</h2>"
	case strings.HasPrefix(s, "# "):
		return "<h1>" + inline(s[2:]) + "</h1>"
	}
	return ""
}

func inline(s string) string {
	s = htmlEscape(s)
	// inline code
	s = replaceAll(s, "`", "`", "<code>", "</code>")
	// links [text](url)
	s = renderLinks(s)
	return s
}

func renderLinks(s string) string {
	for {
		i := strings.Index(s, "[")
		if i < 0 {
			break
		}
		j := strings.Index(s[i:], "](")
		if j < 0 {
			break
		}
		j += i
		k := strings.Index(s[j:], ")")
		if k < 0 {
			break
		}
		k += j
		text := s[i+1 : j]
		url := s[j+2 : k]
		link := `<a href="` + url + `">` + text + "</a>"
		s = s[:i] + link + s[k+1:]
	}
	return s
}

// replaceAll replaces each occurrence of delimited segments.
func replaceAll(s, delim, _, open, close string) string {
	var b strings.Builder
	for {
		i := strings.Index(s, delim)
		if i < 0 {
			b.WriteString(s)
			break
		}
		b.WriteString(s[:i])
		rest := s[i+len(delim):]
		j := strings.Index(rest, delim)
		if j < 0 {
			b.WriteString(s[i:])
			break
		}
		b.WriteString(open)
		b.WriteString(rest[:j])
		b.WriteString(close)
		s = rest[j+len(delim):]
	}
	return b.String()
}

func htmlEscape(s string) string {
	s = strings.ReplaceAll(s, "&", "&amp;")
	s = strings.ReplaceAll(s, "<", "&lt;")
	s = strings.ReplaceAll(s, ">", "&gt;")
	return s
}

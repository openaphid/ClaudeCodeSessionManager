package ui

import "strings"

// splitArgs tokenizes a flags string typed in the resume/new dialog into argv
// elements, honoring single and double quotes so values with spaces survive
// (e.g. --add-dir "/some path"). It is not a full shell parser — it handles
// quoting and whitespace, which covers the flags users pass to claude.
//
// Tokens that would clash with the --resume claude already injects
// (--resume/-r/--continue/-c) are dropped, so a stray resume flag can't produce
// a conflicting double invocation.
func splitArgs(s string) []string {
	var args []string
	var cur strings.Builder
	inTok := false
	var quote rune // 0, '\'' or '"'

	flush := func() {
		if inTok {
			args = append(args, cur.String())
			cur.Reset()
			inTok = false
		}
	}

	for _, r := range s {
		switch {
		case quote != 0:
			if r == quote {
				quote = 0
			} else {
				cur.WriteRune(r)
			}
			inTok = true
		case r == '\'' || r == '"':
			quote = r
			inTok = true
		case r == ' ' || r == '\t':
			flush()
		default:
			cur.WriteRune(r)
			inTok = true
		}
	}
	flush()

	out := args[:0]
	for _, a := range args {
		switch a {
		case "--resume", "-r", "--continue", "-c":
			continue
		}
		out = append(out, a)
	}
	return out
}

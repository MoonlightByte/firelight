// Package compiler lowers NQL text through tokens and AST into core typed IR.
package compiler

import (
	"encoding/json"
	"fmt"
	"strconv"
	"unicode/utf8"
)

// MaxSourceBytes bounds an action source. A world source declares every entity
// at genesis, so it has the larger MaxWorldSourceBytes (docs/WORLD_SIZE.md).
const (
	MaxSourceBytes      = 1 << 20
	MaxWorldSourceBytes = 16 << 20
)

type Position struct {
	Source string `json:"source"`
	Line   int    `json:"line"`
	Column int    `json:"column"`
}
type Diagnostic struct {
	Position
	Code       string `json:"code"`
	Expected   string `json:"expected"`
	Actual     string `json:"actual"`
	Message    string `json:"message"`
	Suggestion string `json:"suggestion,omitempty"`
}

func (d Diagnostic) Error() string {
	return fmt.Sprintf("%s:%d:%d: %s: %s", d.Source, d.Line, d.Column, d.Code, d.Message)
}

type tokenKind uint8

const (
	eof tokenKind = iota
	word
	quoted
	integer
	punctuation
)

type token struct {
	Kind tokenKind
	Text string
	Pos  Position
}

func diag(p Position, code, expected, actual, message string) *Diagnostic {
	return &Diagnostic{Position: p, Code: code, Expected: expected, Actual: actual, Message: message}
}

func lex(source, text string, limit int) ([]token, *Diagnostic) {
	p := Position{source, 1, 1}
	if len(text) > limit {
		return nil, diag(p, "E_LIMIT", fmt.Sprintf("source <= %d MiB", limit>>20), "oversized source", "source limit exceeded")
	}
	out := []token{}
	i := 0
	advance := func() {
		r, n := utf8.DecodeRuneInString(text[i:])
		i += n
		if r == '\n' {
			p.Line++
			p.Column = 1
		} else {
			p.Column++
		}
	}
	for i < len(text) {
		r, n := utf8.DecodeRuneInString(text[i:])
		if r == utf8.RuneError && n == 1 {
			return nil, diag(p, "E_UTF8", "valid UTF-8", "invalid byte", "source is not valid UTF-8")
		}
		if r == ' ' || r == '\t' || r == '\r' || r == '\n' {
			advance()
			continue
		}
		if r == '/' && i+1 < len(text) && text[i+1] == '/' {
			for i < len(text) && text[i] != '\n' {
				rr, nn := utf8.DecodeRuneInString(text[i:])
				if rr == utf8.RuneError && nn == 1 {
					return nil, diag(p, "E_UTF8", "valid UTF-8", "invalid byte", "comment contains invalid UTF-8")
				}
				advance()
			}
			continue
		}
		start, pos := i, p
		if r == '"' {
			advance()
			closed := false
			for i < len(text) {
				rr, nn := utf8.DecodeRuneInString(text[i:])
				if rr == utf8.RuneError && nn == 1 {
					return nil, diag(p, "E_UTF8", "valid UTF-8", "invalid byte", "string contains invalid UTF-8")
				}
				if rr == '"' {
					advance()
					closed = true
					break
				}
				if rr == '\\' {
					advance()
					if i < len(text) {
						rr, nn = utf8.DecodeRuneInString(text[i:])
						if rr == utf8.RuneError && nn == 1 {
							return nil, diag(p, "E_UTF8", "valid UTF-8", "invalid byte", "string escape contains invalid UTF-8")
						}
						advance()
					}
				} else {
					advance()
				}
			}
			if !closed {
				return nil, diag(pos, "E_STRING", "closing quote", "end of input", "unterminated string")
			}
			raw := text[start:i]
			var value string
			if err := json.Unmarshal([]byte(raw), &value); err != nil || !validSurrogates(raw) {
				return nil, diag(pos, "E_ESCAPE", "JSON string with paired Unicode surrogates", raw, "invalid string or escape")
			}
			out = append(out, token{quoted, value, pos})
			continue
		}
		if (r >= 'a' && r <= 'z') || (r >= 'A' && r <= 'Z') || r == '_' {
			for i < len(text) {
				c := text[i]
				if !(c >= 'a' && c <= 'z' || c >= 'A' && c <= 'Z' || c >= '0' && c <= '9' || c == '_') {
					break
				}
				advance()
			}
			out = append(out, token{word, text[start:i], pos})
			continue
		}
		if r >= '0' && r <= '9' || (r == '-' || r == '+') && i+1 < len(text) && text[i+1] >= '0' && text[i+1] <= '9' {
			advance()
			for i < len(text) && text[i] >= '0' && text[i] <= '9' {
				advance()
			}
			out = append(out, token{integer, text[start:i], pos})
			continue
		}
		if r == '{' || r == '}' || r == ';' || r == '=' {
			advance()
			out = append(out, token{punctuation, string(r), pos})
			continue
		}
		return nil, diag(p, "E_TOKEN", "keyword, string, integer, or punctuation", string(r), "unexpected source character")
	}
	return append(out, token{eof, "<eof>", p}), nil
}

// encoding/json replaces lone surrogates. Reject those instead of silently
// repairing source. The JSON decoder has already validated escape spellings.
func validSurrogates(raw string) bool {
	for i := 1; i < len(raw)-1; i++ {
		if raw[i] != '\\' {
			continue
		}
		i++
		if raw[i] != 'u' {
			continue
		}
		v, err := strconv.ParseUint(raw[i+1:i+5], 16, 16)
		if err != nil {
			return false
		}
		i += 4
		if v >= 0xDC00 && v <= 0xDFFF {
			return false
		}
		if v >= 0xD800 && v <= 0xDBFF {
			if i+6 >= len(raw) || raw[i+1] != '\\' || raw[i+2] != 'u' {
				return false
			}
			w, err := strconv.ParseUint(raw[i+3:i+7], 16, 16)
			if err != nil || w < 0xDC00 || w > 0xDFFF {
				return false
			}
			i += 6
		}
	}
	return true
}

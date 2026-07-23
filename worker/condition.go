package worker

import (
	"fmt"
	"os"
	"strconv"
	"strings"
	"unicode"
)

// EvaluateCondition expands $VAR references using the provided vars map,
// then evaluates the resulting expression. Supported operators:
// ==, !=, >, <, contains, !contains, && (AND), || (OR).
// Values can be single-quoted strings or bare words. Parentheses are supported.
func EvaluateCondition(condition string, vars map[string]string) (bool, error) {
	if condition == "" {
		return true, nil
	}

	expanded := os.Expand(condition, func(key string) string {
		return vars[key]
	})

	p := &condParser{input: expanded}
	result, err := p.parseOr()
	if err != nil {
		return false, fmt.Errorf("condition %q: %w", condition, err)
	}

	p.skipSpaces()
	if p.pos < len(p.input) {
		return false, fmt.Errorf("condition %q: unexpected trailing text %q", condition, p.input[p.pos:])
	}

	return result, nil
}

type condParser struct {
	input string
	pos   int
}

func (p *condParser) skipSpaces() {
	for p.pos < len(p.input) && unicode.IsSpace(rune(p.input[p.pos])) {
		p.pos++
	}
}

func (p *condParser) parseOr() (bool, error) {
	left, err := p.parseAnd()
	if err != nil {
		return false, err
	}

	for {
		p.skipSpaces()
		if p.pos+1 < len(p.input) && p.input[p.pos:p.pos+2] == "||" {
			p.pos += 2
			right, err := p.parseAnd()
			if err != nil {
				return false, err
			}
			left = left || right
		} else {
			break
		}
	}
	return left, nil
}


func (p *condParser) parseAnd() (bool, error) {
	left, err := p.parseCompare()
	if err != nil {
		return false, err
	}

	for {
		p.skipSpaces()
		if p.pos+1 < len(p.input) && p.input[p.pos:p.pos+2] == "&&" {
			p.pos += 2
			right, err := p.parseCompare()
			if err != nil {
				return false, err
			}
			left = left && right
		} else {
			break
		}
	}
	return left, nil
}

func (p *condParser) parseCompare() (bool, error) {
	left, err := p.parseValue()
	if err != nil {
		return false, err
	}

	p.skipSpaces()

	// Check for comparison operator
	op := p.peekOp()
	if op == "" {
		// No operator — treat the value as a boolean: non-empty string is true
		return left != "", nil
	}

	p.pos += len(op)

	right, err := p.parseValue()
	if err != nil {
		return false, err
	}

	switch op {
	case "==":
		return left == right, nil
	case "!=":
		return left != right, nil
	case ">":
		lf, le := strconv.ParseFloat(left, 64)
		rf, re := strconv.ParseFloat(right, 64)
		if le == nil && re == nil {
			return lf > rf, nil
		}
		return left > right, nil
	case "<":
		lf, le := strconv.ParseFloat(left, 64)
		rf, re := strconv.ParseFloat(right, 64)
		if le == nil && re == nil {
			return lf < rf, nil
		}
		return left < right, nil
	case "contains":
		return strings.Contains(left, right), nil
	case "!contains":
		return !strings.Contains(left, right), nil
	default:
		return false, fmt.Errorf("unknown operator %q", op)
	}
}

func (p *condParser) peekOp() string {
	if p.pos >= len(p.input) {
		return ""
	}

	// Two-char operators first
	if p.pos+1 < len(p.input) {
		two := p.input[p.pos : p.pos+2]
		if two == "==" || two == "!=" {
			return two
		}
	}

	// !contains (must check before single char)
	if p.pos+9 <= len(p.input) && p.input[p.pos:p.pos+9] == "!contains" {
		// Make sure it's not part of a longer word
		if p.pos+9 >= len(p.input) || !isWordChar(p.input[p.pos+9]) {
			return "!contains"
		}
	}

	// contains
	if p.pos+8 <= len(p.input) && p.input[p.pos:p.pos+8] == "contains" {
		if p.pos+8 >= len(p.input) || !isWordChar(p.input[p.pos+8]) {
			return "contains"
		}
	}

	ch := p.input[p.pos]
	if ch == '>' || ch == '<' {
		return string(ch)
	}

	return ""
}

func (p *condParser) parseValue() (string, error) {
	p.skipSpaces()

	if p.pos >= len(p.input) {
		return "", nil
	}

	// Parenthesized expression — evaluate and return "true"/"false"
	if p.input[p.pos] == '(' {
		p.pos++ // skip '('
		result, err := p.parseOr()
		if err != nil {
			return "", err
		}
		p.skipSpaces()
		if p.pos >= len(p.input) || p.input[p.pos] != ')' {
			return "", fmt.Errorf("expected ')' at position %d", p.pos)
		}
		p.pos++ // skip ')'
		if result {
			return "true", nil
		}
		return "", nil
	}

	// Single-quoted string
	if p.input[p.pos] == '\'' {
		p.pos++ // skip opening quote
		start := p.pos
		for p.pos < len(p.input) && p.input[p.pos] != '\'' {
			p.pos++
		}
		if p.pos >= len(p.input) {
			return "", fmt.Errorf("unterminated single-quoted string starting at position %d", start-1)
		}
		val := p.input[start:p.pos]
		p.pos++ // skip closing quote
		return val, nil
	}

	// Bare word: read until space or operator character
	start := p.pos
	for p.pos < len(p.input) {
		ch := p.input[p.pos]
		if unicode.IsSpace(rune(ch)) || ch == ')' || ch == '(' || ch == '>' || ch == '<' {
			break
		}
		// Check for two-char operators
		if p.pos+1 < len(p.input) {
			two := p.input[p.pos : p.pos+2]
			if two == "==" || two == "!=" || two == "&&" || two == "||" {
				break
			}
		}
		p.pos++
	}
	return p.input[start:p.pos], nil
}

func isWordChar(b byte) bool {
	return (b >= 'a' && b <= 'z') || (b >= 'A' && b <= 'Z') || (b >= '0' && b <= '9') || b == '_'
}

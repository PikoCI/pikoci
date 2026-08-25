package condition

import (
	"fmt"
	"os"
	"strconv"
	"strings"
	"unicode"
)

// Evaluate evaluates a condition expression, substituting $VAR
// references from the provided vars map. Supported operators:
// ==, !=, >, <, contains, !contains, && (AND), || (OR).
// Values can be single-quoted strings or bare words. Parentheses are supported.
//
// Variables are expanded per value, after the expression has been split into
// tokens, so a value containing spaces, quotes or operators is compared as
// data and cannot alter the shape of the expression.
func Evaluate(condition string, vars map[string]string) (bool, error) {
	condition = strings.TrimSpace(condition)
	if condition == "" {
		return true, nil
	}

	p := &condParser{input: condition, vars: vars}
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
	vars  map[string]string
}

// expand substitutes $VAR references in a single parsed value.
func (p *condParser) expand(value string) string {
	if !strings.ContainsRune(value, '$') {
		return value
	}
	return os.Expand(value, func(key string) string { return p.vars[key] })
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
		// No operator — treat the value as a boolean. Anything non-empty is
		// true except the strings that spell out falsehood, so that a variable
		// holding "false" or "0" does not select the branch.
		switch strings.ToLower(left) {
		case "", "false", "0":
			return false, nil
		default:
			return true, nil
		}
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
		return "", fmt.Errorf("expected a value at position %d", p.pos)
	}

	// A leading "!" is not negation; rejecting it beats silently evaluating
	// "!$FLAG" as the bare word "!false", which is always true.
	if p.input[p.pos] == '!' && p.peekOp() != "!contains" && p.peekOp() != "!=" {
		return "", fmt.Errorf("unsupported %q at position %d, negation is not available; compare explicitly instead", "!", p.pos)
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
		return p.expand(val), nil
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
	return p.expand(p.input[start:p.pos]), nil
}

func isWordChar(b byte) bool {
	return (b >= 'a' && b <= 'z') || (b >= 'A' && b <= 'Z') || (b >= '0' && b <= '9') || b == '_'
}

// Validate reports whether a condition is syntactically usable, so a typo is
// caught when the pipeline is saved rather than by a failing build. Variables
// are unset here, which only affects the outcome, not the syntax.
func Validate(cond string) error {
	_, err := Evaluate(cond, nil)
	return err
}

package tools

import (
	"fmt"
	"math"
	"strconv"
	"unicode"
)

// eval parses and evaluates a basic arithmetic expression. It supports +, -,
// *, /, unary minus, parentheses, and numeric literals. It is intentionally
// tiny: a recursive-descent parser over a fixed grammar, no eval of arbitrary
// code.
func eval(expr string) (float64, error) {
	p := &parser{s: expr}
	v, err := p.parseExpr()
	if err != nil {
		return 0, err
	}
	p.skipSpace()
	if p.pos != len(p.s) {
		return 0, fmt.Errorf("unexpected %q at position %d", p.s[p.pos:], p.pos)
	}
	return v, nil
}

type parser struct {
	s   string
	pos int
}

func (p *parser) skipSpace() {
	for p.pos < len(p.s) && unicode.IsSpace(rune(p.s[p.pos])) {
		p.pos++
	}
}

// parseExpr handles addition and subtraction.
func (p *parser) parseExpr() (float64, error) {
	left, err := p.parseTerm()
	if err != nil {
		return 0, err
	}
	for {
		p.skipSpace()
		if p.pos >= len(p.s) {
			return left, nil
		}
		switch p.s[p.pos] {
		case '+':
			p.pos++
			right, err := p.parseTerm()
			if err != nil {
				return 0, err
			}
			left += right
		case '-':
			p.pos++
			right, err := p.parseTerm()
			if err != nil {
				return 0, err
			}
			left -= right
		default:
			return left, nil
		}
	}
}

// parseTerm handles multiplication and division.
func (p *parser) parseTerm() (float64, error) {
	left, err := p.parseFactor()
	if err != nil {
		return 0, err
	}
	for {
		p.skipSpace()
		if p.pos >= len(p.s) {
			return left, nil
		}
		switch p.s[p.pos] {
		case '*':
			p.pos++
			right, err := p.parseFactor()
			if err != nil {
				return 0, err
			}
			left *= right
		case '/':
			p.pos++
			right, err := p.parseFactor()
			if err != nil {
				return 0, err
			}
			if right == 0 {
				return 0, fmt.Errorf("division by zero")
			}
			left /= right
		default:
			return left, nil
		}
	}
}

// parseFactor handles unary minus, parentheses, and numeric literals.
func (p *parser) parseFactor() (float64, error) {
	p.skipSpace()
	if p.pos >= len(p.s) {
		return 0, fmt.Errorf("unexpected end of expression")
	}
	switch {
	case p.s[p.pos] == '-':
		p.pos++
		v, err := p.parseFactor()
		return -v, err
	case p.s[p.pos] == '+':
		p.pos++
		return p.parseFactor()
	case p.s[p.pos] == '(':
		p.pos++
		v, err := p.parseExpr()
		if err != nil {
			return 0, err
		}
		p.skipSpace()
		if p.pos >= len(p.s) || p.s[p.pos] != ')' {
			return 0, fmt.Errorf("missing closing parenthesis")
		}
		p.pos++
		return v, nil
	default:
		return p.parseNumber()
	}
}

func (p *parser) parseNumber() (float64, error) {
	start := p.pos
	for p.pos < len(p.s) && (unicode.IsDigit(rune(p.s[p.pos])) || p.s[p.pos] == '.') {
		p.pos++
	}
	if start == p.pos {
		return 0, fmt.Errorf("expected a number at position %d", p.pos)
	}
	v, err := strconv.ParseFloat(p.s[start:p.pos], 64)
	if err != nil {
		return 0, err
	}
	return v, nil
}

// formatFloat renders a result without a trailing ".0" for whole numbers and
// without floating-point noise for near-integers.
func formatFloat(v float64) string {
	if v == math.Trunc(v) && math.Abs(v) < 1e15 {
		return strconv.FormatInt(int64(v), 10)
	}
	return strconv.FormatFloat(v, 'g', -1, 64)
}

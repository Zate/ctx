package query

import (
	"fmt"
	"strings"
	"unicode"
)

// QueryAST represents a parsed query expression.
type QueryAST struct {
	Type     string    `json:"type"` // "predicate", "and", "or", "not"
	Key      string    `json:"key,omitempty"`
	Operator string    `json:"operator,omitempty"`
	Value    string    `json:"value,omitempty"`
	Left     *QueryAST `json:"left,omitempty"`
	Right    *QueryAST `json:"right,omitempty"`
	Child    *QueryAST `json:"child,omitempty"`
}

var validKeys = map[string]bool{
	"type":    true,
	"tag":     true,
	"created": true,
	"updated": true,
	"tokens":  true,
	"has":     true,
	"from":    true,
	"to":      true,
	"kind":    true, // explicit kind scoping: kind:memory, kind:document, kind:content
}

type parser struct {
	tokens []token
	pos    int
}

type tokenType int

const (
	tokenWord tokenType = iota
	tokenColon
	tokenLParen
	tokenRParen
	tokenOp // >, <, >=, <=
	tokenEOF
)

type token struct {
	typ   tokenType
	value string
}

// Parse parses a query string into an AST.
func Parse(input string) (*QueryAST, error) {
	input = strings.TrimSpace(input)
	if input == "" {
		return nil, nil
	}

	tokens, err := tokenize(input)
	if err != nil {
		return nil, err
	}

	p := &parser{tokens: tokens}
	ast, err := p.parseExpr()
	if err != nil {
		return nil, err
	}

	if p.pos < len(p.tokens) && p.tokens[p.pos].typ != tokenEOF {
		return nil, fmt.Errorf("unexpected token at position %d: %s", p.pos, p.tokens[p.pos].value)
	}

	return ast, nil
}

func tokenize(input string) ([]token, error) {
	var tokens []token
	i := 0

	for i < len(input) {
		ch := rune(input[i])

		if unicode.IsSpace(ch) {
			i++
			continue
		}

		if ch == '(' {
			tokens = append(tokens, token{tokenLParen, "("})
			i++
			continue
		}
		if ch == ')' {
			tokens = append(tokens, token{tokenRParen, ")"})
			i++
			continue
		}
		if ch == ':' {
			tokens = append(tokens, token{tokenColon, ":"})
			i++
			continue
		}
		if ch == '>' || ch == '<' {
			if i+1 < len(input) && input[i+1] == '=' {
				tokens = append(tokens, token{tokenOp, string(input[i : i+2])})
				i += 2
			} else {
				tokens = append(tokens, token{tokenOp, string(ch)})
				i++
			}
			continue
		}

		// Read a word (includes letters, digits, hyphens, dots, underscores)
		start := i
		for i < len(input) {
			c := rune(input[i])
			if unicode.IsSpace(c) || c == '(' || c == ')' || c == ':' {
				break
			}
			// Allow > and < only at start of word context (operators), not mid-word
			if (c == '>' || c == '<') && i > start {
				break
			}
			if c == '>' || c == '<' {
				break
			}
			i++
		}
		if i > start {
			tokens = append(tokens, token{tokenWord, input[start:i]})
		}
	}

	tokens = append(tokens, token{tokenEOF, ""})
	return tokens, nil
}

func (p *parser) peek() token {
	if p.pos >= len(p.tokens) {
		return token{tokenEOF, ""}
	}
	return p.tokens[p.pos]
}

func (p *parser) next() token {
	t := p.peek()
	p.pos++
	return t
}

func (p *parser) parseExpr() (*QueryAST, error) {
	left, err := p.parseTerm()
	if err != nil {
		return nil, err
	}

	for {
		t := p.peek()
		if t.typ == tokenWord && (strings.ToUpper(t.value) == "AND" || strings.ToUpper(t.value) == "OR") {
			op := strings.ToLower(p.next().value)
			right, err := p.parseTerm()
			if err != nil {
				return nil, err
			}
			left = &QueryAST{
				Type:  op,
				Left:  left,
				Right: right,
			}
		} else {
			break
		}
	}

	return left, nil
}

func (p *parser) parseTerm() (*QueryAST, error) {
	t := p.peek()
	if t.typ == tokenWord && strings.ToUpper(t.value) == "NOT" {
		p.next()
		child, err := p.parseFactor()
		if err != nil {
			return nil, err
		}
		return &QueryAST{
			Type:  "not",
			Child: child,
		}, nil
	}
	return p.parseFactor()
}

func (p *parser) parseFactor() (*QueryAST, error) {
	t := p.peek()

	if t.typ == tokenLParen {
		p.next()
		expr, err := p.parseExpr()
		if err != nil {
			return nil, err
		}
		if p.peek().typ != tokenRParen {
			return nil, fmt.Errorf("expected closing parenthesis")
		}
		p.next()
		return expr, nil
	}

	return p.parsePredicate()
}

func (p *parser) parsePredicate() (*QueryAST, error) {
	key := p.next()
	if key.typ != tokenWord {
		return nil, fmt.Errorf("expected key, got %q", key.value)
	}

	if !validKeys[key.value] {
		return nil, fmt.Errorf("unknown key: %s", key.value)
	}

	colon := p.next()
	if colon.typ != tokenColon {
		return nil, fmt.Errorf("expected ':' after key %q", key.value)
	}

	// Check for operator
	var operator string
	if p.peek().typ == tokenOp {
		operator = p.next().value
	}

	// Read value - may include colons (e.g., tag:project:ctx, tag:tier:reference)
	value, err := p.readValue()
	if err != nil {
		return nil, fmt.Errorf("expected value after %s:", key.value)
	}

	return &QueryAST{
		Type:     "predicate",
		Key:      key.value,
		Operator: operator,
		Value:    value,
	}, nil
}

func (p *parser) readValue() (string, error) {
	t := p.next()
	if t.typ == tokenEOF {
		return "", fmt.Errorf("unexpected end of input")
	}
	if t.typ != tokenWord {
		return "", fmt.Errorf("unexpected token: %q", t.value)
	}

	value := t.value

	// Continue reading colon-separated parts for tag values like project:ctx
	for p.peek().typ == tokenColon {
		p.next() // consume colon
		next := p.peek()
		if next.typ == tokenWord {
			value += ":" + p.next().value
		} else {
			// Trailing colon, put it back conceptually
			break
		}
	}

	return value, nil
}

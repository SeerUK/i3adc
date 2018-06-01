package xrandr

import (
	"fmt"
	"regexp"
	"strconv"
	"strings"
)

var (
	dimensionPattern  = regexp.MustCompile(`^([0-9]+)mm$`)
	resolutionPattern = regexp.MustCompile(`^([0-9]+)x([0-9]+)$`)
)

type Parser struct {
	lexer  *Lexer
	token  Token
	skipWS bool
}

func NewParser() *Parser {
	return &Parser{
		skipWS: true,
	}
}

func (p *Parser) ParseProps(input []byte) (PropsOutput, error) {
	var props PropsOutput

	// This isn't thread-safe.
	p.lexer = NewLexer(input)

	err := p.scan()
	if err != nil {
		return props, err
	}

	// "Parse" the screen, we actually just skip it entirely really.
	err = p.parseScreen()
	if err != nil {
		return props, err
	}

	for {
		output, err := p.parseOutput()
		if err != nil {
			return props, err
		}

		props.Outputs = append(props.Outputs, output)

		if p.token.Type == TokenTypeEOF {
			break
		}
	}

	return props, nil
}

func (p *Parser) parseOutputName(output *Output) error {
	p.skipWS = true

	tok, err := p.expect(TokenTypeName)
	if err != nil {
		return err
	}

	output.Name = tok.Literal

	return nil
}

func (p *Parser) parseOutputStatus(output *Output) error {
	p.skipWS = true

	if err := p.scan(); err != nil {
		return err
	}

	if p.token.Literal == "connected" {
		output.IsConnected = true

		if err := p.scan(); err != nil {
			return err
		}
	}

	// This is where we'll start branching. Is this output primary?
	if p.token.Type == TokenTypeName && p.token.Literal == "primary" {
		output.IsPrimary = true

		if err := p.scan(); err != nil {
			return err
		}
	}

	return nil
}

func (p *Parser) parseResolutionAndPosition(output *Output) error {
	p.skipWS = true

	// If the output is enabled, we should see the current resolution, and the position.
	if p.token.Type == TokenTypeName {
		isRes, res := p.parseResolution(p.token.Literal)
		if !isRes {
			return nil
		}

		output.IsEnabled = true
		output.Resolution = res

		err := p.scan()
		if err != nil {
			return err
		}

		if err := p.skipWithLiteral(TokenTypePunctuator, "+"); err != nil {
			return err
		}

		tok, err := p.expect(TokenTypeIntValue)
		if err != nil {
			return err
		}

		offsetX, err := strconv.ParseInt(tok.Literal, 10, 64)
		if err != nil {
			return err
		}

		if err := p.skipWithLiteral(TokenTypePunctuator, "+"); err != nil {
			return err
		}

		tok, err = p.expect(TokenTypeIntValue)
		if err != nil {
			return err
		}

		offsetY, err := strconv.ParseInt(tok.Literal, 10, 64)
		if err != nil {
			return err
		}

		output.Position.OffsetX = int(offsetX)
		output.Position.OffsetY = int(offsetY)
	}

	return nil
}

func (p *Parser) parseOutputRotationAndReflection(output *Output) error {
	p.skipWS = true

	if p.token.Type == TokenTypeName {
		var found bool
		switch p.token.Literal {
		case "normal":
			output.Rotation = RotationNormal
			found = true
		case "left":
			output.Rotation = RotationLeft
			found = true
		case "inverted":
			output.Rotation = RotationInverted
			found = true
		case "right":
			output.Rotation = RotationRight
			found = true
		default:
			found = false
		}

		if found {
			err := p.scan()
			if err != nil {
				return err
			}
		}
	}

	if p.token.Type == TokenTypeName {
		var foundX bool
		var foundY bool

		switch p.token.Literal {
		case "x":
			foundX = true
		case "y":
			foundY = true
		}

		if foundX || foundY {
			err := p.scan()
			if err != nil {
				return err
			}

			if p.token.Type == TokenTypeName && p.token.Literal == "axis" {
				if foundX {
					output.Reflection = ReflectionXAxis
				}

				if foundY {
					output.Reflection = ReflectionYAxis
				}

				err := p.scan()
				if err != nil {
					return err
				}
			}
		}
	}

	return nil
}

func (p *Parser) parseOutputRotationAndReflectionKey() error {
	p.skipWS = true

	return p.all(
		p.skipWithLiteral(TokenTypePunctuator, "("),
		p.skipWithLiteral(TokenTypeName, "normal"),
		p.skipWithLiteral(TokenTypeName, "left"),
		p.skipWithLiteral(TokenTypeName, "inverted"),
		p.skipWithLiteral(TokenTypeName, "right"),
		p.skipWithLiteral(TokenTypeName, "x"),
		p.skipWithLiteral(TokenTypeName, "axis"),
		p.skipWithLiteral(TokenTypeName, "y"),
		p.skipWithLiteral(TokenTypeName, "axis"),
		p.skipWithLiteral(TokenTypePunctuator, ")"),
	)

	return nil
}

func (p *Parser) parseOutputDimensions(output *Output) error {
	p.skipWS = true

	// We probably hit the end of the line here.
	if p.token.Type != TokenTypeName {
		if p.token.Type == TokenTypeLineTerminator {
			err := p.scan()
			if err != nil {
				return err
			}
		}

		return nil
	}

	xdim, err := p.parseOutputDimension()
	if err != nil {
		return err
	}

	err = p.skipWithLiteral(TokenTypeName, "x")
	if err != nil {
		return err
	}

	ydim, err := p.parseOutputDimension()
	if err != nil {
		return err
	}

	output.Dimensions.Width = xdim
	output.Dimensions.Height = ydim

	return nil
}

func (p *Parser) parseOutputDimension() (uint, error) {
	p.skipWS = true

	tok, err := p.expect(TokenTypeName)
	if err != nil {
		return 0, err
	}

	matches := dimensionPattern.FindStringSubmatch(tok.Literal)

	if len(matches) != 2 {
		return 0, err
	}

	dim, err := strconv.ParseUint(matches[1], 10, 64)
	if err != nil {
		return 0, err
	}

	return uint(dim), nil
}

func (p *Parser) parseProperties(output *Output) error {
	// Stop skipping whitespace.
	p.skipWS = false

	for {
		if p.token.Type != TokenTypeWhiteSpace && p.token.Literal != "\t" {
			return nil
		}

		err := p.scan()
		if err != nil {
			return err
		}

		err = p.parseProperty(output)
		if err != nil {
			return err
		}
	}

	return nil
}

func (p *Parser) parseProperty(output *Output) error {
	var name string
	var value string

	tok, err := p.expect(TokenTypeName)
	if err != nil {
		return err
	}

	// Gather up the entire name. Including any spaces, etc. Until we hit a ':'.
	name = tok.Literal

	for {
		if p.token.Type == TokenTypePunctuator && p.token.Literal == ":" {
			break
		}

		name += p.token.Literal

		if err := p.scan(); err != nil {
			return err
		}
	}

	err = p.all(
		p.skipWithLiteral(TokenTypePunctuator, ":"),
		p.skipWithLiteral(TokenTypeWhiteSpace, " "),
	)

	if err != nil {
		return err
	}

	if p.token.Type == TokenTypeLineTerminator {
		for {
			if err := p.scan(); err != nil {
				return err
			}

			// We're no longer processing properties if we've hit something that's not a tab at the
			// start of a new line.
			if p.token.Type != TokenTypeWhiteSpace || p.token.Literal != "\t" {
				break
			}

			if err := p.scan(); err != nil {
				return err
			}

			// If we don't get a second tab, we've hit a new property. So, we need to bail.
			if p.token.Type != TokenTypeWhiteSpace || p.token.Literal != "\t" {
				break
			}

			for {
				if err := p.scan(); err != nil {
					return err
				}

				if p.token.Type == TokenTypeLineTerminator {
					break
				}

				value += p.token.Literal
			}
		}
	} else if p.token.Type == TokenTypeName || p.token.Type == TokenTypeIntValue || p.token.Type == TokenTypeFloatValue {
		value += p.token.Literal

		for {
			if err := p.scan(); err != nil {
				return err
			}

			// Consume the value that's on the same line.
			if p.token.Type == TokenTypeLineTerminator {
				break
			}

			value += p.token.Literal
		}

		// Then, consume everything else after it until we hit another thing that looks like a
		// new property.
		for {
			if err := p.scan(); err != nil {
				return err
			}

			// We're no longer processing properties if we've hit something that's not a tab at the
			// start of a new line.
			if p.token.Type != TokenTypeWhiteSpace || p.token.Literal != "\t" {
				break
			}

			if err := p.scan(); err != nil {
				return err
			}

			// If we don't get a second tab, we've hit a new property. So, we need to bail.
			if p.token.Type != TokenTypeWhiteSpace || p.token.Literal != "\t" {
				break
			}

			// Skip past the "value"
			for {
				if err := p.scan(); err != nil {
					return err
				}

				if p.token.Type == TokenTypeLineTerminator {
					break
				}
			}
		}
	}

	output.Properties[strings.TrimSpace(name)] = strings.TrimSpace(value)

	return nil
}

func (p *Parser) parseModes(output *Output) error {
	p.skipWS = true

	// Sometimes we don't have modes to parse.
	if p.token.Type != TokenTypeWhiteSpace && p.token.Literal != " " {
		return nil
	}

	if err := p.scan(); err != nil {
		return err
	}

	for {
		if p.token.Type != TokenTypeName {
			break
		}

		var mode OutputMode

		isRes, res := p.parseResolution(p.token.Literal)
		if !isRes {
			return fmt.Errorf("expected resolution: got %q", p.token.Literal)
		}

		mode.Resolution = res

		if err := p.scan(); err != nil {
			return err
		}

		for {
			if p.token.Type != TokenTypeFloatValue {
				break
			}

			var rate Rate
			var err error

			rate.Rate, err = strconv.ParseFloat(p.token.Literal, 64)
			if err != nil {
				return err
			}

			if err := p.scan(); err != nil {
				return err
			}

			if p.token.Type == TokenTypePunctuator && p.token.Literal == "*" {
				rate.IsCurrent = true

				if err := p.scan(); err != nil {
					return err
				}
			}

			if p.token.Type == TokenTypePunctuator && p.token.Literal == "+" {
				rate.IsPreferred = true

				if err := p.scan(); err != nil {
					return err
				}
			}

			mode.Rates = append(mode.Rates, rate)
		}

		output.Modes = append(output.Modes, mode)
	}

	return nil
}

func (p *Parser) parseOutput() (Output, error) {
	output := Output{}
	output.Properties = make(map[string]string)
	output.Rotation = RotationNormal
	output.Reflection = ReflectionNone

	err := p.parseOutputName(&output)
	if err != nil {
		return output, fmt.Errorf("error parsing output name: %v", err)
	}

	err = p.parseOutputStatus(&output)
	if err != nil {
		return output, fmt.Errorf("error parsing output status: %v", err)
	}

	err = p.parseResolutionAndPosition(&output)
	if err != nil {
		return output, fmt.Errorf("error parsing output resolution and position: %v", err)
	}

	err = p.parseOutputRotationAndReflection(&output)
	if err != nil {
		return output, fmt.Errorf("error parsing output rotation and reflection: %v", err)
	}

	err = p.parseOutputRotationAndReflectionKey()
	if err != nil {
		return output, fmt.Errorf("error parsing output rotation and reflection key: %v", err)
	}

	err = p.parseOutputDimensions(&output)
	if err != nil {
		return output, fmt.Errorf("error parsing output dimensions: %v", err)
	}

	err = p.parseProperties(&output)
	if err != nil {
		return output, fmt.Errorf("error parsing output properties: %v", err)
	}

	err = p.parseModes(&output)
	if err != nil {
		return output, fmt.Errorf("error parsing output modes: %v", err)
	}

	return output, nil
}

func (p *Parser) parseResolution(literal string) (bool, Resolution) {
	var res Resolution

	matches := resolutionPattern.FindStringSubmatch(literal)

	if len(matches) != 3 {
		return false, res
	}

	xres, err := strconv.ParseUint(matches[1], 10, 64)
	if err != nil {
		return false, res
	}

	yres, err := strconv.ParseUint(matches[2], 10, 64)
	if err != nil {
		return false, res
	}

	res.Width = uint(xres)
	res.Height = uint(yres)

	return true, res
}

func (p *Parser) parseScreen() error {
	// Scan, and skip all expectations here.
	return p.all(
		p.skipWithLiteral(TokenTypeName, "Screen"),
		p.skip(TokenTypeIntValue),
		p.skip(TokenTypePunctuator),
		p.skipWithLiteral(TokenTypeName, "minimum"),
		p.skip(TokenTypeIntValue),
		p.skipWithLiteral(TokenTypeName, "x"),
		p.skip(TokenTypeIntValue),
		p.skip(TokenTypePunctuator),
		p.skipWithLiteral(TokenTypeName, "current"),
		p.skip(TokenTypeIntValue),
		p.skipWithLiteral(TokenTypeName, "x"),
		p.skip(TokenTypeIntValue),
		p.skip(TokenTypePunctuator),
		p.skipWithLiteral(TokenTypeName, "maximum"),
		p.skip(TokenTypeIntValue),
		p.skipWithLiteral(TokenTypeName, "x"),
		p.skip(TokenTypeIntValue),
		p.skip(TokenTypeLineTerminator),
	)
}

// Parser utilities:

func (p *Parser) all(errs ...error) error {
	for _, err := range errs {
		if err != nil {
			return err
		}
	}

	return nil
}

func (p *Parser) expect(t TokenType) (Token, error) {
	token := p.token
	match, err := p.match(t)
	if err != nil {
		return token, err
	}

	if match {
		return token, nil
	}

	return token, fmt.Errorf(
		"syntax error: unexpected token found: %s (%q). Wanted: %s. Line: %d. Column: %d",
		p.token.Type.String(),
		p.token.Literal,
		t.String(),
		p.token.Line,
		p.token.Position,
	)
}

// skip reads the next token, then verifies that it matches the given type expectation. If it
// doesn't, then an error will be returned. If scanning fails, an error will be returned.
func (p *Parser) skip(t TokenType) error {
	token := p.token

	match, err := p.match(t)
	if err != nil {
		return err
	}

	if match {
		return nil
	}

	return fmt.Errorf(
		"syntax error: unexpected token found: %s (%q). Wanted: %s. Line: %d. Column: %d",
		token.Type.String(),
		token.Literal,
		t.String(),
		token.Line,
		token.Position,
	)
}

// skipWithLiteral reads the next token, then verifies that it matches the given type and string
// literal expectations. If it doesn't, then an error will be returned. If scanning fails, an error
// will be returned.
func (p *Parser) skipWithLiteral(t TokenType, l string) error {
	token := p.token

	err := p.skip(t)
	if err != nil {
		return err
	}

	if token.Literal != l {
		return fmt.Errorf(
			"syntax error: unexpected literal %q found for token type %q. Line: %d. Column: %d",
			l,
			t.String(),
			token.Line,
			token.Position,
		)
	}

	return nil
}

func (p *Parser) match(t TokenType) (bool, error) {
	var err error
	match := p.token.Type == t
	if match {
		err = p.scan()
	}

	return match, err
}

func (p *Parser) scan() (err error) {
	p.token, err = p.lexer.Scan()
	if err != nil {
		return err
	}

	if p.skipWS && p.token.Type == TokenTypeWhiteSpace {
		return p.scan()
	}

	return nil
}

// check current, read next, return current = expect
// check current, read next = skip
// check current = p.token

// Maybe what we really need is two variants of scan:
// - One for scanning any token.
// - One for scanning any token other than whitespace.
//
// Both functions should get the next token, but also keep the last. Maybe even when scan is called,
// then the previous token is returned or something.

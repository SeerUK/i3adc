package xrandr

import (
	"fmt"
	"regexp"
	"strconv"
	"strings"
)

var (
	dimensionPattern  = regexp.MustCompile(`^([0-9]+)mm$`)
	resolutionPattern = regexp.MustCompile(`^([0-9]+)x([0-9]+)i?$`)
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
	p.scan()

	// "Parse" the screen, we actually just skip it entirely really.
	err := p.parseScreen()
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

	tok, err := p.consume(TokenTypeName)
	if err != nil {
		return err
	}

	output.Name = tok.Literal

	return nil
}

func (p *Parser) parseOutputStatus(output *Output) error {
	p.skipWS = true

	tok, err := p.consume(TokenTypeName, "connected", "disconnected")
	if err == nil {
		output.IsConnected = tok.Literal == "connected"
	}

	if p.skip(TokenTypeName, "primary") {
		output.IsPrimary = true
	}

	return nil
}

func (p *Parser) parseResolutionAndPosition(output *Output) error {
	p.skipWS = true

	// If the output is enabled, we should see the current resolution, and the position.
	if p.next(TokenTypeName) {
		isRes, res := p.parseResolution(p.token.Literal)
		if !isRes {
			return nil
		}

		output.IsEnabled = true
		output.Resolution = res

		p.scan()

		if err := p.expect(TokenTypePunctuator, "+"); err != nil {
			return err
		}

		tok, err := p.consume(TokenTypeIntValue)
		if err != nil {
			return err
		}

		offsetX, err := strconv.ParseInt(tok.Literal, 10, 64)
		if err != nil {
			return err
		}

		if err := p.expect(TokenTypePunctuator, "+"); err != nil {
			return err
		}

		tok, err = p.consume(TokenTypeIntValue)
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

	// We can ignore this error, we might not have any rotation status.
	if tok, err := p.consume(TokenTypeName, "normal", "left", "inverted", "right"); err == nil {
		switch tok.Literal {
		case "normal":
			output.Rotation = RotationNormal
		case "left":
			output.Rotation = RotationLeft
		case "inverted":
			output.Rotation = RotationInverted
		case "right":
			output.Rotation = RotationRight
		}

		p.scan()
	}

	if tok, err := p.consume(TokenTypeName, "x", "y"); err == nil {
		// If we get 'x' or 'y' we always expect the word 'axis' to follow.
		if err := p.expect(TokenTypeName, "axis"); err != nil {
			return err
		}

		switch tok.Literal {
		case "x":
			output.Reflection = ReflectionXAxis
		case "y":
			output.Reflection = ReflectionYAxis
		}
	}

	return nil
}

func (p *Parser) parseOutputRotationAndReflectionKey() error {
	p.skipWS = true

	if p.token.Type != TokenTypePunctuator && p.token.Literal == "(" {
		return nil
	}

	return p.expectAll(
		p.expectFn(TokenTypePunctuator, "("),
		p.expectFn(TokenTypeName, "normal"),
		p.expectFn(TokenTypeName, "left"),
		p.expectFn(TokenTypeName, "inverted"),
		p.expectFn(TokenTypeName, "right"),
		p.expectFn(TokenTypeName, "x"),
		p.expectFn(TokenTypeName, "axis"),
		p.expectFn(TokenTypeName, "y"),
		p.expectFn(TokenTypeName, "axis"),
		p.expectFn(TokenTypePunctuator, ")"),
	)

	return nil
}

func (p *Parser) parseOutputDimensions(output *Output) error {
	p.skipWS = true

	// We probably hit the end of the line here.
	if !p.next(TokenTypeName) {
		if p.skip(TokenTypeLineTerminator) {
			// We _might_ hit properties next, so we have to do this in advance.
			p.skipWS = false
		}

		return nil
	}

	xdim, err := p.parseOutputDimension()
	if err != nil {
		return err
	}

	if err = p.expect(TokenTypeName, "x"); err != nil {
		return err
	}

	ydim, err := p.parseOutputDimension()
	if err != nil {
		return err
	}

	output.Dimensions.Width = xdim
	output.Dimensions.Height = ydim

	if p.skip(TokenTypeLineTerminator) {
		p.skipWS = false
	}

	return nil
}

func (p *Parser) parseOutputDimension() (uint, error) {
	p.skipWS = true

	tok, err := p.consume(TokenTypeName)
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

	if err := p.expect(TokenTypeWhiteSpace, "\t"); err != nil {
		return err
	}

	for {
		stop, err := p.parseProperty(output)
		if err != nil {
			return err
		}

		if stop {
			break
		}

		if p.token.Type != TokenTypeName {
			break
		}
	}

	return nil
}

func (p *Parser) parseProperty(output *Output) (bool, error) {
	var name string
	var value string
	var stop bool

	tok, err := p.consume(TokenTypeName)
	if err != nil {
		return stop, err
	}

	// Gather up the entire name. Including any spaces, etc. Until we hit a ':'.
	name = tok.Literal

	for {
		if p.token.Type == TokenTypePunctuator && p.token.Literal == ":" {
			break
		}

		name += p.token.Literal

		p.scan()
	}

	err = p.expectAll(
		p.expectFn(TokenTypePunctuator, ":"),
		p.expectFn(TokenTypeWhiteSpace, " "),
	)

	if err != nil {
		return stop, err
	}

	if p.token.Type == TokenTypeLineTerminator {
		for {
			p.scan()

			// We're no longer processing properties if we've hit something that's not a tab at the
			// start of a new line.
			if p.token.Type != TokenTypeWhiteSpace || p.token.Literal != "\t" {
				stop = true
				break
			}

			p.scan()

			// If we don't get a second tab, we've hit a new property. So, we need to bail from this
			// loop iteration.
			if p.token.Type != TokenTypeWhiteSpace || p.token.Literal != "\t" {
				break
			}

			for {
				p.scan()

				if p.token.Type == TokenTypeLineTerminator {
					break
				}

				value += p.token.Literal
			}
		}
	} else if p.token.Type == TokenTypeName || p.token.Type == TokenTypeIntValue || p.token.Type == TokenTypeFloatValue {
		value += p.token.Literal

		for {
			p.scan()

			// Consume the value that's on the same line.
			if p.token.Type == TokenTypeLineTerminator {
				break
			}

			value += p.token.Literal
		}

		// Then, consume everything else after it until we hit another thing that looks like a
		// new property.
		for {
			p.scan()

			// We're no longer processing properties if we've hit something that's not a tab at the
			// start of a new line.
			if p.token.Type != TokenTypeWhiteSpace || p.token.Literal != "\t" {
				stop = true
				break
			}

			p.scan()

			// If we don't get a second tab, we've hit a new property. So, we need to bail.
			if p.token.Type != TokenTypeWhiteSpace || p.token.Literal != "\t" {
				break
			}

			// Skip past the "value"
			for {
				p.scan()

				if p.token.Type == TokenTypeLineTerminator {
					break
				}
			}
		}
	}

	output.Properties[strings.TrimSpace(name)] = strings.TrimSpace(value)

	return stop, nil
}

func (p *Parser) parseModes(output *Output) error {
	p.skipWS = true

	// Sometimes we don't have modes to parse.
	if p.token.Type != TokenTypeWhiteSpace && p.token.Literal != " " {
		return nil
	}

	p.scan()

	for {
		if p.token.Type != TokenTypeName {
			break
		}

		var mode OutputMode

		isRes, res := p.parseResolution(p.token.Literal)
		if !isRes {
			// We've probably just hit the end of resolutions at this point, and are looking at the
			// next output.
			return nil
		}

		mode.Resolution = res
		p.scan()

		for {
			if !p.next(TokenTypeFloatValue) {
				break
			}

			var rate Rate
			var err error

			rate.Rate, err = strconv.ParseFloat(p.token.Literal, 64)
			if err != nil {
				return err
			}

			p.scan()

			if p.skip(TokenTypePunctuator, "*") {
				rate.IsCurrent = true
			}

			if p.skip(TokenTypePunctuator, "+") {
				rate.IsPreferred = true
			}

			mode.Rates = append(mode.Rates, rate)
		}

		_ = p.skip(TokenTypeLineTerminator)

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
	return p.expectAll(
		p.expectFn(TokenTypeName, "Screen"),
		p.expectFn(TokenTypeIntValue),
		p.expectFn(TokenTypePunctuator),
		p.expectFn(TokenTypeName, "minimum"),
		p.expectFn(TokenTypeIntValue),
		p.expectFn(TokenTypeName, "x"),
		p.expectFn(TokenTypeIntValue),
		p.expectFn(TokenTypePunctuator),
		p.expectFn(TokenTypeName, "current"),
		p.expectFn(TokenTypeIntValue),
		p.expectFn(TokenTypeName, "x"),
		p.expectFn(TokenTypeIntValue),
		p.expectFn(TokenTypePunctuator),
		p.expectFn(TokenTypeName, "maximum"),
		p.expectFn(TokenTypeIntValue),
		p.expectFn(TokenTypeName, "x"),
		p.expectFn(TokenTypeIntValue),
		p.expectFn(TokenTypeLineTerminator),
	)
}

// Parser utilities:

func (p *Parser) expectAll(fns ...func() error) error {
	for _, fn := range fns {
		if err := fn(); err != nil {
			return err
		}
	}

	return nil
}

func (p *Parser) consume(t TokenType, ls ...string) (Token, error) {
	tok := p.token
	if tok.Type != t {
		return tok, p.unexpected(tok, t, ls...)
	}

	if len(ls) == 0 {
		p.scan()
		return tok, nil
	}

	for _, l := range ls {
		if tok.Literal != l {
			continue
		}

		p.scan()
		return tok, nil
	}

	return tok, p.unexpected(tok, t, ls...)
}

func (p *Parser) expect(t TokenType, ls ...string) error {
	if !p.next(t, ls...) {
		return p.unexpected(p.token, t, ls...)
	}

	return nil
}

func (p *Parser) expectFn(t TokenType, ls ...string) func() error {
	return func() error {
		return p.expect(t, ls...)
	}
}

func (p *Parser) next(t TokenType, ls ...string) bool {
	if p.token.Type != t {
		return false
	}

	if len(ls) == 0 {
		return true
	}

	for _, l := range ls {
		if p.token.Literal == l {
			return true
		}
	}

	return false
}

func (p *Parser) skip(t TokenType, ls ...string) bool {
	_, err := p.consume(t, ls...)
	if err != nil {
		return false
	}

	return true
}

func (p *Parser) scan() {
	p.token = p.lexer.Scan()
}

func (p *Parser) unexpected(token Token, t TokenType, ls ...string) error {
	if len(ls) == 0 {
		ls = []string{"N/A"}
	}

	return fmt.Errorf(
		"parser error: unexpected token found: %s (%q). Wanted: %s (%q). Line: %d. Column: %d",
		token.Type.String(),
		token.Literal,
		t.String(),
		strings.Join(ls, "|"),
		token.Line,
		token.Position,
	)
}

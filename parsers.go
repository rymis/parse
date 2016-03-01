package parse

import (
	"bytes"
	"errors"
	"fmt"
	"io"
	"reflect"
	"regexp"
	"strconv"
	"unicode/utf8"
)

// Actual parsers for Go types. Parsers for Struct, Slice and Ptr are placed in compile.go.

// Parser representation.
type parser interface {
	// Set identifier (for fast packrat getting/setting)
	SetID(id uint)
	// Get identifier that has set before
	ID() uint
	// Get string representation of the parser
	String() string
	// Set string representation of the parser
	SetString(nm string)

	// Parse function.
	ParseValue(ctx *parseContext, valueOf reflect.Value, location int, err *Error) int

	// Write value to output stream.
	WriteValue(out io.Writer, valueOf reflect.Value) error

	// Check if this parser parses terminal symbol (doesn't contain sub-parsers)
	IsTerm() bool

	// Check possibility of left recursion.
	// This function must add all parsers with offset 0 to the set of parsers and
	// return two values: is left recursion possible (and if possible execution will be stopped)
	// and could parser parse empty value.
	IsLRPossible(parsers []parser) (possible bool, canParseEmpty bool)

	// Check if rule left recursive:
	IsLR() int
	// Set LR state:
	SetLR(v int)
}

// Utility function to call IsLRPossible
func isLRPossible(p parser, parsers []parser) (possible bool, canParseEmpty bool) {
	lr := p.IsLR()
	if lr < 0 {
		possible = true
		return
	} else if lr > 0 {
		possible = false
		return
	}

	for _, t := range parsers {
		if t.ID() == p.ID() {
			p.SetLR(-1)
			possible = true
			return
		}
	}

	possible, canParseEmpty = p.IsLRPossible(append(parsers, p))
	if possible {
		p.SetLR(-1)
	} else {
		p.SetLR(1)
	}

	return
}

// Type that implements first 4 methods for all parsers
type idHolder struct {
	id   uint
	name string
	lr   int
}

func (par *idHolder) SetID(id uint) {
	par.id = id
}

func (par *idHolder) ID() uint {
	return par.id
}

func (par *idHolder) String() string {
	return par.name
}

func (par *idHolder) SetString(nm string) {
	par.name = nm
}

func (par *idHolder) IsLR() int {
	return par.lr
}

func (par *idHolder) SetLR(v int) {
	par.lr = v
}

type terminal struct {
	terminal bool
}

type nonTerminal struct {
	terminal bool
}

func (par *terminal) IsTerm() bool {
	return true
}

func (par *nonTerminal) IsTerm() bool {
	return false
}

// Parser for Go-like boolean values.
// Value is 'true' or 'false' with following character from [^a-zA-Z0-9_]
type boolParser struct {
	idHolder
	terminal
}

var boolError = "Waiting for boolean value"

func (par *boolParser) ParseValue(ctx *parseContext, valueOf reflect.Value, location int, err *Error) int {
	if strAt(ctx.str, location, "true") {
		valueOf.SetBool(true)
		location += 4
	} else if strAt(ctx.str, location, "false") {
		valueOf.SetBool(false)
		location += 5
	} else {
		err.Location = location
		err.Message = boolError
		return -1
	}

	if location < len(ctx.str) {
		if ctx.str[location] == '_' ||
			(ctx.str[location] >= 'a' && ctx.str[location] <= 'z') ||
			(ctx.str[location] >= 'A' && ctx.str[location] <= 'Z') ||
			(ctx.str[location] >= '0' && ctx.str[location] <= '9') {
			err.Location = location
			err.Message = boolError
			return -1
		}
	}

	return location
}

func (par *boolParser) WriteValue(out io.Writer, valueOf reflect.Value) error {
	var err error
	if valueOf.Bool() {
		_, err = out.Write([]byte("true"))
	} else {
		_, err = out.Write([]byte("false"))
	}

	return err
}

func (par *boolParser) IsLRPossible(parsers []parser) (possible bool, canParseEmpty bool) {
	return false, false
}

// Parse string matched with regular expression.
type regexpParser struct {
	idHolder
	terminal
	Regexp *regexp.Regexp
	err    string
}

func (par *regexpParser) ParseValue(ctx *parseContext, valueOf reflect.Value, location int, err *Error) int {
	m := par.Regexp.Find(ctx.str[location:])
	if m == nil {
		err.Location = location
		err.Message = par.err
		return -1
	}

	valueOf.SetString(string(m))

	return location + len(m)
}

func (par *regexpParser) WriteValue(out io.Writer, valueOf reflect.Value) error {
	s := valueOf.String()
	if par.Regexp.MatchString(s) {
		_, err := out.Write([]byte(s))
		return err
	}

	return fmt.Errorf("String `%s' does not match regular expression %v", s, par.Regexp)
}

func (par *regexpParser) IsLRPossible(parsers []parser) (possible bool, canParseEmpty bool) {
	return false, par.Regexp.MatchString("")
}

func newRegexpParser(rx string) (parser, error) {
	r, err := regexp.Compile("^" + rx)
	if err != nil {
		return nil, err
	}

	msg := fmt.Sprintf("Waiting for /%s/", rx)

	return &regexpParser{Regexp: r, err: msg}, nil
}

// Go string parser.
type stringParser struct {
	idHolder
	terminal
}

// Parse Go unicode value:
func (ctx *parseContext) parseUnicodeValue(location int, err *Error) (rune, int) {
	/*
		unicode_value    = unicode_char | little_u_value | big_u_value | escaped_char .
		byte_value       = octal_byte_value | hex_byte_value .
		octal_byte_value = `\` octal_digit octal_digit octal_digit .
		hex_byte_value   = `\` "x" hex_digit hex_digit .
		little_u_value   = `\` "u" hex_digit hex_digit hex_digit hex_digit .
		big_u_value      = `\` "U" hex_digit hex_digit hex_digit hex_digit
		                           hex_digit hex_digit hex_digit hex_digit .
					   escaped_char     = `\` ( "a" | "b" | "f" | "n" | "r" | "t" | "v" | `\` | "'" | `"` ) .
	*/
	if location >= len(ctx.str) {
		err.Location = location
		err.Message = "Unexpected end of file: waiting for Unicode character"
		return 0, -1
	}

	if ctx.str[location] == '\\' {
		location++
		if location >= len(ctx.str) {
			err.Location = location
			err.Message = "Unexpected end of file in escape sequence"
			return 0, -1
		}

		if ctx.str[location] == '\\' {
			return '\\', location + 1
		} else if ctx.str[location] == 'a' {
			return '\a', location + 1
		} else if ctx.str[location] == 'b' {
			return '\b', location + 1
		} else if ctx.str[location] == 'f' {
			return '\f', location + 1
		} else if ctx.str[location] == 'n' {
			return '\n', location + 1
		} else if ctx.str[location] == 'r' {
			return '\r', location + 1
		} else if ctx.str[location] == 't' {
			return '\t', location + 1
		} else if ctx.str[location] == 'v' {
			return '\v', location + 1
		} else if ctx.str[location] == '`' {
			return '`', location + 1
		} else if ctx.str[location] == '\'' {
			return '\'', location + 1
		} else if ctx.str[location] == '"' {
			return '"', location + 1
		} else if ctx.str[location] >= '0' && ctx.str[location] < 3 {
			if location+2 >= len(ctx.str) {
				err.Location = location
				err.Message = "Unexpected end of file in escape sequence"
				return 0, -1
			}

			var r rune
			for i := 0; i < 3; i++ {
				if ctx.str[location+i] >= '0' && ctx.str[location+i] <= '7' {
					r = r*8 + rune(ctx.str[location+i]-'0')
				} else {
					err.Location = location
					err.Message = "Invalid character in octal_byte"
					return 0, -1
				}
			}

			return r, location + 3

		} else if ctx.str[location] == 'x' || ctx.str[location] == 'u' || ctx.str[location] == 'U' {
			var l int
			if ctx.str[location] == 'x' {
				l = 2
			} else if ctx.str[location] == 'u' {
				l = 4
			} else {
				l = 8
			}

			if location+l >= len(ctx.str) {
				err.Location = location
				err.Message = "Unexpected end of file in escape sequence"
				return 0, -1
			}

			location++

			var r rune
			for i := 0; i < l; i++ {
				if ctx.str[location+i] >= '0' && ctx.str[location+i] <= '9' {
					r = r*16 + rune(ctx.str[location+i]-'0')
				} else if ctx.str[location+i] >= 'a' && ctx.str[location+i] <= 'f' {
					r = r*16 + rune(ctx.str[location+i]-'a'+10)
				} else if ctx.str[location+i] >= 'A' && ctx.str[location+i] <= 'F' {
					r = r*16 + rune(ctx.str[location+i]-'A'+10)
				} else {
					err.Location = location
					err.Message = "Illegal character in hex code"
					return 0, -1
				}
			}

			if !utf8.ValidRune(r) {
				err.Location = location
				err.Message = "Invalid rune"
				return 0, -1
			}

			return r, location + l
		}

		err.Location = location
		err.Message = "Invalid escaped char"
		return 0, -1
	}

	r, l := utf8.DecodeRune(ctx.str[location:])
	if l <= 0 {
		err.Location = location
		err.Message = "Invalid Unicode character"
		return 0, -1
	}

	return r, location + l
}

// Parse Go string and return processed string:
func (ctx *parseContext) parseString(location int, err *Error) (string, int) {
	buf := bytes.NewBuffer(nil)
	/* Grammar:
	string_lit             = raw_string_lit | interpreted_string_lit .
	raw_string_lit         = "`" { unicode_char | newline } "`" .
	interpreted_string_lit = `"` { unicode_value | byte_value } `"` .

	rune_lit         = "'" ( unicode_value | byte_value ) "'" .

	*/

	if ctx.str[location] == '`' { // raw string
		for location++; location < len(ctx.str); {
			if ctx.str[location] == '`' { // End of string
				return buf.String(), location + 1
			} else if ctx.str[location] == '\r' { // Skip it
				location++
			} else {
				buf.WriteByte(ctx.str[location])
				location++
			}
		}
	} else if ctx.str[location] == '"' { // interpreted string
		for location++; location < len(ctx.str); {
			if ctx.str[location] == '"' {
				return buf.String(), location + 1
			}

			r, l := ctx.parseUnicodeValue(location, err)
			if l < 0 {
				return "", l
			}

			if r >= 0x80 && r <= 0xff && l-location == 4 { // TODO: make it better
				buf.WriteByte(byte(r))
			} else {
				_, e := buf.WriteRune(r)
				if e != nil {
					err.Message = fmt.Sprintf("Invalid Rune: %s", err.Error())
					err.Location = location
					return "", -1
				}
			}

			location = l
		}
	}

	err.Message = "Waiting for Go string"
	err.Location = location
	return "", -1
}

func (par *stringParser) ParseValue(ctx *parseContext, valueOf reflect.Value, location int, err *Error) int {
	s, nl := ctx.parseString(location, err)
	if nl < 0 {
		return nl
	}

	valueOf.SetString(s)

	return nl
}

func (par *stringParser) WriteValue(out io.Writer, valueOf reflect.Value) error {
	_, err := out.Write(strconv.AppendQuote(nil, valueOf.String()))
	return err
}

func (par *stringParser) IsLRPossible(parsers []parser) (possible bool, canParseEmpty bool) {
	return false, false
}

// Parse specified literal:
type literalParser struct {
	idHolder
	terminal
	Literal string
	msg     string
}

func (par *literalParser) ParseValue(ctx *parseContext, valueOf reflect.Value, location int, err *Error) int {
	if strAt(ctx.str, location, par.Literal) {
		valueOf.SetString(par.Literal)
		return location + len(par.Literal)
	}

	err.Message = par.msg
	err.Location = location
	return -1
}

func (par *literalParser) WriteValue(out io.Writer, valueOf reflect.Value) error {
	_, err := out.Write([]byte(par.Literal))
	return err
}

func (par *literalParser) IsLRPossible(parsers []parser) (possible bool, canParseEmpty bool) {
	return false, len(par.Literal) == 0
}

func newLiteralParser(lit string) parser {
	msg := fmt.Sprintf("Waiting for '%s'", lit)
	return &literalParser{Literal: lit, msg: msg}
}

// Check if there was overflow for <size> bits type
func (ctx *parseContext) checkUintOverflow(v uint64, location int, size uint) bool {
	if size >= 64 {
		return false
	}

	if (v >> size) != 0 {
		return true
	}

	return false
}

// Parse uint value and save it in uint64.
// size is value size in bits.
func (ctx *parseContext) parseUint64(location int, size uint, err *Error) (uint64, int) {
	if location >= len(ctx.str) {
		err.Message = "Unexpected end of file. Waiting for integer literal."
		err.Location = location
		return 0, -1
	}

	var res uint64
	if ctx.str[location] == '0' {
		if location+1 < len(ctx.str) && (ctx.str[location+1] == 'x' || ctx.str[location+1] == 'X') { // HEX
			location += 2

			if location >= len(ctx.str) {
				err.Message = "Unexpected end of file in hexadecimal literal."
				err.Location = location
				return 0, -1
			}

			for ; location < len(ctx.str); location++ {
				if (res & 0xf000000000000000) != 0 {
					err.Message = "Integer overflow"
					err.Location = location
					return 0, -1
				}

				if (ctx.str[location] >= '0') && (ctx.str[location] <= '9') {
					res = (res << 4) + uint64(ctx.str[location]-'0')
				} else if (ctx.str[location] >= 'a') && (ctx.str[location] <= 'f') {
					res = (res << 4) + uint64(ctx.str[location]-'a') + 10
				} else if (ctx.str[location] >= 'A') && (ctx.str[location] <= 'F') {
					res = (res << 4) + uint64(ctx.str[location]-'A') + 10
				} else {
					break
				}
			}

			if ctx.checkUintOverflow(res, location, size) {
				err.Message = "Integer overflow"
				err.Location = location
				return 0, -1
			}

			return res, location
		}

		// OCT
		for ; location < len(ctx.str); location++ {
			if (res & 0xe000000000000000) != 0 {
				err.Message = "Integer overflow"
				err.Location = location
				return 0, -1
			}

			if ctx.str[location] >= '0' && ctx.str[location] <= '7' {
				res = (res << 3) + uint64(ctx.str[location]-'0')
			} else {
				break
			}
		}

		if ctx.checkUintOverflow(res, location, size) {
			err.Message = "Integer overflow"
			err.Location = location
			return 0, -1
		}

		return res, location
	} else if ctx.str[location] > '0' && ctx.str[location] <= '9' {
		var r8 uint64
		for ; location < len(ctx.str); location++ {
			if (res & 0xe000000000000000) != 0 {
				err.Message = "Integer overflow"
				err.Location = location
				return 0, -1
			}

			if ctx.str[location] >= '0' && ctx.str[location] <= '9' {
				r8 = res << 3 // r8 = res * 8 Here could not be overflow: we have checked this before
				res = r8 + (res << 1)
				if res < r8 { // Overflow!
					err.Message = "Integer overflow"
					err.Location = location
					return 0, location
				}

				res += uint64(ctx.str[location] - '0')
			} else {
				break
			}
		}

		if ctx.checkUintOverflow(res, location, size) {
			err.Message = "Integer overflow"
			err.Location = location
			return 0, -1
		}

		return res, location
	}

	err.Message = "Waiting for integer literal"
	err.Location = location
	return 0, -1
}

// Parse int value and save it in int64.
// size is value size in bits.
func (ctx *parseContext) parseInt64(location int, size uint, err *Error) (int64, int) {
	neg := false
	if location >= len(ctx.str) {
		err.Message = "Unexpected end of file. Waiting for integer."
		return 0, -1
	}

	if ctx.str[location] == '-' {
		neg = true
		location++

		/* TODO: allow spaces after '-'??? */
	}

	v, l := ctx.parseUint64(location, size, err)
	if l < 0 {
		return 0, l
	}

	if (v & 0x8000000000000000) != 0 {
		err.Message = "Integer overflow"
		err.Location = location
		return 0, location
	}

	res := int64(v)
	if neg {
		res = -res
	}

	return res, l
}

var floatRegexp = regexp.MustCompile(`^[-+]?([0-9]+(\.[0-9]+)?|\.[0-9]+)([eE][-+]?[0-9]+)?`)

func (ctx *parseContext) parseFloat(location int, size int, err *Error) (float64, int) {
	m := floatRegexp.Find(ctx.str[location:])

	if m == nil {
		err.Message = "Waiting for floating point number"
		err.Location = location
		return 0.0, -1
	}

	r, e := strconv.ParseFloat(string(m), size)
	if e != nil {
		err.Message = "Invalid floating point number"
		err.Location = location
		return 0.0, -1
	}

	return r, location + len(m)
}

type intParser struct {
	idHolder
	terminal
}

type uintParser struct {
	idHolder
	terminal
}

type floatParser struct {
	idHolder
	terminal
}

func (par *intParser) ParseValue(ctx *parseContext, valueOf reflect.Value, location int, err *Error) int {
	if valueOf.Type().Bits() == 32 && location < len(ctx.str) && ctx.str[location] == '\'' {
		location++
		r, location := ctx.parseUnicodeValue(location, err)
		if location < 0 {
			return location
		}

		if location >= len(ctx.str) || ctx.str[location] != '\'' {
			err.Message = "Waiting for closing quote in unicode character"
			err.Location = location
			return -1
		}
		location++

		valueOf.SetInt(int64(r))

		return location
	}

	r, l := ctx.parseInt64(location, uint(valueOf.Type().Bits()), err)
	if l < 0 {
		return l
	}

	valueOf.SetInt(r)
	return l
}

func (par *intParser) WriteValue(out io.Writer, valueOf reflect.Value) error {
	_, err := out.Write(strconv.AppendInt(nil, valueOf.Int(), 10))
	return err
}

func (par *intParser) IsLRPossible(parsers []parser) (possible bool, canParseEmpty bool) {
	return false, false
}

func (par *uintParser) ParseValue(ctx *parseContext, valueOf reflect.Value, location int, err *Error) int {
	r, l := ctx.parseUint64(location, uint(valueOf.Type().Bits()), err)
	if l < 0 {
		return l
	}

	valueOf.SetUint(r)
	return l
}

func (par *uintParser) WriteValue(out io.Writer, valueOf reflect.Value) error {
	_, err := out.Write(strconv.AppendUint(nil, valueOf.Uint(), 10))
	return err
}

func (par *uintParser) IsLRPossible(parsers []parser) (possible bool, canParseEmpty bool) {
	return false, false
}

func (par *floatParser) ParseValue(ctx *parseContext, valueOf reflect.Value, location int, err *Error) int {
	r, l := ctx.parseFloat(location, valueOf.Type().Bits(), err)
	if l < 0 {
		return l
	}

	valueOf.SetFloat(r)
	return l
}

func (par *floatParser) WriteValue(out io.Writer, valueOf reflect.Value) error {
	_, err := out.Write(strconv.AppendFloat(nil, valueOf.Float(), 'e', -1, valueOf.Type().Bits()))
	return err
}

func (par *floatParser) IsLRPossible(parsers []parser) (possible bool, canParseEmpty bool) {
	return false, false
}

// This parser only saves location
type locationParser struct {
	idHolder
	terminal
}

func (par *locationParser) ParseValue(ctx *parseContext, valueOf reflect.Value, location int, err *Error) int {
	valueOf.SetInt(int64(location))
	return location
}

func (par *locationParser) WriteValue(out io.Writer, valueOf reflect.Value) error {
	return nil
}

func (par *locationParser) IsLRPossible(parsers []parser) (possible bool, canParseEmpty bool) {
	return false, true
}

type field struct {
	Name  string
	Index int
	Parse parser
	Flags uint
	Set   string
	Type  reflect.Type
}

func (par field) ParseValue(ctx *parseContext, valueOf reflect.Value, location int, err *Error) int {
	var f reflect.Value
	var l int

	if par.Index < 0 {
		f = reflect.New(par.Type).Elem()
	} else {
		f = valueOf.Field(par.Index)
	}

	if !f.CanSet() {
		panic(fmt.Sprintf("Can't set field '%v.%s'", valueOf.Type(), par.Name))
	}

	l = ctx.parse(f, par.Parse, location, err)
	if (par.Flags & fieldNotAny) != 0 {
		if l >= 0 {
			err.Message = fmt.Sprintf("Unexpected input: %v", par.Parse)
			err.Location = location
			return -1
		}

		// Don't change the location
		return location
	} else if (par.Flags & fieldFollowedBy) != 0 {
		if l < 0 {
			return l
		}
		// Don't change the location
		return location
	} else {
		if l < 0 {
			return l
		}

		if par.Set != "" {
			method := valueOf.MethodByName(par.Set)
			if !method.IsValid() && valueOf.CanAddr() {
				method = valueOf.Addr().MethodByName(par.Set)
			}

			if !method.IsValid() {
				panic(fmt.Sprintf("Can't find `%s' method", par.Set))
			}

			mtp := method.Type()
			if mtp.NumIn() != 1 || mtp.NumOut() != 1 || mtp.In(0) != f.Type() || mtp.Out(0).Name() != "error" {
				panic(fmt.Sprintf("Invalid method `%s' signature. Waiting for func (%v) error", par.Set, par.Parse))
			}

			resv := method.Call([]reflect.Value{f})[0]
			if !resv.IsNil() {
				err.Message = fmt.Sprintf("Set failed: %v", resv.Interface())
				err.Location = l
				return -l
			}
		}

		return ctx.skipWS(l)
	}
}

func (par field) IsLRPossible(parsers []parser) (possible bool, canParseEmpty bool) {
	possible, canParseEmpty = isLRPossible(par.Parse, parsers)
	if possible {
		return
	}

	if (par.Flags & (fieldNotAny | fieldFollowedBy)) != 0 {
		canParseEmpty = true
	}

	return
}

func (par field) WriteValue(out io.Writer, valueOf reflect.Value) error {
	if (par.Flags & (fieldNotAny | fieldFollowedBy)) != 0 {
		return nil
	}

	if par.Index < 0 { // We can not out this value in all cases but if it was literal we can do it
		// TODO: Check if it is string and output only in case it is literal
		p := par.Parse
		v := valueOf
		for {
			switch tp := p.(type) {
			case *ptrParser:
				p = tp.Parser
				if v.IsNil() {
					if tp.Optional {
						return nil
					}
					return errors.New("Ptr value is nil")
				}
				v = v.Elem()
				break

			case *literalParser:
				_, err := out.Write([]byte(tp.Literal))
				return err
			default:
				return errors.New("Could not out anonymous field if it is not literal")
			}
		}
	} else {
		f := valueOf.Field(par.Index)
		return par.Parse.WriteValue(out, f)
	}
}

const (
	fieldNotAny     uint = 1
	fieldFollowedBy uint = 2
)

type sequenceParser struct {
	idHolder
	nonTerminal
	Fields []field
}

func (par *sequenceParser) ParseValue(ctx *parseContext, valueOf reflect.Value, location int, err *Error) int {
	for _, f := range par.Fields {
		location = f.ParseValue(ctx, valueOf, location, err)
		if location < 0 {
			return location
		}
	}

	return location
}

func (par *sequenceParser) WriteValue(out io.Writer, valueOf reflect.Value) error {
	var err error
	for _, f := range par.Fields {
		err = f.WriteValue(out, valueOf)
		if err != nil {
			return err
		}
	}
	return nil
}

func (par *sequenceParser) IsLRPossible(parsers []parser) (possible bool, canParseEmpty bool) {
	for _, f := range par.Fields {
		p, can := f.IsLRPossible(parsers)
		if p {
			// Recursion has been found:
			return p, can
		}

		// There could not be left recursion: can not parse empty prefix.
		if !can {
			return false, false
		}
	}

	// Here could not be recusive call but structure could parse emptry string
	return false, true
}

type firstOfParser struct {
	idHolder
	nonTerminal
	Fields []field
}

func (par *firstOfParser) ParseValue(ctx *parseContext, valueOf reflect.Value, location int, err *Error) int {
	maxError := Error{ctx.str, location - 1, "No choices in first of"}
	var l int

	for _, f := range par.Fields {
		l = f.ParseValue(ctx, valueOf, location, err)
		if l >= 0 {
			valueOf.FieldByName("FirstOf").FieldByName("Field").SetString(f.Name)
			return l
		}

		if err.Location > maxError.Location {
			maxError.Location = err.Location
			maxError.Str = err.Str
			maxError.Message = err.Message
		}
	}

	err.Message = maxError.Message
	err.Location = maxError.Location
	return -1
}

func (par *firstOfParser) WriteValue(out io.Writer, valueOf reflect.Value) error {
	var err error
	nm := valueOf.Field(0).Field(0).String()

	if nm == "" {
		return errors.New("Field is not selected in FirstOf")
	}

	for _, f := range par.Fields {
		if f.Name == nm {
			err = f.WriteValue(out, valueOf)
			return err
		}
	}

	return fmt.Errorf("Field `%s' is not present in %v", nm, valueOf.Type())
}

func (par *firstOfParser) IsLRPossible(parsers []parser) (possible bool, canParseEmpty bool) {
	canParseEmpty = false
	possible = false

	for _, f := range par.Fields {
		p, can := f.IsLRPossible(parsers)
		if p {
			possible = true
			return
		}

		if can {
			canParseEmpty = true
		}
	}

	return
}

// Slice parser
type sliceParser struct {
	idHolder
	nonTerminal
	Parser    parser
	Delimiter string
	Min       int
}

func (par *sliceParser) ParseValue(ctx *parseContext, valueOf reflect.Value, location int, err *Error) int {
	var v reflect.Value

	valueOf.SetLen(0)
	tp := valueOf.Type().Elem()
	for {
		v = reflect.New(tp).Elem()
		var nl int

		nl = ctx.parse(v, par.Parser, location, err)
		if nl < 0 {
			if valueOf.Len() >= par.Min {
				return location
			}

			return nl
		}

		if nl <= location {
			panic("Invalid grammar: 0-length member of ZeroOrMore")
		}

		location = nl
		valueOf.Set(reflect.Append(valueOf, v))

		if len(par.Delimiter) > 0 {
			nl = ctx.skipWS(location)

			if strAt(ctx.str, nl, par.Delimiter) {
				location = ctx.skipWS(nl + len(par.Delimiter))
			} else {
				// Here we've got at least one parsed member, so it could not be an error.
				return nl
			}
		}
	}
}

func (par *sliceParser) WriteValue(out io.Writer, valueOf reflect.Value) error {
	var err error

	if valueOf.Len() < par.Min {
		return errors.New("Not enough members in slice")
	}

	for i := 0; i < valueOf.Len(); i++ {
		if i > 0 && len(par.Delimiter) > 0 {
			_, err = out.Write([]byte(par.Delimiter))
			if err != nil {
				return err
			}
		}

		v := valueOf.Index(i)
		err = par.Parser.WriteValue(out, v)
		if err != nil {
			return err
		}
	}

	return nil
}

func (par *sliceParser) IsLRPossible(parsers []parser) (possible bool, canParseEmpty bool) {
	possible, canParseEmpty = isLRPossible(par.Parser, parsers)
	if par.Min == 0 {
		canParseEmpty = true
	}

	return
}

// Ptr
type ptrParser struct {
	idHolder
	Parser   parser
	Optional bool
}

func (par *ptrParser) ParseValue(ctx *parseContext, valueOf reflect.Value, location int, err *Error) int {
	v := reflect.New(valueOf.Type().Elem())
	nl := ctx.parse(v.Elem(), par.Parser, location, err)
	if nl < 0 {
		if par.Optional {
			return location
		}
		return nl
	}

	valueOf.Set(v)

	return nl
}

func (par *ptrParser) WriteValue(out io.Writer, valueOf reflect.Value) error {
	if valueOf.IsNil() {
		if par.Optional {
			return nil
		}

		return errors.New("Not optional value is nil")
	}

	return par.Parser.WriteValue(out, valueOf.Elem())
}

func (par *ptrParser) IsLRPossible(parsers []parser) (possible bool, canParseEmpty bool) {
	possible, canParseEmpty = isLRPossible(par.Parser, parsers)
	if possible {
		return
	}

	if par.Optional {
		canParseEmpty = true
	}

	return
}

func (par *ptrParser) IsTerm() bool {
	return par.Parser.IsTerm()
}

// Parser
type parserParser struct {
	idHolder
	ptr bool
}

func (par *parserParser) ParseValue(ctx *parseContext, valueOf reflect.Value, location int, err *Error) int {
	var v Parser
	if par.ptr {
		v = valueOf.Addr().Interface().(Parser)
	} else {
		if valueOf.Kind() == reflect.Ptr {
			valueOf = reflect.New(valueOf.Type().Elem())
		}
		v = valueOf.Interface().(Parser)
	}

	l, e := v.ParseValue(ctx.str, location)
	if e != nil {
		switch ev := e.(type) {
		case Error:
			err.Location = ev.Location
			err.Message = ev.Message
			err.Str = ev.Str
			return -1
		}
		err.Location = location
		err.Message = e.Error()
		return -1
	}

	location = l
	if location > len(ctx.str) {
		panic("Invalid parser")
	}

	return location
}

var errEmptyValue = errors.New("Trying to out nil value")

func (par *parserParser) WriteValue(out io.Writer, valueOf reflect.Value) error {
	var v Parser
	if par.ptr {
		v = valueOf.Addr().Interface().(Parser)
	} else {
		v = valueOf.Interface().(Parser)
	}

	if valueOf.Kind() == reflect.Ptr && valueOf.IsNil() {
		return errEmptyValue
	}

	return v.WriteValue(out)
}

func (par *parserParser) IsLRPossible(parsers []parser) (possible bool, canParseEmpty bool) {
	return false, true // We will think bad way
}

func (par *parserParser) IsTerm() bool {
	return false // Actually it is not applicable property
}

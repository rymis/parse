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
	SetId(id uint)
	// Get identifier that has set before
	Id() uint
	// Get string representation of the parser
	String() string
	// Set string representation of the parser
	SetString(nm string)

	// Parse function.
	ParseValue(ctx *parseContext, value_of reflect.Value, location int, err *Error) int

	// Write value to output stream.
	WriteValue(out io.Writer, value_of reflect.Value) error

	// Check if this parser parses terminal symbol (doesn't contain sub-parsers)
	IsTerm() bool

	// Check possibility of left recursion.
	// This function must add all parsers with offset 0 to the set of parsers and
	// return two values: is left recursion possible (and if possible execution will be stopped)
	// and could parser parse empty value.
	IsLRPossible(parsers []parser) (possible bool, can_parse_empty bool)

	// Check if rule left recursive:
	IsLR() int
	// Set LR state:
	SetLR(v int)
}

// Utility function to call IsLRPossible
func isLRPossible(p parser, parsers []parser) (possible bool, can_parse_empty bool) {
	lr := p.IsLR()
	if lr < 0 {
		possible = true
		return
	} else if lr > 0 {
		possible = false
		return
	}

	for _, t := range parsers {
		if t.Id() == p.Id() {
			p.SetLR(-1)
			possible = true
			return
		}
	}

	possible, can_parse_empty = p.IsLRPossible(append(parsers, p))
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

func (self *idHolder) SetId(id uint) {
	self.id = id
}

func (self *idHolder) Id() uint {
	return self.id
}

func (self *idHolder) String() string {
	return self.name
}

func (self *idHolder) SetString(nm string) {
	self.name = nm
}

func (self *idHolder) IsLR() int {
	return self.lr
}

func (self *idHolder) SetLR(v int) {
	self.lr = v
}

type terminal struct {
	terminal bool
}

type nonTerminal struct {
	terminal bool
}

func (self *terminal) IsTerm() bool {
	return true
}

func (self *nonTerminal) IsTerm() bool {
	return false
}

// Parser for Go-like boolean values.
// Value is 'true' or 'false' with following character from [^a-zA-Z0-9_]
type boolParser struct {
	idHolder
	terminal
}

var boolError string = "Waiting for boolean value"

func (self *boolParser) ParseValue(ctx *parseContext, value_of reflect.Value, location int, err *Error) int {
	if strAt(ctx.str, location, "true") {
		value_of.SetBool(true)
		location += 4
	} else if strAt(ctx.str, location, "false") {
		value_of.SetBool(false)
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

func (self *boolParser) WriteValue(out io.Writer, value_of reflect.Value) error {
	var err error
	if value_of.Bool() {
		_, err = out.Write([]byte("true"))
	} else {
		_, err = out.Write([]byte("false"))
	}

	return err
}

func (self *boolParser) IsLRPossible(parsers []parser) (possible bool, can_parse_empty bool) {
	return false, false
}

// Parse string matched with regular expression.
type regexpParser struct {
	idHolder
	terminal
	Regexp *regexp.Regexp
	err    string
}

func (self *regexpParser) ParseValue(ctx *parseContext, value_of reflect.Value, location int, err *Error) int {
	m := self.Regexp.Find(ctx.str[location:])
	if m == nil {
		err.Location = location
		err.Message = self.err
		return -1
	}

	value_of.SetString(string(m))

	return location + len(m)
}

func (self *regexpParser) WriteValue(out io.Writer, value_of reflect.Value) error {
	s := value_of.String()
	if self.Regexp.MatchString(s) {
		_, err := out.Write([]byte(s))
		return err
	}

	return errors.New(fmt.Sprintf("String `%s' does not match regular expression %v", s, self.Regexp))
}

func (self *regexpParser) IsLRPossible(parsers []parser) (possible bool, can_parse_empty bool) {
	return false, self.Regexp.MatchString("")
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

			var r rune = 0
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

			var r rune = 0
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
		} else {
			err.Location = location
			err.Message = "Invalid escaped char"
			return 0, -1
		}
	} else {
		r, l := utf8.DecodeRune(ctx.str[location:])
		if l <= 0 {
			err.Location = location
			err.Message = "Invalid Unicode character"
			return 0, -1
		}

		return r, location + l
	}
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

func (self *stringParser) ParseValue(ctx *parseContext, value_of reflect.Value, location int, err *Error) int {
	s, nl := ctx.parseString(location, err)
	if nl < 0 {
		return nl
	}

	value_of.SetString(s)

	return nl
}

func (self *stringParser) WriteValue(out io.Writer, value_of reflect.Value) error {
	_, err := out.Write(strconv.AppendQuote(nil, value_of.String()))
	return err
}

func (self *stringParser) IsLRPossible(parsers []parser) (possible bool, can_parse_empty bool) {
	return false, false
}

// Parse specified literal:
type literalParser struct {
	idHolder
	terminal
	Literal string
	msg     string
}

func (self *literalParser) ParseValue(ctx *parseContext, value_of reflect.Value, location int, err *Error) int {
	if strAt(ctx.str, location, self.Literal) {
		value_of.SetString(self.Literal)
		return location + len(self.Literal)
	} else {
		err.Message = self.msg
		err.Location = location
		return -1
	}
}

func (self *literalParser) WriteValue(out io.Writer, value_of reflect.Value) error {
	_, err := out.Write([]byte(self.Literal))
	return err
}

func (self *literalParser) IsLRPossible(parsers []parser) (possible bool, can_parse_empty bool) {
	return false, len(self.Literal) == 0
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

	var res uint64 = 0
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
			} else {
				return res, location
			}
		} else { // OCT
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
			} else {
				return res, location
			}
		}
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
		} else {
			return res, location
		}
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

var floatRegexp *regexp.Regexp = regexp.MustCompile(`^[-+]?([0-9]+(\.[0-9]+)?|\.[0-9]+)([eE][-+]?[0-9]+)?`)

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

func (self *intParser) ParseValue(ctx *parseContext, value_of reflect.Value, location int, err *Error) int {
	if value_of.Type().Bits() == 32 && location < len(ctx.str) && ctx.str[location] == '\'' {
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

		value_of.SetInt(int64(r))

		return location
	}

	r, l := ctx.parseInt64(location, uint(value_of.Type().Bits()), err)
	if l < 0 {
		return l
	}

	value_of.SetInt(r)
	return l
}

func (self *intParser) WriteValue(out io.Writer, value_of reflect.Value) error {
	_, err := out.Write(strconv.AppendInt(nil, value_of.Int(), 10))
	return err
}

func (self *intParser) IsLRPossible(parsers []parser) (possible bool, can_parse_empty bool) {
	return false, false
}

func (self *uintParser) ParseValue(ctx *parseContext, value_of reflect.Value, location int, err *Error) int {
	r, l := ctx.parseUint64(location, uint(value_of.Type().Bits()), err)
	if l < 0 {
		return l
	}

	value_of.SetUint(r)
	return l
}

func (self *uintParser) WriteValue(out io.Writer, value_of reflect.Value) error {
	_, err := out.Write(strconv.AppendUint(nil, value_of.Uint(), 10))
	return err
}

func (self *uintParser) IsLRPossible(parsers []parser) (possible bool, can_parse_empty bool) {
	return false, false
}

func (self *floatParser) ParseValue(ctx *parseContext, value_of reflect.Value, location int, err *Error) int {
	r, l := ctx.parseFloat(location, value_of.Type().Bits(), err)
	if l < 0 {
		return l
	}

	value_of.SetFloat(r)
	return l
}

func (self *floatParser) WriteValue(out io.Writer, value_of reflect.Value) error {
	_, err := out.Write(strconv.AppendFloat(nil, value_of.Float(), 'e', -1, value_of.Type().Bits()))
	return err
}

func (self *floatParser) IsLRPossible(parsers []parser) (possible bool, can_parse_empty bool) {
	return false, false
}

// This parser only saves location
type locationParser struct {
	idHolder
	terminal
}

func (self *locationParser) ParseValue(ctx *parseContext, value_of reflect.Value, location int, err *Error) int {
	value_of.SetInt(int64(location))
	return location
}

func (self *locationParser) WriteValue(out io.Writer, value_of reflect.Value) error {
	return nil
}

func (self *locationParser) IsLRPossible(parsers []parser) (possible bool, can_parse_empty bool) {
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

func (self field) ParseValue(ctx *parseContext, value_of reflect.Value, location int, err *Error) int {
	var f reflect.Value
	var l int

	if self.Index < 0 {
		f = reflect.New(self.Type).Elem()
	} else {
		f = value_of.Field(self.Index)
	}

	if !f.CanSet() {
		panic(fmt.Sprintf("Can't set field '%v.%s'", value_of.Type(), self.Name))
	}

	l = ctx.parse(f, self.Parse, location, err)
	if (self.Flags & fieldNotAny) != 0 {
		if l >= 0 {
			err.Message = fmt.Sprintf("Unexpected input: %v", self.Parse)
			err.Location = location
			return -1
		}

		// Don't change the location
		return location
	} else if (self.Flags & fieldFollowedBy) != 0 {
		if l < 0 {
			return l
		}
		// Don't change the location
		return location
	} else {
		if l < 0 {
			return l
		}

		if self.Set != "" {
			method := value_of.MethodByName(self.Set)
			if !method.IsValid() && value_of.CanAddr() {
				method = value_of.Addr().MethodByName(self.Set)
			}

			if !method.IsValid() {
				panic(fmt.Sprintf("Can't find `%s' method", self.Set))
			}

			mtp := method.Type()
			if mtp.NumIn() != 1 || mtp.NumOut() != 1 || mtp.In(0) != f.Type() || mtp.Out(0).Name() != "error" {
				panic(fmt.Sprintf("Invalid method `%s' signature. Waiting for func (%v) error", self.Set, self.Parse))
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

func (self field) IsLRPossible(parsers []parser) (possible bool, can_parse_empty bool) {
	possible, can_parse_empty = isLRPossible(self.Parse, parsers)
	if possible {
		return
	}

	if (self.Flags & (fieldNotAny | fieldFollowedBy)) != 0 {
		can_parse_empty = true
	}

	return
}

func (self field) WriteValue(out io.Writer, value_of reflect.Value) error {
	if (self.Flags & (fieldNotAny | fieldFollowedBy)) != 0 {
		return nil
	}

	if self.Index < 0 { // We can not out this value in all cases but if it was literal we can do it
		// TODO: Check if it is string and output only in case it is literal
		p := self.Parse
		v := value_of
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
		return errors.New("Could not out anonymous field if it is not literal")
	} else {
		f := value_of.Field(self.Index)
		return self.Parse.WriteValue(out, f)
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

func (self *sequenceParser) ParseValue(ctx *parseContext, value_of reflect.Value, location int, err *Error) int {
	for _, f := range self.Fields {
		location = f.ParseValue(ctx, value_of, location, err)
		if location < 0 {
			return location
		}
	}

	return location
}

func (self *sequenceParser) WriteValue(out io.Writer, value_of reflect.Value) error {
	var err error
	for _, f := range self.Fields {
		err = f.WriteValue(out, value_of)
		if err != nil {
			return err
		}
	}
	return nil
}

func (self *sequenceParser) IsLRPossible(parsers []parser) (possible bool, can_parse_empty bool) {
	for _, f := range self.Fields {
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

func (self *firstOfParser) ParseValue(ctx *parseContext, value_of reflect.Value, location int, err *Error) int {
	max_error := Error{ctx.str, location - 1, "No choices in first of"}
	var l int

	for _, f := range self.Fields {
		l = f.ParseValue(ctx, value_of, location, err)
		if l >= 0 {
			value_of.FieldByName("FirstOf").FieldByName("Field").SetString(f.Name)
			return l
		} else {
			if err.Location > max_error.Location {
				max_error.Location = err.Location
				max_error.Str = err.Str
				max_error.Message = err.Message
			}
		}
	}

	err.Message = max_error.Message
	err.Location = max_error.Location
	return -1
}

func (self *firstOfParser) WriteValue(out io.Writer, value_of reflect.Value) error {
	var err error
	nm := value_of.Field(0).Field(0).String()

	if nm == "" {
		return errors.New("Field is not selected in FirstOf")
	}

	for _, f := range self.Fields {
		if f.Name == nm {
			err = f.WriteValue(out, value_of)
			return err
		}
	}

	return errors.New(fmt.Sprintf("Field `%s' is not present in %v", nm, value_of.Type()))
}

func (self *firstOfParser) IsLRPossible(parsers []parser) (possible bool, can_parse_empty bool) {
	can_parse_empty = false
	possible = false

	for _, f := range self.Fields {
		p, can := f.IsLRPossible(parsers)
		if p {
			possible = true
			return
		}

		if can {
			can_parse_empty = true
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

func (self *sliceParser) ParseValue(ctx *parseContext, value_of reflect.Value, location int, err *Error) int {
	var v reflect.Value

	value_of.SetLen(0)
	tp := value_of.Type().Elem()
	for {
		v = reflect.New(tp).Elem()
		var nl int

		nl = ctx.parse(v, self.Parser, location, err)
		if nl < 0 {
			if value_of.Len() >= self.Min {
				return location
			}

			return nl
		}

		if nl <= location {
			panic("Invalid grammar: 0-length member of ZeroOrMore")
		}

		location = nl
		value_of.Set(reflect.Append(value_of, v))

		if len(self.Delimiter) > 0 {
			nl = ctx.skipWS(location)

			if strAt(ctx.str, nl, self.Delimiter) {
				location = ctx.skipWS(nl + len(self.Delimiter))
			} else {
				// Here we've got at least one parsed member, so it could not be an error.
				return nl
			}
		}
	}
}

func (self *sliceParser) WriteValue(out io.Writer, value_of reflect.Value) error {
	var err error

	if value_of.Len() < self.Min {
		return errors.New("Not enough members in slice")
	}

	for i := 0; i < value_of.Len(); i++ {
		if i > 0 && len(self.Delimiter) > 0 {
			_, err = out.Write([]byte(self.Delimiter))
			if err != nil {
				return err
			}
		}

		v := value_of.Index(i)
		err = self.Parser.WriteValue(out, v)
		if err != nil {
			return err
		}
	}

	return nil
}

func (self *sliceParser) IsLRPossible(parsers []parser) (possible bool, can_parse_empty bool) {
	possible, can_parse_empty = isLRPossible(self.Parser, parsers)
	if self.Min == 0 {
		can_parse_empty = true
	}

	return
}

// Ptr
type ptrParser struct {
	idHolder
	Parser   parser
	Optional bool
}

func (self *ptrParser) ParseValue(ctx *parseContext, value_of reflect.Value, location int, err *Error) int {
	v := reflect.New(value_of.Type().Elem())
	nl := ctx.parse(v.Elem(), self.Parser, location, err)
	if nl < 0 {
		if self.Optional {
			return location
		} else {
			return nl
		}
	} else {
		value_of.Set(v)
	}

	return nl
}

func (self *ptrParser) WriteValue(out io.Writer, value_of reflect.Value) error {
	if value_of.IsNil() {
		if self.Optional {
			return nil
		}

		return errors.New("Not optional value is nil")
	}

	return self.Parser.WriteValue(out, value_of.Elem())
}

func (self *ptrParser) IsLRPossible(parsers []parser) (possible bool, can_parse_empty bool) {
	possible, can_parse_empty = isLRPossible(self.Parser, parsers)
	if possible {
		return
	}

	if self.Optional {
		can_parse_empty = true
	}

	return
}

func (self *ptrParser) IsTerm() bool {
	return self.Parser.IsTerm()
}

// Parser
type parserParser struct {
	idHolder
	ptr bool
}

func (self *parserParser) ParseValue(ctx *parseContext, value_of reflect.Value, location int, err *Error) int {
	var v Parser
	if self.ptr {
		v = value_of.Addr().Interface().(Parser)
	} else {
		if value_of.Kind() == reflect.Ptr {
			value_of = reflect.New(value_of.Type().Elem())
		}
		v = value_of.Interface().(Parser)
	}

	l, e := v.ParseValue(ctx.str[location:])
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

	location += l
	if location > len(ctx.str) {
		panic("Invalid parser")
	}

	return location
}

var emptyValueError = errors.New("Trying to out nil value")
func (self *parserParser) WriteValue(out io.Writer, value_of reflect.Value) error {
	var v Parser
	if self.ptr {
		v = value_of.Addr().Interface().(Parser)
	} else {
		v = value_of.Interface().(Parser)
	}

	if value_of.Kind() == reflect.Ptr && value_of.IsNil() {
		return emptyValueError
	}

	return v.WriteValue(out)
}

func (self *parserParser) IsLRPossible(parsers []parser) (possible bool, can_parse_empty bool) {
	return false, true // We will think bad way
}

func (self *parserParser) IsTerm() bool {
	return false // Actually it is not applicable property
}


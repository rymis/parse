package parse

import (
	"io"
	"reflect"
	"fmt"
	"regexp"
	"unicode/utf8"
	"bytes"
	"strconv"
	"errors"
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
	ParseValue(ctx *parseContext, value_of reflect.Value, location int) (new_loc int, err error)

	// Write value to output stream.
	WriteValue(out io.Writer, value_of reflect.Value) error
}

// Type that implements first 4 methods for all parsers
type idHolder struct {
	id uint
	name string
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

// Parser for Go-like boolean values.
// Value is 'true' or 'false' with following character from [^a-zA-Z0-9_]
type boolParser struct {
	idHolder
}

var boolError string = "Waiting for boolean value"
func (self *boolParser) ParseValue(ctx *parseContext, value_of reflect.Value, location int) (new_loc int, err error) {
	if strAt(ctx.str, location, "true") {
		value_of.SetBool(true)
		location += 4
	} else if strAt(ctx.str, location, "false") {
		value_of.SetBool(false)
		location += 5
	} else {
		return location, ctx.NewError(location, boolError)
	}

	if location < len(ctx.str) {
		if ctx.str[location] == '_' ||
			(ctx.str[location] >= 'a' && ctx.str[location] <= 'z') ||
			(ctx.str[location] >= 'A' && ctx.str[location] <= 'Z') ||
			(ctx.str[location] >= '0' && ctx.str[location] <= '9') {
			return location, ctx.NewError(location, boolError)
		}
	}

	return location, nil
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

// Parse string matched with regular expression.
type regexpParser struct {
	idHolder
	Regexp *regexp.Regexp
	err     string
}

func (self *regexpParser) ParseValue(ctx *parseContext, value_of reflect.Value, location int) (new_loc int, err error) {
	m := self.Regexp.Find(ctx.str[location:])
	if m == nil {
		return location, ctx.NewError(location, self.err)
	}

	value_of.SetString(string(m))

	return location + len(m), nil
}

func (self *regexpParser) WriteValue(out io.Writer, value_of reflect.Value) error {
	s := value_of.String()
	if self.Regexp.MatchString(s) {
		_, err := out.Write([]byte(s))
		return err
	}

	return errors.New(fmt.Sprintf("String `%s' does not match regular expression %v", s, self.Regexp))
}

func newRegexpParser(rx string) (parser, error) {
	r, err := regexp.Compile("^" + rx)
	if err != nil {
		return nil, err
	}

	msg := fmt.Sprintf("Waiting for /%s/", rx)

	return &regexpParser{ Regexp: r, err: msg }, nil
}

// Go string parser.
type stringParser struct {
	idHolder
}

// Parse Go unicode value:
func (ctx *parseContext) parseUnicodeValue(location int) (rune, int, error) {
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
		return 0, location, ctx.NewError(location, "Unexpected end of file: waiting for Unicode character")
	}

	if ctx.str[location] == '\\' {
		location++
		if location >= len(ctx.str) {
			return 0, location, ctx.NewError(location, "Unexpected end of file in escape sequence")
		}

		if (ctx.str[location] == '\\') {
			return '\\', location + 1, nil
		} else if (ctx.str[location] == 'a') {
			return '\a', location + 1, nil
		} else if (ctx.str[location] == 'b') {
			return '\b', location + 1, nil
		} else if (ctx.str[location] == 'f') {
			return '\f', location + 1, nil
		} else if (ctx.str[location] == 'n') {
			return '\n', location + 1, nil
		} else if (ctx.str[location] == 'r') {
			return '\r', location + 1, nil
		} else if (ctx.str[location] == 't') {
			return '\t', location + 1, nil
		} else if (ctx.str[location] == 'v') {
			return '\v', location + 1, nil
		} else if (ctx.str[location] == '`') {
			return '`', location + 1, nil
		} else if (ctx.str[location] == '\'') {
			return '\'', location + 1, nil
		} else if (ctx.str[location] == '"') {
			return '"', location + 1, nil
		} else if (ctx.str[location] >= '0' && ctx.str[location] < 3) {
			if location + 2 >= len(ctx.str) {
				return 0, location, ctx.NewError(location, "Unexpected end of file in escape sequence")
			}

			var r rune = 0
			for i := 0; i < 3; i++ {
				if (ctx.str[location + i] >= '0' && ctx.str[location + i] <= '7') {
					r = r * 8 + rune(ctx.str[location + i] - '0')
				} else {
					return 0, location, ctx.NewError(location, "Invalid character in octal_byte")
				}
			}

			return r, location + 3, nil

		} else if (ctx.str[location] == 'x' || ctx.str[location] == 'u' || ctx.str[location] == 'U') {
			var l int
			if ctx.str[location] == 'x' {
				l = 2
			} else if ctx.str[location] == 'u' {
				l = 4
			} else {
				l = 8
			}

			if location + l >= len(ctx.str) {
				return 0, location, ctx.NewError(location, "Unexpected end of file in escape sequence")
			}

			location++

			var r rune = 0
			for i := 0; i < l; i++ {
				if (ctx.str[location + i] >= '0' && ctx.str[location + i] <= '9') {
					r = r * 16 + rune(ctx.str[location + i] - '0')
				} else if (ctx.str[location + i] >= 'a' && ctx.str[location + i] <= 'f') {
					r = r * 16 + rune(ctx.str[location + i] - 'a' + 10)
				} else if (ctx.str[location + i] >= 'A' && ctx.str[location + i] <= 'F') {
					r = r * 16 + rune(ctx.str[location + i] - 'A' + 10)
				} else {
					return 0, location, ctx.NewError(location, "Illegal character in hex code")
				}
			}

			if !utf8.ValidRune(r) {
				return 0, location, ctx.NewError(location - 2, "Invalid rune")
			}

			return r, location + l, nil
		} else {
			return 0, location, ctx.NewError(location, "Invalid escaped char")
		}
	} else {
		r, l := utf8.DecodeRune(ctx.str[location:])
		if l <= 0 {
			return 0, location, ctx.NewError(location, "Invalid Unicode character")
		}

		return r, location + l, nil
	}
}

// Parse Go string and return processed string:
func (ctx *parseContext) parseString(location int) (string, int, error) {
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
				return buf.String(), location + 1, nil
			} else if (ctx.str[location] == '\r') { // Skip it
				location++;
			} else {
				buf.WriteByte(ctx.str[location])
				location++;
			}
		}
	} else if ctx.str[location] == '"' { // interpreted string
		for location++; location < len(ctx.str); {
			if ctx.str[location] == '"' {
				return buf.String(), location + 1, nil
			}

			r, l, err := ctx.parseUnicodeValue(location)
			if err != nil {
				return "", l, err
			}

			if r >= 0x80 && r <= 0xff && l - location == 4 { // TODO: make it better
				buf.WriteByte(byte(r))
			} else {
				_, err = buf.WriteRune(r)
				if err != nil {
					return "", location, ctx.NewError(location, "Invalid Rune: %s", err.Error())
				}
			}

			location = l
		}
	}

	return "", location, ctx.NewError(location, "Waiting for Go string");
}

func (self *stringParser) ParseValue(ctx *parseContext, value_of reflect.Value, location int) (int, error) {
	s, nl, err := ctx.parseString(location)
	if err != nil {
		return nl, err
	}

	value_of.SetString(s)

	return nl, nil
}

func (self *stringParser) WriteValue(out io.Writer, value_of reflect.Value) error {
	_, err := out.Write(strconv.AppendQuote(nil, value_of.String()))
	return err
}

// Parse specified literal:
type literalParser struct {
	idHolder
	Literal string
	msg     string
}

func (self *literalParser) ParseValue(ctx *parseContext, value_of reflect.Value, location int) (int, error) {
	if strAt(ctx.str, location, self.Literal) {
		value_of.SetString(self.Literal)
		return location + len(self.Literal), nil
	} else {
		return location, ctx.NewError(location, self.msg)
	}
}

func (self *literalParser) WriteValue(out io.Writer, value_of reflect.Value) error {
	_, err := out.Write([]byte(self.Literal))
	return err
}

func newLiteralParser(lit string) parser {
	msg := fmt.Sprintf("Waiting for '%s'", lit)
	return &literalParser{ Literal: lit, msg: msg }
}

// Check if there was overflow for <size> bits type
func (ctx *parseContext) checkUintOverflow(v uint64, location int, size uint) (uint64, int, error) {
	if size >= 64 {
		return v, location, nil
	}

	if (v >> size) != 0 {
		return 0, location, ctx.NewError(location, "Integer overflow (%d bits)", size)
	}

	return v, location, nil
}

// Parse uint value and save it in uint64.
// size is value size in bits.
func (ctx *parseContext) parseUint64(location int, size uint) (uint64, int, error) {
	if location >= len(ctx.str) {
		return 0, location, ctx.NewError(location, "Unexpected end of file. Waiting for integer literal.")
	}

	var res uint64 = 0
	if ctx.str[location] == '0' {
		if location + 1 < len(ctx.str) && (ctx.str[location + 1] == 'x' || ctx.str[location + 1] == 'X') { // HEX
			location += 2

			if location >= len(ctx.str) {
				return 0, location, ctx.NewError(location, "Unexpected end of file in hexadecimal literal.")
			}

			for ; location < len(ctx.str); location++ {
				if (res & 0xf000000000000000) != 0 {
					return 0, location, ctx.NewError(location, "Integer overflow")
				}

				if (ctx.str[location] >= '0') && (ctx.str[location] <= '9') {
					res = (res << 4) + uint64(ctx.str[location] - '0')
				} else if (ctx.str[location] >= 'a') && (ctx.str[location] <= 'f') {
					res = (res << 4) + uint64(ctx.str[location] - 'a') + 10
				} else if (ctx.str[location] >= 'A') && (ctx.str[location] <= 'F') {
					res = (res << 4) + uint64(ctx.str[location] - 'A') + 10
				} else {
					break
				}
			}

			return ctx.checkUintOverflow(res, location, size)
		} else { // OCT
			for ; location < len(ctx.str); location++ {
				if (res & 0xe000000000000000) != 0 {
					return 0, location, ctx.NewError(location, "Integer overflow")
				}

				if ctx.str[location] >= '0' && ctx.str[location] <= '7' {
					res = (res << 3) + uint64(ctx.str[location] - '0')
				} else {
					break
				}
			}

			return ctx.checkUintOverflow(res, location, size)
		}
	} else if ctx.str[location] > '0' && ctx.str[location] <= '9' {
		var r8 uint64
		for ; location < len(ctx.str); location++ {
			if (res & 0xe000000000000000) != 0 {
				return 0, location, ctx.NewError(location, "Integer overflow")
			}

			if ctx.str[location] >= '0' && ctx.str[location] <= '9' {
				r8 = res << 3 // r8 = res * 8 Here could not be overflow: we have checked this before
				res = r8 + (res << 1)
				if res < r8 { // Overflow!
					return 0, location, ctx.NewError(location, "Integer overflow")
				}

				res += uint64(ctx.str[location] - '0')
			} else {
				break
			}
		}

		return ctx.checkUintOverflow(res, location, size)
	}

	return 0, location, ctx.NewError(location, "Waiting for integer literal")
}

// Parse int value and save it in int64.
// size is value size in bits.
func (ctx *parseContext) parseInt64(location int, size uint) (int64, int, error) {
	neg := false
	if location >= len(ctx.str) {
		return 0, location, ctx.NewError(location, "Unexpected end of file. Waiting for integer.")
	}

	if ctx.str[location] == '-' {
		neg = true
		location++

		/* TODO: allow spaces after '-'??? */
	}

	v, l, err := ctx.parseUint64(location, size)
	if err != nil {
		return 0, location, err
	}

	if (v & 0x8000000000000000) != 0 {
		return 0, location, ctx.NewError(location, "Integer overflow")
	}

	res := int64(v)
	if neg {
		res = -res
	}

	return res, l, nil
}

var floatRegexp *regexp.Regexp = regexp.MustCompile(`^[-+]?([0-9]+(\.[0-9]+)?|\.[0-9]+)([eE][-+]?[0-9]+)?`)
func (ctx *parseContext) parseFloat(location int, size int) (float64, int, error) {
	m := floatRegexp.Find(ctx.str[location:])

	if m == nil {
		return 0.0, location, ctx.NewError(location, "Waiting for floating point number")
	}

	r, err := strconv.ParseFloat(string(m), size)
	if err != nil {
		return 0.0, location, ctx.NewError(location, "Invalid floating point number")
	}

	return r, location + len(m), nil
}

type intParser struct {
	idHolder
}

type uintParser struct {
	idHolder
}

type floatParser struct {
	idHolder
}

func (self *intParser) ParseValue(ctx *parseContext, value_of reflect.Value, location int) (int, error) {
	if value_of.Type().Bits() == 32 && location < len(ctx.str) && ctx.str[location] == '\'' {
		location++
		r, location, err := ctx.parseUnicodeValue(location)
		if err != nil {
			return location, err
		}

		if location >= len(ctx.str) || ctx.str[location] != '\'' {
			return location, ctx.NewError(location, "Waiting for closing quote in unicode character")
		}
		location++

		value_of.SetInt(int64(r))

		return location, nil
	}

	r, l, err := ctx.parseInt64(location, uint(value_of.Type().Bits()))
	if err != nil {
		return location, err
	}

	value_of.SetInt(r)
	return l, nil
}

func (self *intParser) WriteValue(out io.Writer, value_of reflect.Value) error {
	_, err := out.Write(strconv.AppendInt(nil, value_of.Int(), 10))
	return err
}

func (self *uintParser) ParseValue(ctx *parseContext, value_of reflect.Value, location int) (int, error) {
	r, l, err := ctx.parseUint64(location, uint(value_of.Type().Bits()))
	if err != nil {
		return location, err
	}

	value_of.SetUint(r)
	return l, nil
}

func (self *uintParser) WriteValue(out io.Writer, value_of reflect.Value) error {
	_, err := out.Write(strconv.AppendUint(nil, value_of.Uint(), 10))
	return err
}

func (self *floatParser) ParseValue(ctx *parseContext, value_of reflect.Value, location int) (int, error) {
	r, l, err := ctx.parseFloat(location, value_of.Type().Bits())
	if err != nil {
		return location, err
	}

	value_of.SetFloat(r)
	return l, nil
}

func (self *floatParser) WriteValue(out io.Writer, value_of reflect.Value) error {
	_, err := out.Write(strconv.AppendFloat(nil, value_of.Float(), 'e', -1, value_of.Type().Bits()))
	return err
}

type field struct {
	Name   string
	Index  int
	Parse  parser
	Flags  uint
	Set    string
	Type   reflect.Type
}

func (self field) ParseValue(ctx *parseContext, value_of reflect.Value, location int) (int, error) {
	var f reflect.Value
	var l int
	var err error

	if self.Index < 0 {
		f = reflect.New(self.Type).Elem()
	} else {
		f = value_of.Field(self.Index)
	}

	if !f.CanSet() {
		return location, errors.New(fmt.Sprintf("Can't set field '%v.%s'", value_of.Type(), self.Name))
	}

	l, err = ctx.parse(f, self.Parse, location)
	if (self.Flags & fieldNotAny) != 0 {
		if err == nil {
			return location, ctx.NewError(location, "Unexpected input: %v", self.Parse)
		}

		// Don't change the location
		return location, nil
	} else if (self.Flags & fieldFollowedBy) != 0 {
		if err != nil {
			return l, err
		}
		// Don't change the location
		return location, nil
	} else {
		if err != nil {
			return l, err
		}

		if self.Set != "" {
			method := value_of.MethodByName(self.Set)
			if !method.IsValid() && value_of.CanAddr() {
				method = value_of.Addr().MethodByName(self.Set)
			}

			if !method.IsValid() {
				return location, errors.New(fmt.Sprintf("Can't find `%s' method", self.Set))
			}

			mtp := method.Type()
			if mtp.NumIn() != 1 || mtp.NumOut() != 1 || mtp.In(0) != f.Type() || mtp.Out(0).Name() != "error" {
				return location, errors.New(fmt.Sprintf("Invalid method `%s' signature. Waiting for func (%v) error", self.Set, self.Parse))
			}

			resv := method.Call([]reflect.Value{f})[0]
			if resv.IsNil() {
				err = nil
			} else {
				err = resv.Interface().(error)
			}

			if err != nil {
				return location, err
			}
		}

		return ctx.skipWS(l), nil
	}

	return -1, errors.New("XXX")
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
	Fields []field
}

func (self *sequenceParser) ParseValue(ctx *parseContext, value_of reflect.Value, location int) (int, error) {
	var err error
	for _, f := range(self.Fields) {
		location, err = f.ParseValue(ctx, value_of, location)
		if err != nil {
			return location, err
		}
	}

	return location, nil
}

func (self *sequenceParser) WriteValue(out io.Writer, value_of reflect.Value) error {
	var err error
	for _, f := range(self.Fields) {
		err = f.WriteValue(out, value_of)
		if err != nil {
			return err
		}
	}
	return nil
}

type firstOfParser struct {
	idHolder
	Fields []field
}

func (self *firstOfParser) ParseValue(ctx *parseContext, value_of reflect.Value, location int) (new_loc int, err error) {
	max_error := Error{ ctx.str, location - 1, "No choices in first of" }
	var l int

	for _, f := range(self.Fields) {
		l, err = f.ParseValue(ctx, value_of, location)
		if err == nil {
			value_of.FieldByName("FirstOf").FieldByName("Field").SetString(f.Name)
			return l, nil
		} else {
			switch err := err.(type) {
			case Error:
				if err.Location > max_error.Location {
					max_error.Location = err.Location
					max_error.Str = err.Str
					max_error.Message = err.Message
				}
			default:
				return l, err
			}
		}
	}

	return location, max_error
}

func (self *firstOfParser) WriteValue(out io.Writer, value_of reflect.Value) error {
	var err error
	nm := value_of.Field(0).Field(0).String()

	if nm == "" {
		return errors.New("Field is not selected in FirstOf")
	}

	for _, f := range(self.Fields) {
		if f.Name == nm {
			err = f.WriteValue(out, value_of)
			return err
		}
	}

	return errors.New(fmt.Sprintf("Field `%s' is not present in %v", nm, value_of.Type()))
}

// Slice parser
type sliceParser struct {
	idHolder
	Parser    parser
	Delimiter string
	Min       int
}

func (self *sliceParser) ParseValue(ctx *parseContext, value_of reflect.Value, location int) (new_loc int, err error) {
	var v reflect.Value

	value_of.SetLen(0)
	tp := value_of.Type().Elem()
	for {
		v = reflect.New(tp).Elem()
		var nl int

		nl, err = ctx.parse(v, self.Parser, location)
		if err != nil {
			if value_of.Len() >= self.Min {
				return location, nil
			}

			return nl, err
		}

		if nl <= location {
			fmt.Printf("V: %v\nTYPE: %v\nLOC: %d\nVAL: %v\nNL: %d\nSTR: %s\n", v, value_of.Type().Elem(), location, value_of, nl, string(ctx.str[location:]))
			return -1, errors.New("Invalid grammar: 0-length member of ZeroOrMore")
		}

		location = nl
		value_of.Set(reflect.Append(value_of, v))

		if len(self.Delimiter) > 0 {
			nl = ctx.skipWS(location)

			if strAt(ctx.str, nl, self.Delimiter) {
				location = ctx.skipWS(nl + len(self.Delimiter))
			} else {
				// Here we've got at least one parsed member, so it could not be an error.
				return nl, nil
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

// Ptr
type ptrParser struct {
	idHolder
	Parser parser
	Optional bool
}

func (self *ptrParser) ParseValue(ctx *parseContext, value_of reflect.Value, location int) (new_loc int, err error) {
	v := reflect.New(value_of.Type().Elem())
	nl, err := ctx.parse(v.Elem(), self.Parser, location)
	if err != nil {
		switch err.(type) {
		case Error:
			if self.Optional {
				return location, nil
			} else {
				return nl, err
			}
		default:
			return nl, err
		}
	} else {
		value_of.Set(v)
	}

	return nl, err
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



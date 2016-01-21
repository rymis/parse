/*
Easy to use PEG implementation with Go.

This package contains PEG (Parsing Expressions Grammar) implementation that could be used with Go.
This library is much different from another libraries because grammar mapped to Go types, so you don't need to use
external grammar files nor use expressions to specify one like with pyparsing or Boost.Spirit.

For example you can parse hello world using this structure:

	type HelloWorld struct {
		Hello string `regexp:"[hH]ello"`
		_     string `regexp:","`
		World string `regexp:"[a-zA-Z]+"`
		_     string `regexp:"!?"`
	}

And the only thing you need to do is call Parse function:

	var hello HelloWorld
	new_location, err := parse.Parse(&hello, []byte("Hello, World!"), nil)

You can also specify whitespace skipping function (default is to skip all spaces, tabulations, new-lines and carier returns)
packrat using, grammar debugging options et. cetera.

One of the interesting features of this library is ability to parse Go base data types using Go grammar. For example you can
simply parse int64 with Parse:

	var i int64
	new_location, err := parse.Parse(&i, []byte("123"), nil)

If you need to parse variant types you need to insert FirstOf as first field in your structure:

	type StringOrInt struct {
		FirstOf
		Str     string
		Int     int
	}
	new_location, err := parse.Parse(new(StringOrInt), `"I can parse Go string!"`, nil)

Optional fields must be of pointer type and contain `optional:"true"` tag. You can use slices that
will be parsed as ELEMENT* or ELEMENT+ (if `repeat:"+"` was set in tag).

Parser supports left recursion out of the box so you can parse expressions without a problem. For example you can parse this grammar:
	X <- E
	E <- X '-' Number / Number
with
	type X struct {
		Expr E
	}
	type E struct {
		FirstOf
		Expr struct {
			Expr *X
			_ string `regexp:"-"`
			N uint64
		}
		N uint64

	}
*/
package parse

import (
	"reflect"
	"fmt"
	"errors"
	"regexp"
	"unicode"
	"unicode/utf8"
	"sync"
	"bytes"
	"strconv"
)

// Error is parse error representation.
// Error implements error interface. Error message contains message, position information and marked error line.
type Error struct {
	// Original string
	Str []byte
	// Location of this error in the original string
	Location int
	// Error message
	Message string
}

// FirstOf is structure that indicates that we need to parse first expression of the fields of structure.
// After pasring Field contains name of parsed field.
type FirstOf struct {
	// Name of parsed field
	Field string
}

// Returns error string of parse error.
// It is well-formed version of error so you can simply write it to user.
func (self Error) Error() string {
	start := 0
	lineno := 1
	col := 1
	i := 0
	for i = 0; i < len(self.Str) - 1 && i < self.Location; i++ {
		if self.Str[i] == '\n' {
			lineno++
			start = i + 1
			col = 1
		}
		col++
	}

	for ; i < len(self.Str); i++ {
		if self.Str[i] == '\n' {
			break
		}
	}

	var s string
	if len(self.Str) > start + col - 1 {
		s = string(self.Str[start:start + col - 1]) + "<!--here--!>" + string(self.Str[start + col - 1:i])
	} else {
		s = string(self.Str[start:i])
	}

	return fmt.Sprintf("Syntax error at line %d:%d: %s\n%s", lineno, col, self.Message, s)
}

// Key in packrat table representation
type packratKey struct {
	// Location of parsed value
	location int
	// Type of parsed value
	t        reflect.Type
	// Tag for field
	tag      reflect.StructTag
}

type packratValue struct {
	// Set to true when result is actual in table
	parsed    bool

	// Recursion level
	recursionLevel int

	// New location
	new_loc  int
	// Value
	value    reflect.Value
	// Error
	err      error
}

func (self packratValue) String() string {
	return fmt.Sprintf("{ parsed = %v, recursion = %d, new_loc = %d, err = %v }", self.parsed, self.recursionLevel, self.new_loc, self.err)
}

// Params is structure containing parameters of the parsing process.
type Params struct {
	// Function to skip whitespaces. If nil will not skip anything.
	SkipWhite            func (str []byte, loc int) int
	// Flag to enable packrat parsing. If not set packrat table is used only for left recursion detection and processing.
	PackratEnabled       bool
	// Enable grammar debugging messages. It is useful if you have some problems with grammar but produces a lot of output.
	Debug                bool
}

// Private variant of context. Contains string and packrat table.
type context struct {
	params *Params
	str []byte
	packrat map[packratKey]packratValue
	// Locations with recursive rules:
	recursiveLocations  map[int]bool
}

// Create new parse.Error:
func (ctx *context) NewError(location int, msg string, args... interface{}) error {
	var s string

	if len(args) == 0 {
		s = msg
	} else {
		s = fmt.Sprintf(msg, args...)
	}

	return Error{ctx.str, location, s}
}

// Map of compiled regular expressions. I beleive that take value from map is faster operation then compile regular
// expression in most cases.
var (
	_compiled_map map[string]*regexp.Regexp
	_compiled_mtx sync.Mutex
)

// Compile regular expression and save in global map.
func compileRegexp(rx string) (*regexp.Regexp, error) {
	_compiled_mtx.Lock()
	defer _compiled_mtx.Unlock()

	r, ok := _compiled_map[rx]
	if ok {
		return r, nil
	}

	r, err := regexp.Compile("^" + rx)
	if err != nil {
		_compiled_map[rx] = r
	}

	return r, err
}

// Parse regular expression and return result as string.
// This function searches for compiled regexp in global and must be faster when compile + find.
func (ctx *context) parseRegexp(location int, rx string) (string, int, error) {
	r, err := compileRegexp(rx)
	if err != nil {
		return "", location, err
	}

	m := r.Find(ctx.str[location:])
	if m == nil {
		return "", location, ctx.NewError(location, "Waiting for /%s/", rx)
	}

	return string(m), location + len(m), nil
}

// Parse Go unicode value:
func (ctx *context) parseUnicodeValue(location int) (rune, int, error) {
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
func (ctx *context) parseString(location int) (string, int, error) {
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

// Check if there was overflow for <size> bits type
func (ctx *context) checkUintOverflow(v uint64, location int, size uint) (uint64, int, error) {
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
func (ctx *context) parseUint64(location int, size uint) (uint64, int, error) {
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
func (ctx *context) parseInt64(location int, size uint) (int64, int, error) {
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

func (ctx *context) parseFloat(location int, size int) (float64, int, error) {
	s, l, err := ctx.parseRegexp(location, `[-+]?([0-9]+(\.[0-9]+)?|\.[0-9]+)([eE][-+]?[0-9]+)?`)
	if err != nil {
		return 0.0, location, ctx.NewError(location, "Waiting for floating point number")
	}

	r, err := strconv.ParseFloat(s, size)
	if err != nil {
		return 0.0, location, ctx.NewError(location, "Invalid floating point number")
	}

	return r, l, nil
}

// Skip whitespace:
func (ctx *context) skipWS(loc int) int {
	if ctx.params != nil {
		if ctx.params.SkipWhite != nil {
			l := ctx.params.SkipWhite(ctx.str, loc)
			if l >= loc {
				return l
			}
		}
	}

	return loc
}

// Show debug message if need to
func (ctx *context) debug(msg string, args... interface{}) {
	if ctx.params != nil && ctx.params.Debug {
		fmt.Printf("DEBUG: " + msg, args...)
	}
}

// Helper function to parse struture field.
func (ctx *context) parseField(value_of reflect.Value, idx int, location int) (new_loc int, err error) {
	type_of := value_of.Type()
	f_type := type_of.Field(idx)

	if f_type.Tag.Get("skip") == "true" {
		// Skip this field
		return location, nil
	}

	var f reflect.Value
	if f_type.Name != "_" {
		r, l := utf8.DecodeRuneInString(f_type.Name)
		if l == 0 || !unicode.IsUpper(r) { // Private field
			ctx.debug("[PRIVATE FIELD: %s]\n", f_type.Name)

			return location, nil
		}

		f = value_of.Field(idx)
	} else {
		f = reflect.New(f_type.Type).Elem()
	}

	if !f.CanSet() {
		return location, errors.New(fmt.Sprintf("Can't set field '%v.%s'", type_of, f_type.Name))
	}

	not_any := f_type.Tag.Get("not_any") == "true"
	followed_by := f_type.Tag.Get("followed_by") == "true"

	ctx.debug("[FIELD: %s] (type=%v, not_any=%v, followed_by=%v)\n", f_type.Name, f_type.Type, not_any, followed_by)

	var l int
	l, err = ctx.parse(f, f_type.Tag, location)

	if not_any {
		if err == nil {
			return location, Error{ctx.str, location, "Unexpected input"}
		}

		// Don't change the location
		return location, nil
	} else if followed_by {
		if err != nil {
			return l, err
		}
		// Don't change the location
		return location, nil
	} else {
		if err != nil {
			return l, err
		}

		set := f_type.Tag.Get("set")
		if set != "" {
			method := value_of.MethodByName(set)
			if !method.IsValid() && value_of.CanAddr() {
				method = value_of.Addr().MethodByName(set)
			}

			if !method.IsValid() {
				return location, errors.New(fmt.Sprintf("Can't find `%s' method", set))
			}

			mtp := method.Type()
			if mtp.NumIn() != 1 || mtp.NumOut() != 1 || mtp.In(0) != f_type.Type || mtp.Out(0).Name() != "error" {
				return location, errors.New(fmt.Sprintf("Invalid method `%s' signature. Waiting for func (%v) error", set, f.Type()))
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

		new_loc = ctx.skipWS(l)
		return
	}
}

// Internal parse function without packrat:
func (ctx *context) parseValue(value_of reflect.Value, tag reflect.StructTag, location int) (new_loc int, err error) {
	type_of := value_of.Type()

	location = ctx.skipWS(location)

	if !value_of.CanSet() {
		return location, errors.New(fmt.Sprintf("Invalid argument for parse: can't set (%v: %v)", value_of, type_of))
	}

	switch value_of.Kind() {
	case reflect.Struct:
		if value_of.NumField() == 0 { // Empty
			return location, nil
		}

		if type_of.Field(0).Type == reflect.TypeOf(FirstOf{}) {
			max_error := Error{ ctx.str, location - 1, "No choices in first of" }
			var l int
			for i := 1; i < value_of.NumField(); i++ {
				l, err = ctx.parseField(value_of, i, location)

				if err == nil {
					value_of.FieldByName("FirstOf").FieldByName("Field").SetString(type_of.Field(i).Name)
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
		} else {
			for i := 0; i < value_of.NumField(); i++ {
				location, err = ctx.parseField(value_of, i, location)

				if err != nil {
					return location, err
				}
			}
		}

		return location, nil

	case reflect.String:
		var s string
		rx := tag.Get("regexp")
		if rx == "" {
			lit := tag.Get("literal")
			if lit == "" {
				s, location, err = ctx.parseString(location)
				if err != nil {
					return location, err
				}
			} else {
				if strAt(ctx.str, location, lit) {
					s = string(ctx.str[location:location + len(lit)])
					location += len(lit)
				} else {
					return location, ctx.NewError(location, "Waiting for '%s'", lit)
				}
			}
		} else {
			s, location, err = ctx.parseRegexp(location, rx)
			if err != nil {
				return location, err
			}
		}

		value_of.SetString(s)

		return location, nil

	case reflect.Int32:
		var r int32

		if location >= len(ctx.str) {
			return location, ctx.NewError(location, "Unexpected end of file: waiting for int32")
		}

		if ctx.str[location] == '\'' {
			location++
			r, location, err = ctx.parseUnicodeValue(location)
			if err != nil {
				return location, err
			}

			if location >= len(ctx.str) || ctx.str[location] != '\'' {
				return location, ctx.NewError(location, "Waiting for closing quote in unicode character")
			}
			location++
		} else {
			v, l, err := ctx.parseInt64(location, 32)
			if err != nil {
				return 0, err
			}

			location = l
			r = int32(v)
		}

		value_of.SetInt(int64(r))

		return location, nil

	case reflect.Int, reflect.Int8, reflect.Int16, reflect.Int64:
		r, l, err := ctx.parseInt64(location, uint(type_of.Bits()))
		if err != nil {
			return location, err
		}
		value_of.SetInt(r)
		location = l
		return location, nil

	case reflect.Uint, reflect.Uint8, reflect.Uint16, reflect.Uint32, reflect.Uint64:
		r, l, err := ctx.parseUint64(location, uint(type_of.Bits()))
		if err != nil {
			return location, err
		}
		value_of.SetUint(r)
		location = l
		return location, nil

	case reflect.Bool:
		if location + 3 < len(ctx.str) &&
				ctx.str[location    ] == 't' &&
				ctx.str[location + 1] == 'r' &&
				ctx.str[location + 2] == 'u' &&
				ctx.str[location + 3] == 'e' {

			value_of.SetBool(true)
		} else if location + 4 < len(ctx.str) &&
				ctx.str[location    ] == 'f' &&
				ctx.str[location + 1] == 'a' &&
				ctx.str[location + 2] == 'l' &&
				ctx.str[location + 3] == 's' &&
				ctx.str[location + 4] == 'e' {

			value_of.SetBool(false)
		} else {
			return location, ctx.NewError(location, "Waiting for boolean value")
		}

	case reflect.Float32, reflect.Float64:
		r, l, err := ctx.parseFloat(location, type_of.Bits())
		if err != nil {
			return location, err
		}
		value_of.SetFloat(r)
		location = l
		return location, nil

	/* TODO: complex numbers */

	case reflect.Slice:
		min := 0

		tmp := tag.Get("repeat")
		if tmp == "*" {
			min = 0
		} else if tmp == "+" {
			min = 1
		}

		tp := type_of.Elem()
		value_of.SetLen(0)
		for {
			v := reflect.New(tp)
			var nl int

			nl, err = ctx.parse(v.Elem(), tag, location)
			if err != nil {
				if value_of.Len() >= min {
					return location, nil
				}

				return location, err
			}

			if nl == location {
				return -1, errors.New("Invalid grammar: 0-length member of ZeroOrMore")
			}

			location = nl
			value_of.Set(reflect.Append(value_of, v.Elem()))
		}

	case reflect.Ptr:
		v := reflect.New(type_of.Elem())
		var nl int

		nl, err = ctx.parse(v.Elem(), tag, location)
		if err != nil {
			switch err.(type) {
			case Error:
				if tag.Get("optional") != "true" {
					return location, err
				}
				nl = location
			default:
				return location, err
			}
		} else {
			value_of.Set(v)
		}

		return nl, nil
	default:
		return -1, errors.New(fmt.Sprintf("Invalid argument for Parse: unsupported type '%v'", type_of))
	}

	return 0, nil
}

// Internal parse function
func (ctx *context) parse(value_of reflect.Value, tag reflect.StructTag, location int) (new_loc int, err error) {
	ctx.debug("[PARSE {%v} `%v` %d %v]\n", value_of.Type(), tag, location, ctx.params)

	if false && (ctx.params == nil || !ctx.params.PackratEnabled) { // TODO: remove this code completely
		new_loc, err = ctx.parseValue(value_of, tag, location)
		if err != nil {
			ctx.debug("ER [%d] {%s:%v} %v\n", location, value_of.Type(), tag, err)
		} else {
			ctx.debug("OK [%d->%d] {%s:%v} %v\n", location, new_loc, value_of.Type(), tag, value_of.Interface())
		}
		return
	}

	key := packratKey{location, value_of.Type(), tag}
	cache, ok := ctx.packrat[key]
	if ok {
		ctx.debug("CACHE [%d] %v\n", location, cache)

		if cache.parsed { // Cached value
			if cache.err == nil {
				value_of.Set(cache.value.Elem())
			}

			return cache.new_loc, cache.err
		}

		if cache.recursionLevel == 0 { // Recursion detected:
			// Left recursion parsing in progress:
			ctx.recursiveLocations[location] = true

			cache.recursionLevel = 1
			cache.err = ctx.NewError(location, "Waiting for %v", key.t)
			cache.new_loc = location
			ctx.packrat[key] = cache
			return location, cache.err
		} else { // Return previous recursion level result:
			if cache.err == nil {
				value_of.Set(cache.value.Elem())
			}

			return cache.new_loc, cache.err
		}

		return location, errors.New("LR failed") // Not reached
	}

	ctx.packrat[key] = packratValue{ parsed: false, recursionLevel: 0, new_loc: location }
	l, err := ctx.parseValue(value_of, tag, location)
	cache = ctx.packrat[key]

	if cache.recursionLevel == 0 { // Not recursive
		if !ctx.recursiveLocations[location] {
			if ctx.params == nil || !ctx.params.PackratEnabled {
				delete(ctx.packrat, key)
			} else {
				cache.parsed = true
				cache.err = err
				if err == nil {
					cache.value = reflect.New(key.t)
					cache.value.Elem().Set(value_of)
				}
				cache.new_loc = l
				ctx.packrat[key] = cache
			}
		} else {
			delete(ctx.packrat, key)
		}

		return l, err
	} else {
		ctx.recursiveLocations[location] = true

		cache.new_loc = l
		cache.err = err
		if err == nil {
			cache.value = reflect.New(key.t)
			cache.value.Elem().Set(value_of)
		}
		cache.recursionLevel = 2
		ctx.packrat[key] = cache

		for {
			// We will parse n times until the error or stop of position increasing:
			// cache = ctx.packrat[key]

			cache.recursionLevel = 2
			ctx.packrat[key] = cache

			l, err := ctx.parseValue(value_of, tag, location)

			cache = ctx.packrat[key]
			if err != nil { // This step was not good so we must return previous value
				cache.parsed = true
				ctx.packrat[key] = cache

				if cache.err == nil {
					value_of.Set(cache.value.Elem())
				}

				return cache.new_loc, cache.err
			} else if l <= cache.new_loc { // End of recursion: there was no increasing of position
				if cache.err != nil {
					cache.value = reflect.New(key.t)
					cache.value.Elem().Set(value_of)
					cache.new_loc = l
					cache.err = nil
				} else {
					value_of.Set(cache.value.Elem())
				}
				cache.parsed = true
				cache.recursionLevel = 0
				ctx.packrat[key] = cache
				return cache.new_loc, nil
			}

			cache.new_loc = l
			cache.err = nil
			if !cache.value.IsValid() {
				cache.value = reflect.New(key.t)
			}
			cache.value.Elem().Set(value_of)

			ctx.packrat[key] = cache
		}
	}

	return l, err
}

func skipDefault(str []byte, loc int) int {
	return SkipSpaces(str, loc)
}

// Parse value from string and return position after parsing and error.
// This function parses value using PEG parser.
// Here: result is pointer to value,
// str is string to parse,
// params is parsing parameters.
// Function returns new_location - location after the parsed string. On errors err != nil.
func Parse(result interface{}, str []byte, params *Params) (new_location int, err error) {
	type_of := reflect.TypeOf(result)
	value_of := reflect.ValueOf(result)

	if type_of.Kind() != reflect.Ptr {
		return -1, errors.New("Invalid argument for Parse: waiting for pointer")
	}

	if params == nil {
		params = &Params{ SkipWhite: skipDefault }
	}

	C := new(context)
	C.params = params
	C.str = str
	C.packrat = make(map[packratKey]packratValue)
	C.recursiveLocations = make(map[int]bool)

	new_location, err =  C.parse(value_of.Elem(), reflect.StructTag(""), 0)

	return
}

// Create new default parameters object.
func NewParams() *Params {
	return &Params{ SkipWhite: skipDefault }
}

// Skip spaces, tabulations and newlines:
func SkipSpaces(str []byte, loc int) int {
	for i := loc; i < len(str); i++ {
		if str[i] != ' ' && str[i] != '\t' && str[i] != '\n' && str[i] != '\r' {
			return i
		}
	}

	return len(str)
}

func strAt(str []byte, loc int, s string) bool {
	if loc + len(s) < len(str) {
		for i := range(s) {
			if str[loc + i] != s[i] {
				return false
			}
		}
		return true
	}
	return false
}

// Skip one-line comment that starts from begin and ends with newline or end of string
func SkipOneLineComment(str []byte, loc int, begin string) int {
	if strAt(str, loc, begin) {
		loc += len(begin)

		for ; loc < len(str); loc++ {
			if str[loc] == '\n' {
				return loc + 1
			}
		}

		return loc
	}
	return loc
}

// Skip multiline comment that starts from begin and ends with end.
// If you are allowing nested comments recursive must be set to true.
func SkipMultilineComment(str []byte, loc int, begin, end string, recursive bool) int {
	if strAt(str, loc, begin) {
		for i := loc + len(begin); i < len(str) - len(end); i++ {
			if strAt(str, i, end) {
				return i + len(end)
			}

			if recursive && strAt(str, i, begin) {
				j := SkipMultilineComment(str, i, begin, end, recursive)
				if j == i { // Here was error
					return loc
				}
				i = j - 1
			}
		}
	}

	return loc
}

// Skip shell style comment: "# .... \n"
func SkipShellComment(str []byte, loc int) int {
	return SkipOneLineComment(str, loc, "#")
}

// Skip C++ style comment: "// ..... \n"
func SkipCPPComment(str []byte, loc int) int {
	return SkipOneLineComment(str, loc, "//")
}

// Skip C style comment: "/* ..... */"
func SkipCComment(str []byte, loc int) int {
	return SkipMultilineComment(str, loc, "/*", "*/", false)
}

// Skip Pascal style comment: "(* ... *)"
func SkipPascalComment(str []byte, loc int) int {
	return SkipMultilineComment(str, loc, "(*", "*)", true)
}

// Skip HTML style comment: "<!-- ... -->"
func SkipHTMLComment(str []byte, loc int) int {
	return SkipMultilineComment(str, loc, "<!--", "-->", false)
}

// Skip Ada style comment: "-- .... \n"
func SkipAdaComment(str []byte, loc int) int {
	return SkipOneLineComment(str, loc, "--")
}

// Skip Lisp style comment: "; .... \n"
func SkipLispComment(str []byte, loc int) int {
	return SkipOneLineComment(str, loc, ";")
}

// Skip TeX style comment: "% .... \n"
func SkipTeXComment(str []byte, loc int) int {
	return SkipOneLineComment(str, loc, ";")
}

// Skip any count of any substrings defined by skip functions.
func SkipAll(str []byte, loc int, funcs... func ([]byte, int) int) int {
	var l int
	var skipped bool
	for {
		skipped = false
		for _, f := range(funcs) {
			l = f(str, loc)
			if l > loc {
				loc = l
				skipped = true
			}
		}

		if !skipped {
			return loc
		}
	}

	return loc
}


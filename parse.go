/*
	Easy to use PEG implementation with Go.
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
)

// Error is parse error representation.
//
// Here str is original string, location - location of error in source string, message is error message.
type Error struct {
	str []byte
	location int
	message string
}

// FirstOf is empty structure that indicates that we need to parse first expression of the fields of structure
type FirstOf struct {
	// Name of parsed field
	Field string
}

func (self Error) Error() string {
	start := 0
	lineno := 1
	col := 1
	i := 0
	for i = 0; i < len(self.str) - 1 && i < self.location; i++ {
		if self.str[i] == '\n' {
			lineno++
			start = i + 1
			col = 1
		}
		col++
	}

	for ; i < len(self.str); i++ {
		if self.str[i] == '\n' {
			break
		}
	}

	var s string
	if len(self.str) > start + col - 1 {
		s = string(self.str[start:start + col - 1]) + "<!--here--!>" + string(self.str[start + col - 1:i])
	} else {
		s = string(self.str[start:i])
	}

	return fmt.Sprintf("Syntax error at line %d:%d: %s\n%s", lineno, col, self.message, s)
}

type packrat_key struct {
	location int
	t        reflect.Type
	tag      reflect.StructTag
}

type packrat_value struct {
	process  bool
	v        reflect.Value
	location int
	err      error
}

// Parse context. It is structure containing information useful while parsing processes.
type Context struct {
	skip_ws_f func (str []byte, loc int) int
	packrat_enabled bool
	debug bool
}

type context struct {
	Context
	str []byte
	packrat map[packrat_key]packrat_value
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

var (
	_compiled_map map[string]*regexp.Regexp
	_compiled_mtx sync.Mutex
)

func compile_regexp(rx string) (*regexp.Regexp, error) {
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

// Parse regular expression and return result as string
func (ctx *context) parse_regexp(location int, rx string) (string, int, error) {
	r, err := compile_regexp(rx)
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
func (ctx *context) parse_unicode_value(location int) (rune, int, error) {
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
func (ctx *context) parse_string(location int) (string, int, error) {
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

			r, l, err := ctx.parse_unicode_value(location)
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

func (ctx *context) check_uint_overflow(v uint64, location int, size uint) (uint64, int, error) {
	if size == 8 {
		return v, location, nil
	}

	if (v >> size) != 0 {
		return 0, location, ctx.NewError(location, "Integer overflow (%d bits)", size)
	}

	return v, location, nil
}

func (ctx *context) parse_uint64(location int, size uint) (uint64, int, error) {
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

			return ctx.check_uint_overflow(res, location, size)
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

			return ctx.check_uint_overflow(res, location, size)
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

		return ctx.check_uint_overflow(res, location, size)
	}

	return 0, location, ctx.NewError(location, "Waiting for integer literal")
}

func (ctx *context) parse_int64(location int, size uint) (int64, int, error) {
	neg := false
	if location >= len(ctx.str) {
		return 0, location, ctx.NewError(location, "Unexpected end of file. Waiting for integer.")
	}

	if ctx.str[location] == '-' {
		neg = true
		location++

		/* TODO: allow spaces after '-'??? */
	}

	v, l, err := ctx.parse_uint64(location, size)
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

// Skip whitespace:
func (ctx *context) skip_ws(loc int) int {
	l := ctx.skip_ws_f(ctx.str, loc)
	if l >= loc {
		return l
	}
	return loc
}

func (ctx *context) parse_field(value_of reflect.Value, idx int, location int) (new_loc int, err error) {
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
			if ctx.debug {
				fmt.Printf("\t[PRIVATE FIELD: %s]\n", f_type.Name)
			}

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

	if ctx.debug {
		fmt.Printf("\t[FIELD: %s] (not_any=%v, followed_by=%v)\n", f_type.Name, not_any, followed_by)
	}

	var l int
	l, err = ctx.parse_int(f, f_type.Tag, location)

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
				return location, errors.New(fmt.Sprintf("Invalid method `%s' signature. Waiting for func (%s) error", set, f.Type().Name()))
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

		new_loc = ctx.skip_ws(l)
		return
	}
}

// Internal parse function
func (ctx *context) parse_int(value_of reflect.Value, tag reflect.StructTag, location int) (new_loc int, err error) {
	if !ctx.packrat_enabled {
		new_loc, err = ctx.parse_raw(value_of, tag, location)
		if ctx.debug {
			if err != nil {
				fmt.Printf("ER [%d] {%s:%v} %v\n", location, value_of.Type(), tag, err)
			} else {
				fmt.Printf("OK [%d->%d] {%s:%v} %v\n", location, new_loc, value_of.Type(), tag, value_of.Interface())
			}
		}
		return
	}

	key := packrat_key{location, value_of.Type(), tag}
	cache, ok := ctx.packrat[key]
	if ok {
		if ctx.debug {
			fmt.Printf("CACHE [%d] %v\n", location, cache)
		}

		if cache.process {
			return location, ctx.NewError(location, "Unrecoverable left recurtion in grammar")
		}

		if cache.err == nil {
			value_of.Set(cache.v.Elem())
		}

		return cache.location, cache.err
	}

	var v packrat_value
	v.process = true
	ctx.packrat[key] = v

	l, err := ctx.parse_raw(value_of, tag, location)
	v.location = l
	v.err = err
	v.process = false
	if err == nil {
		v.v = reflect.New(key.t)
		v.v.Elem().Set(value_of)
	}

	ctx.packrat[key] = v

	if ctx.debug {
		fmt.Printf("SET CACHE [%d] %v\n", location, v)
	}

	return l, err
}

// Internal parse function without packrat:
func (ctx *context) parse_raw(value_of reflect.Value, tag reflect.StructTag, location int) (new_loc int, err error) {
	type_of := value_of.Type()

	location = ctx.skip_ws(location)

	if !value_of.CanSet() {
		return location, errors.New(fmt.Sprintf("Invalid argument for parse_int: can't set (%v: %v)", value_of, type_of))
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
				l, err = ctx.parse_field(value_of, i, location)

				if err == nil {
					value_of.FieldByName("FirstOf").FieldByName("Field").SetString(type_of.Field(i).Name)
					return l, nil
				} else {
					switch err := err.(type) {
					case Error:
						if err.location > max_error.location {
							max_error.location = err.location
							max_error.str = err.str
							max_error.message = err.message
						}
					default:
						return l, err
					}
				}
			}

			return location, max_error
		} else {
			for i := 0; i < value_of.NumField(); i++ {
				location, err = ctx.parse_field(value_of, i, location)

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
			s, location, err = ctx.parse_string(location)
			if err != nil {
				return location, err
			}
		} else {
			s, location, err = ctx.parse_regexp(location, rx)
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
			r, location, err = ctx.parse_unicode_value(location)
			if err != nil {
				return location, err
			}

			if location >= len(ctx.str) || ctx.str[location] != '\'' {
				return location, ctx.NewError(location, "Waiting for closing quote in unicode character")
			}
			location++
		} else {
			v, l, err := ctx.parse_int64(location, 32)
			if err != nil {
				return 0, err
			}

			location = l
			r = int32(v)
		}

		value_of.SetInt(int64(r))

		return location, nil

	case reflect.Int, reflect.Int8, reflect.Int16, reflect.Int64:
		r, l, err := ctx.parse_int64(location, uint(type_of.Bits()))
		if err != nil {
			return location, err
		}
		value_of.SetInt(r)
		location = l
		return location, nil

	case reflect.Uint, reflect.Uint8, reflect.Uint16, reflect.Uint32, reflect.Uint64:
		r, l, err := ctx.parse_uint64(location, uint(type_of.Bits()))
		if err != nil {
			return location, err
		}
		value_of.SetUint(r)
		location = l
		return location, nil

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

			nl, err = ctx.parse_int(v.Elem(), tag, location)
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

		nl, err = ctx.parse_int(v.Elem(), tag, location)
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

func skip_default(str []byte, loc int) int {
	for i := loc; i < len(str); i++ {
		if str[i] != ' ' && str[i] != '\t' && str[i] != '\n' && str[i] != '\r' {
			return i
		}
	}

	return len(str)
}

func Parse(result interface{}, str []byte) (new_location int, err error) {
	return ParseFull(result, str, skip_default)
}

func ParseFull(result interface{}, str []byte, ignore func ([]byte, int) int) (new_location int, err error) {
	ctx := new(Context)
	ctx.SetIgnore(ignore)
	new_location, err = ctx.Parse(result, str)
	return
}

func New() *Context {
	ctx := new(Context)
	ctx.skip_ws_f = skip_default
	ctx.packrat_enabled = false

	return ctx
}

func (ctx *Context) Parse(result interface{}, str []byte) (new_location int, err error) {
	type_of := reflect.TypeOf(result)
	value_of := reflect.ValueOf(result)

	if type_of.Kind() != reflect.Ptr {
		return -1, errors.New("Invalid argument for Parse: waiting for pointer")
	}

	C := new(context)
	C.Context = *ctx
	C.str = str
	C.packrat = make(map[packrat_key]packrat_value)

	new_location, err =  C.parse_int(value_of.Elem(), reflect.StructTag(""), 0)

	return
}

func (ctx *Context) SetIgnore(ignore func ([]byte, int) int) {
	ctx.skip_ws_f = ignore
}

func (ctx *Context) SetPackrat(v bool) {
	ctx.packrat_enabled = v
}

func (ctx *Context) SetDebug(v bool) {
	ctx.debug = v
}


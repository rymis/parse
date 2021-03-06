/*
Package parse - easy to use PEG implementation with Go.

This package contains PEG (Parsing Expressions Grammar) implementation that could be used with Go.
This library is much different from other libraries because grammar mapped to Go types, so you don't need to use
external grammar files nor expressions to specify one like with pyparsing or Boost.Spirit.

For example you can parse hello world using this structure:

	type HelloWorld struct {
		Hello string `regexp:"[hH]ello"`
		_     string `literal:","`
		World string `regexp:"[a-zA-Z]+"`
		_     string `regexp:"!?"`
	}

And the only thing you need to do is call Parse function:

	var hello HelloWorld
	newLocation, err := parse.Parse(&hello, []byte("Hello, World!"), nil)

You can also specify whitespace skipping function (default is to skip all spaces, tabulations, new-lines and carier returns)
packrat using, grammar debugging options et. cetera.

One of the interesting features of this library is ability to parse Go base data types using Go grammar. For example you can
simply parse int64 with Parse:

	var i int64
	newLocation, err := parse.Parse(&i, []byte("123"), nil)

If you need to parse variant types you need to insert FirstOf as first field in your structure:

	type StringOrInt struct {
		FirstOf
		Str     string
		Int     int
	}
	newLocation, err := parse.Parse(new(StringOrInt), `"I can parse Go string!"`, nil)

Optional fields must be of pointer type and contain `optional:"true"` tag. You can use slices that
will be parsed as ELEMENT* or ELEMENT+ (if `repeat:"+"` was set in tag). You can specify another tags and types listed bellow.

	+-------------+-------------+----------------------------------------------------+
	| Type        | Tag         | Description                                        |
	+-------------+-------------+----------------------------------------------------+
	| string      |             | Parse Go string. `string` and "string" are both    |
	|             |             | supported.                                         |
	+-------------+-------------+----------------------------------------------------+
	| string      | regexp      | Parse regular expression in regexp module syntax.  |
	+-------------+-------------+----------------------------------------------------+
	| string      | literal     | Parse literal specified in tag. If there are both  |
	|             |             | regexp and literal specified regexp will be used.  |
	+-------------+-------------+----------------------------------------------------+
	| int*        |             | Parse integer constant. Hexadecimal, Octal and     |
	|             |             | decimal constants supported. int32 and rune types  |
	|             |             | are the same type in Go, so int32 parse characters |
	|             |             | in Go syntax.                                      |
	+-------------+-------------+----------------------------------------------------+
	| int*        | parse       | If tag parse:"#" was set parser will save current  |
	|             |             | location in this field and will not advance one.   |
	+-------------+-------------+----------------------------------------------------+
	| uint*       |             | Same as int* but unsigned constant.                |
	+-------------+-------------+----------------------------------------------------+
	| float*      |             | Parse floating point number.                       |
	+-------------+-------------+----------------------------------------------------+
	| bool        |             | Parse boolean constant (true or false)             |
	+-------------+-------------+----------------------------------------------------+
	| []type      | parse       | Parse sequence of type. If parse is not specified  |
	|             |             | or parse is '*' here could be zero or more         |
	|             |             | elements. If parse is '+' here could be one or     |
	|             |             | more elements.                                     |
	+-------------+-------------+----------------------------------------------------+
	| []type      | delimiter   | Parse list with delimiter literal. It is very      |
	|             |             | common situation to have a DELIMITER b DELIMITER...|
	|             |             | like lists so I think that it is good idea to      |
	|             |             | support such lists out of the box.                 |
	+-------------+-------------+----------------------------------------------------+
	| *type       | parse       | Parse type. Element will be allocated or set to nil|
	|             |             | for optional elements that doesn't present. If     |
	|             |             | parse was specified and set to '?' element is      |
	|             |             | optional: if it is not present in the input field  |
	|             |             | will be nil.                                       |
	+-------------+-------------+----------------------------------------------------+
	| any         | parse       | If parse == "skip" field will be skipped while     |
	|             |             | parsing or encoding. If parse == "&" it is followed|
	|             |             | by element: it will be parsed but position will not|
	|             |             | be increased. If parse == "!" it is not predicate: |
	|             |             | element must not be present at this position.      |
	+-------------+-------------+----------------------------------------------------+
	| any         | set         | If present this tag contains name of the method to |
	|             |             | call after parsing of element. Method must have    |
	|             |             | signature func (x element-type) error.             |
	+-------------+-------------+----------------------------------------------------+

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
	"errors"
	"fmt"
	"io"
	"reflect"
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
func (e Error) Error() string {
	start := 0
	lineno := 1
	col := 1
	i := 0
	for i = 0; i < len(e.Str)-1 && i < e.Location; i++ {
		if e.Str[i] == '\n' {
			lineno++
			start = i + 1
			col = 1
		}
		col++
	}

	for ; i < len(e.Str); i++ {
		if e.Str[i] == '\n' {
			break
		}
	}

	var s string
	if len(e.Str) > start+col-1 {
		s = string(e.Str[start:start+col-1]) + "<!--here--!>" + string(e.Str[start+col-1:i])
	} else {
		s = string(e.Str[start:i])
	}

	return fmt.Sprintf("Syntax error at line %d:%d: %s\n%s", lineno, col, e.Message, s)
}

// Parser interface. Parser will call ParseValue method to parse values of this types.
type Parser interface {
	// This function must parse value from buffer and return length or error
	ParseValue(buf []byte, loc int) (newLocation int, err error)
	// This function must write value into the output stream.
	WriteValue(out io.Writer) error
}

type packratKey struct {
	rule     uint
	location int
}

type packratValue struct {
	// Set to true when result is actual in table
	parsed bool

	// Recursion level
	recursionLevel int

	// New location
	newLocation int
	// Value
	value reflect.Value
	// Error
	msg         string
	errLocation int
}

// Parse context
type parseContext struct {
	params *Options
	// String to parse.
	str []byte
	// Packrat table
	packrat map[packratKey]*packratValue
	// Locations with recursive rules:
	recursiveLocations map[int]bool
}

func (pv packratValue) String() string {
	return fmt.Sprintf("{ parsed = %v, recursion = %d, newLocation = %d, errLocation = %d, msg = %s }", pv.parsed, pv.recursionLevel, pv.newLocation, pv.errLocation, pv.msg)
}

// Create new parse.Error:
func (ctx *parseContext) NewError(location int, msg string, args ...interface{}) error {
	var s string

	if len(args) == 0 {
		s = msg
	} else {
		s = fmt.Sprintf(msg, args...)
	}

	return Error{ctx.str, location, s}
}

// Show debug message if need to
func (ctx *parseContext) debug(msg string, args ...interface{}) {
	if ctx.params != nil && ctx.params.Debug {
		fmt.Printf("DEBUG: "+msg, args...)
	}
}

// Skip whitespace:
func (ctx *parseContext) skipWS(loc int) int {
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

// Internal parse function
func (ctx *parseContext) parse(valueOf reflect.Value, p parser, location int, err *Error) int {
	ctx.debug("[PARSE {%v} %d %v]\n", p, location, ctx.params)

	location = ctx.skipWS(location)

	if !ctx.params.PackratEnabled {
		if p.IsLR() > 0 { // Left recursion is not possible
			return p.ParseValue(ctx, valueOf, location, err)
		}
	}

	key := packratKey{p.ID(), location}
	cache, ok := ctx.packrat[key]
	if ok {
		ctx.debug("[CACHE [%d] %v]\n", location, cache)

		if cache.parsed { // Cached value
			if cache.newLocation >= 0 {
				valueOf.Set(cache.value.Elem())
			} else {
				err.Location = cache.errLocation
				err.Message = cache.msg
			}

			ctx.debug("[RETURN %d %d %v]\n", cache.newLocation, cache.errLocation, cache.msg)
			return cache.newLocation
		}

		if cache.recursionLevel == 0 { // Recursion detected:
			// Left recursion parsing in progress:
			ctx.recursiveLocations[location] = true

			cache.recursionLevel = 1
			cache.msg = fmt.Sprintf("Waiting for %v", p) // TODO: generate once
			cache.newLocation = -1
			cache.errLocation = location
			ctx.debug("[RETURN %d]\n", location)
			return -1
		}
		// Return previous recursion level result:
		if cache.newLocation >= 0 {
			valueOf.Set(cache.value.Elem())
		} else {
			err.Message = cache.msg
			err.Location = cache.errLocation
		}

		ctx.debug("[RETURN %d]\n", cache.newLocation)
		return cache.newLocation
	}

	ctx.packrat[key] = &packratValue{parsed: false, recursionLevel: 0, newLocation: location}
	l := p.ParseValue(ctx, valueOf, location, err)
	cache = ctx.packrat[key]

	if cache.recursionLevel == 0 { // Not recursive
		if !ctx.recursiveLocations[location] {
			if ctx.params == nil || !ctx.params.PackratEnabled {
				delete(ctx.packrat, key)
			} else {
				cache.parsed = true
				cache.msg = err.Message
				cache.errLocation = err.Location
				if l >= 0 {
					cache.value = reflect.New(valueOf.Type())
					cache.value.Elem().Set(valueOf)
				}
				cache.newLocation = l
			}
		} else {
			delete(ctx.packrat, key)
		}

		ctx.debug("[RETURN %d]\n", l)
		return l
	}

	ctx.recursiveLocations[location] = true

	cache.newLocation = l
	cache.msg = err.Message
	cache.errLocation = err.Location
	if l >= 0 {
		cache.value = reflect.New(valueOf.Type())
		cache.value.Elem().Set(valueOf)
	}
	cache.recursionLevel = 2

	for {
		// We will parse n times until the error or stop of position increasing:
		cache.recursionLevel = 2

		l := p.ParseValue(ctx, valueOf, location, err)

		// cache = ctx.packrat[key] // TODO: ???
		if l < 0 { // This step was not good so we must return previous value
			cache.parsed = true

			if cache.newLocation >= 0 {
				valueOf.Set(cache.value.Elem())
			}

			ctx.debug("[RETURN %d]\n", cache.newLocation)

			return cache.newLocation
		} else if cache.newLocation >= 0 && l <= cache.newLocation { // End of recursion: there was no increasing of position
			valueOf.Set(cache.value.Elem())
			cache.parsed = true
			cache.recursionLevel = 0
			ctx.debug("[RETURN %d]\n", cache.newLocation)
			return cache.newLocation
		}

		cache.newLocation = l
		if !cache.value.IsValid() {
			cache.value = reflect.New(valueOf.Type())
		}
		cache.value.Elem().Set(valueOf)
	}

	//	ctx.debug("[RETURN %d %v]\n", l, err)
	//	return l, err
}

// Options is structure containing parameters of the parsing process.
type Options struct {
	// Function to skip whitespaces. If nil will not skip anything.
	SkipWhite func(str []byte, loc int) int
	// Flag to enable packrat parsing. If not set packrat table is used only for left recursion detection and processing.
	PackratEnabled bool
	// Enable grammar debugging messages. It is useful if you have some problems with grammar but produces a lot of output.
	Debug bool
}

// Parse value from string and return position after parsing and error.
// This function parses value using PEG parser.
// Here: result is pointer to value,
// str is string to parse,
// params is parsing parameters.
// Function returns newLocation - location after the parsed string. On errors err != nil.
func Parse(result interface{}, str []byte, params *Options) (newLocation int, err error) {
	typeOf := reflect.TypeOf(result)
	valueOf := reflect.ValueOf(result)

	if typeOf.Kind() != reflect.Ptr {
		return -1, errors.New("Invalid argument for Parse: waiting for pointer")
	}

	if params == nil {
		params = &Options{SkipWhite: SkipSpaces}
	}

	p, err := compile(typeOf.Elem(), reflect.StructTag(""))
	if err != nil {
		return -1, err
	}

	C := new(parseContext)
	C.params = params
	C.str = str
	C.packrat = make(map[packratKey]*packratValue)
	C.recursiveLocations = make(map[int]bool)

	e := Error{str, 0, ""}
	newLocation = C.parse(valueOf.Elem(), p, 0, &e)
	if newLocation < 0 {
		return newLocation, e
	}

	return newLocation, nil
}

// NewOptions creates new default parameters object.
func NewOptions() *Options {
	return &Options{SkipWhite: SkipSpaces}
}

// SkipSpaces skips spaces, tabulations and newlines:
func SkipSpaces(str []byte, loc int) int {
	for i := loc; i < len(str); i++ {
		if str[i] != ' ' && str[i] != '\t' && str[i] != '\n' && str[i] != '\r' {
			return i
		}
	}

	return len(str)
}

func strAt(str []byte, loc int, s string) bool {
	if loc+len(s) <= len(str) {
		for i := range s {
			if str[loc+i] != s[i] {
				return false
			}
		}
		return true
	}
	return false
}

// SkipOneLineComment skips one-line comment that starts from begin and ends with newline or end of string
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

// SkipMultilineComment skips multiline comment that starts from begin and ends with end.
// If you are allowing nested comments recursive must be set to true.
func SkipMultilineComment(str []byte, loc int, begin, end string, recursive bool) int {
	if strAt(str, loc, begin) {
		for i := loc + len(begin); i < len(str)-len(end); i++ {
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

// SkipShellComment skips shell style comment: "# .... \n"
func SkipShellComment(str []byte, loc int) int {
	return SkipOneLineComment(str, loc, "#")
}

// SkipCPPComment skips C++ style comment: "// ..... \n"
func SkipCPPComment(str []byte, loc int) int {
	return SkipOneLineComment(str, loc, "//")
}

// SkipCComment skips C style comment: "/* ..... */"
func SkipCComment(str []byte, loc int) int {
	return SkipMultilineComment(str, loc, "/*", "*/", false)
}

// SkipPascalComment skips Pascal style comment: "(* ... *)"
func SkipPascalComment(str []byte, loc int) int {
	return SkipMultilineComment(str, loc, "(*", "*)", true)
}

// SkipHTMLComment skips HTML style comment: "<!-- ... -->"
func SkipHTMLComment(str []byte, loc int) int {
	return SkipMultilineComment(str, loc, "<!--", "-->", false)
}

// SkipAdaComment skips Ada style comment: "-- .... \n"
func SkipAdaComment(str []byte, loc int) int {
	return SkipOneLineComment(str, loc, "--")
}

// SkipLispComment skips Lisp style comment: "; .... \n"
func SkipLispComment(str []byte, loc int) int {
	return SkipOneLineComment(str, loc, ";")
}

// SkipTeXComment skips TeX style comment: "% .... \n"
func SkipTeXComment(str []byte, loc int) int {
	return SkipOneLineComment(str, loc, ";")
}

// SkipAll skips any count of any substrings defined by skip functions.
func SkipAll(str []byte, loc int, funcs ...func([]byte, int) int) int {
	var l int
	var skipped bool
	for {
		skipped = false
		for _, f := range funcs {
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
}

package parse

import (
	"reflect"
	"io"
	"errors"
	"fmt"
	"unicode"
	"unicode/utf8"
	"sync"
)

type packratKey2 struct {
	rule uint
	location int
}

// Parse context
type parseContext struct {
	params *Options
	// String to parse.
	str []byte
	// Packrat table
	packrat map[packratKey2]packratValue
	// Locations with recursive rules:
	recursiveLocations  map[int]bool
}

// Create new parse.Error:
func (ctx *parseContext) NewError(location int, msg string, args... interface{}) error {
	var s string

	if len(args) == 0 {
		s = msg
	} else {
		s = fmt.Sprintf(msg, args...)
	}

	return Error{ctx.str, location, s}
}

// Show debug message if need to
func (ctx *parseContext) debug(msg string, args... interface{}) {
	if ctx.params != nil && ctx.params.Debug {
		fmt.Printf("DEBUG: " + msg, args...)
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

// Object to hold parser. I need this one because recursive rules compilation.
type proxyParser struct {
	p parser
}

func (self *proxyParser) ParseValue(ctx *parseContext, value_of reflect.Value, location int) (new_loc int, err error) {
	if self.p == nil {
		panic("nil parser")
	}

	return self.p.ParseValue(ctx, value_of, location)
}

func (self *proxyParser) WriteValue(out io.Writer, value_of reflect.Value) error {
	if self.p == nil {
		panic("nil parser")
	}

	return self.p.WriteValue(out, value_of)
}

func (self *proxyParser) SetId(id uint) {
	if self.p == nil {
		panic("nil parser")
	}

	self.p.SetId(id)
}

func (self *proxyParser) Id() uint {
	if self.p == nil {
		panic("nil parser")
	}

	return self.p.Id()
}

func (self *proxyParser) String() string {
	if self.p == nil {
		panic("nil parser")
	}

	return self.p.String()
}

func (self *proxyParser) SetString(nm string) {
	if self.p == nil {
		panic("nil parser")
	}

	self.p.SetString(nm)
}

func (self *proxyParser) SetParser(p parser) {
	if self.p != nil {
		panic("Trying to change parser in proxy object")
	}

	self.p = p
}

// Internal parse function
func (ctx *parseContext) parse(value_of reflect.Value, p parser, location int) (int, error) {
	ctx.debug("[PARSE {%v} %d %v]\n", p, location, ctx.params)

	location = ctx.skipWS(location)

	key := packratKey2{ p.Id(), location }
	cache, ok := ctx.packrat[key]
	if ok {
		ctx.debug("[CACHE [%d] %v]\n", location, cache)

		if cache.parsed { // Cached value
			if cache.err == nil {
				value_of.Set(cache.value.Elem())
			}

			ctx.debug("[RETURN %d %v]\n", cache.new_loc, cache.err)
			return cache.new_loc, cache.err
		}

		if cache.recursionLevel == 0 { // Recursion detected:
			// Left recursion parsing in progress:
			ctx.recursiveLocations[location] = true

			cache.recursionLevel = 1
			cache.err = ctx.NewError(location, "Waiting for %v", p)
			cache.new_loc = location
			ctx.packrat[key] = cache
			ctx.debug("[RETURN %d %v]\n", location, cache.err)
			return location, cache.err
		} else { // Return previous recursion level result:
			if cache.err == nil {
				value_of.Set(cache.value.Elem())
			}

			ctx.debug("[RETURN %d %v]\n", cache.new_loc, cache.err)
			return cache.new_loc, cache.err
		}

		return location, errors.New("LR failed") // Not reached
	}

	ctx.packrat[key] = packratValue{ parsed: false, recursionLevel: 0, new_loc: location }
	l, err := p.ParseValue(ctx, value_of, location)
	cache = ctx.packrat[key]

	if cache.recursionLevel == 0 { // Not recursive
		if !ctx.recursiveLocations[location] {
			if ctx.params == nil || !ctx.params.PackratEnabled {
				delete(ctx.packrat, key)
			} else {
				cache.parsed = true
				cache.err = err
				if err == nil {
					cache.value = reflect.New(value_of.Type())
					cache.value.Elem().Set(value_of)
				}
				cache.new_loc = l
				ctx.packrat[key] = cache
			}
		} else {
			delete(ctx.packrat, key)
		}

		ctx.debug("[RETURN %d %v]\n", l, err)
		return l, err
	} else {
		ctx.recursiveLocations[location] = true

		cache.new_loc = l
		cache.err = err
		if err == nil {
			cache.value = reflect.New(value_of.Type())
			cache.value.Elem().Set(value_of)
		}
		cache.recursionLevel = 2
		ctx.packrat[key] = cache

		for {
			// We will parse n times until the error or stop of position increasing:
			cache = ctx.packrat[key]

			cache.recursionLevel = 2
			ctx.packrat[key] = cache

			l, err := p.ParseValue(ctx, value_of, location)

			cache = ctx.packrat[key]
			if err != nil { // This step was not good so we must return previous value
				cache.parsed = true
				ctx.packrat[key] = cache

				if cache.err == nil {
					value_of.Set(cache.value.Elem())
				}

				ctx.debug("[RETURN %d %v]\n", cache.new_loc, cache.err)
				return cache.new_loc, cache.err
			} else if l <= cache.new_loc { // End of recursion: there was no increasing of position
				if cache.err != nil {
					cache.value = reflect.New(value_of.Type())
					cache.value.Elem().Set(value_of)
					cache.new_loc = l
					cache.err = nil
				} else {
					value_of.Set(cache.value.Elem())
				}
				cache.parsed = true
				cache.recursionLevel = 0
				ctx.packrat[key] = cache
				ctx.debug("[RETURN %d %v]\n", cache.new_loc, cache.err)
				return cache.new_loc, nil
			}

			cache.new_loc = l
			cache.err = nil
			if !cache.value.IsValid() {
				cache.value = reflect.New(value_of.Type())
			}
			cache.value.Elem().Set(value_of)

			ctx.packrat[key] = cache
		}
	}

	ctx.debug("[RETURN %d %v]\n", l, err)
	return l, err
}

func appendField(type_of reflect.Type, fields *[]field, idx int) error {
	f_type := type_of.Field(idx)

	if f_type.Tag.Get("skip") == "true" {
		// Skipping
		return nil
	}

	fld := field{ Name: f_type.Name, Type: f_type.Type }
	if f_type.Name != "_" {
		r, l := utf8.DecodeRuneInString(f_type.Name)
		if l == 0 || !unicode.IsUpper(r) { // Private field: skipping
			return nil
		}

		fld.Index = idx
	} else {
		fld.Index = -1
	}

	if f_type.Tag.Get("not_any") == "true" {
		fld.Flags |= fieldNotAny
	} else if f_type.Tag.Get("followed_by") == "true" {
		fld.Flags |= fieldFollowedBy
	}

	fld.Set = f_type.Tag.Get("set")

	p, err := compile(f_type.Type, f_type.Tag)
	if err != nil {
		return nil
	}

	fld.Parse = p

	*fields = append(*fields, fld)

	return nil
}

type typeAndTag struct {
	Type     reflect.Type
	Tag      reflect.StructTag
}

var _compiledParsers = make(map[typeAndTag]parser)
var _lastId uint = 1
var _lastIdMutex sync.Mutex

func compile(type_of reflect.Type, tag reflect.StructTag) (parser, error) {
	key := typeAndTag{type_of, tag}
	p, ok := _compiledParsers[key]
	if ok {
		return p, nil
	}

	proxy := &proxyParser{ nil }
	_compiledParsers[key] = proxy

	p, err := compileType(type_of, tag)
	if err != nil {
		delete(_compiledParsers, key)
		return nil, err
	}

	p.SetString(fmt.Sprintf("%v `%v`", type_of, tag))
	_lastIdMutex.Lock()
	p.SetId(_lastId)
	_lastId++
	proxy.SetParser(p)
	_lastIdMutex.Unlock()

	return p, nil
}

func compileType(type_of reflect.Type, tag reflect.StructTag) (p parser, err error) {
	switch type_of.Kind() {
	case reflect.Struct:
		if type_of.NumField() == 0 { // Empty
			return &sequenceParser{ Fields : nil }, nil
		}

		fields := make([]field, 0)
		if type_of.Field(0).Type == reflect.TypeOf(FirstOf{}) { // FirstOf
			for i := 1; i < type_of.NumField(); i++ {
				err = appendField(type_of, &fields, i)
				if err != nil {
					return nil, err
				}
			}

			return &firstOfParser{ Fields: fields }, nil
		} else {
			for i := 0; i < type_of.NumField(); i++ {
				err = appendField(type_of, &fields, i)

				if err != nil {
					return nil, err
				}
			}

			return &sequenceParser{ Fields: fields }, nil
		}

		return nil, errors.New("XXX")

	case reflect.String:
		rx := tag.Get("regexp")
		if rx == "" {
			lit := tag.Get("literal")
			if lit == "" {
				return &stringParser{}, nil
			} else {
				return newLiteralParser(lit), nil
			}
		} else {
			return newRegexpParser(rx)
		}

		return nil, errors.New("XXX")

	case reflect.Int, reflect.Int8, reflect.Int16, reflect.Int32, reflect.Int64:
		return &intParser{ }, nil

	case reflect.Uint, reflect.Uint8, reflect.Uint16, reflect.Uint32, reflect.Uint64:
		return &uintParser{ }, nil

	case reflect.Bool:
		return &boolParser{}, nil

	case reflect.Float32, reflect.Float64:
		return &floatParser{ }, nil

	/* TODO: complex numbers */

	case reflect.Slice:
		min := 0

		tmp := tag.Get("repeat")
		if tmp == "*" {
			min = 0
		} else if tmp == "+" {
			min = 1
		}

		delimiter := tag.Get("delimiter")

		p, err := compile(type_of.Elem(), "")
		if err != nil {
			return nil, err
		}

		return &sliceParser{ Min: min, Delimiter: delimiter, Parser: p }, nil

	case reflect.Ptr:
		p, err := compile(type_of.Elem(), tag)
		if err != nil {
			return nil, err
		}

		return &ptrParser{ Parser: p, Optional: (tag.Get("optional") == "true") }, nil
	default:
		return nil, errors.New(fmt.Sprintf("Invalid argument for Compile: unsupported type '%v'", type_of))
	}

	return nil, errors.New(fmt.Sprintf("Invalid argument for Compile: unsupported type '%v'", type_of))
}


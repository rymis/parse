package parse

import (
	"errors"
	"fmt"
	"io"
	"reflect"
	"sync"
	"unicode"
	"unicode/utf8"
)

// Object to hold parser. I need this one because recursive rules compilation.
type proxyParser struct {
	p parser
}

func (self *proxyParser) ParseValue(ctx *parseContext, value_of reflect.Value, location int, err *Error) int {
	if self.p == nil {
		panic("nil parser")
	}

	return self.p.ParseValue(ctx, value_of, location, err)
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

func (self *proxyParser) IsTerm() bool {
	if self.p == nil {
		panic("nil parser")
	}

	return self.p.IsTerm()
}

func (self *proxyParser) IsLRPossible(parsers []parser) (possible bool, can_parse_empty bool) {
	if self.p == nil {
		panic("nil parser")
	}

	return self.p.IsLRPossible(parsers)
}

func (self *proxyParser) IsLR() int {
	if self.p == nil {
		panic("nil parser")
	}

	return self.p.IsLR()
}

func (self *proxyParser) SetLR(v int) {
	if self.p == nil {
		panic("nil parser")
	}

	self.p.SetLR(v)
}

func appendField(type_of reflect.Type, fields *[]field, idx int) error {
	f_type := type_of.Field(idx)

	ptag := f_type.Tag.Get("parse")
	if ptag == "skip" {
		// Skipping
		return nil
	}

	fld := field{Name: f_type.Name, Type: f_type.Type}
	if f_type.Name != "_" {
		r, l := utf8.DecodeRuneInString(f_type.Name)
		if l == 0 || !unicode.IsUpper(r) { // Private field: skipping
			return nil
		}

		fld.Index = idx
	} else {
		fld.Index = -1
	}

	if ptag == "!" {
		fld.Flags |= fieldNotAny
	} else if ptag == "&" {
		fld.Flags |= fieldFollowedBy
	}

	fld.Set = f_type.Tag.Get("set")

	p, err := compileInternal(f_type.Type, f_type.Tag)
	if err != nil {
		return nil
	}

	fld.Parse = p

	*fields = append(*fields, fld)

	return nil
}

// Type and tag for parse keys
type typeAndTag struct {
	Type reflect.Type
	Tag  reflect.StructTag
}

// This map is not so big, because it will contain only type+tag keys.
var _compiledParsers = make(map[typeAndTag]parser)
var _lastId uint = 1
var _compileMutex sync.Mutex

// Compile parser for type. Only one compilation process is possible in the same time.
func compile(type_of reflect.Type, tag reflect.StructTag) (parser, error) {
	_compileMutex.Lock()
	defer _compileMutex.Unlock()

	p, err := compileInternal(type_of, tag)
	if err != nil {
		return nil, err
	}

	isLRPossible(p, nil)
	return p, nil
}

func compileInternal(type_of reflect.Type, tag reflect.StructTag) (parser, error) {
	key := typeAndTag{type_of, tag}
	p, ok := _compiledParsers[key]
	if ok {
		return p, nil
	}

	proxy := &proxyParser{nil}
	_compiledParsers[key] = proxy

	p, err := compileType(type_of, tag)
	if err != nil {
		delete(_compiledParsers, key)
		return nil, err
	}

	p.SetString(fmt.Sprintf("%v `%v`", type_of, tag))
	p.SetId(_lastId)
	_lastId++
	proxy.SetParser(p)

	// It is Ok even if we used p while compiling:
	_compiledParsers[key] = p

	return p, nil
}

func compileType(type_of reflect.Type, tag reflect.StructTag) (p parser, err error) {
	switch type_of.Kind() {
	case reflect.Struct:
		if type_of.NumField() == 0 { // Empty
			return &sequenceParser{Fields: nil}, nil
		}

		fields := make([]field, 0)
		if type_of.Field(0).Type == reflect.TypeOf(FirstOf{}) { // FirstOf
			for i := 1; i < type_of.NumField(); i++ {
				err = appendField(type_of, &fields, i)
				if err != nil {
					return nil, err
				}
			}

			return &firstOfParser{Fields: fields}, nil
		} else {
			for i := 0; i < type_of.NumField(); i++ {
				err = appendField(type_of, &fields, i)

				if err != nil {
					return nil, err
				}
			}

			return &sequenceParser{Fields: fields}, nil
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
		opt := tag.Get("parse")
		if opt == "#" {
			return &locationParser{}, nil
		}

		return &intParser{}, nil

	case reflect.Uint, reflect.Uint8, reflect.Uint16, reflect.Uint32, reflect.Uint64:
		return &uintParser{}, nil

	case reflect.Bool:
		return &boolParser{}, nil

	case reflect.Float32, reflect.Float64:
		return &floatParser{}, nil

	/* TODO: complex numbers */

	case reflect.Slice:
		min := 0

		tmp := tag.Get("parse")
		if tmp == "*" {
			min = 0
		} else if tmp == "+" {
			min = 1
		}

		delimiter := tag.Get("delimiter")

		p, err := compileInternal(type_of.Elem(), "")
		if err != nil {
			return nil, err
		}

		return &sliceParser{Min: min, Delimiter: delimiter, Parser: p}, nil

	case reflect.Ptr:
		p, err := compileInternal(type_of.Elem(), tag)
		if err != nil {
			return nil, err
		}

		return &ptrParser{Parser: p, Optional: (tag.Get("parse") == "?")}, nil
	default:
		return nil, errors.New(fmt.Sprintf("Invalid argument for Compile: unsupported type '%v'", type_of))
	}

	return nil, errors.New(fmt.Sprintf("Invalid argument for Compile: unsupported type '%v'", type_of))
}

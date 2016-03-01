package parse

import (
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

func (par *proxyParser) ParseValue(ctx *parseContext, valueOf reflect.Value, location int, err *Error) int {
	if par.p == nil {
		panic("nil parser")
	}

	return par.p.ParseValue(ctx, valueOf, location, err)
}

func (par *proxyParser) WriteValue(out io.Writer, valueOf reflect.Value) error {
	if par.p == nil {
		panic("nil parser")
	}

	return par.p.WriteValue(out, valueOf)
}

func (par *proxyParser) SetID(id uint) {
	if par.p == nil {
		panic("nil parser")
	}

	par.p.SetID(id)
}

func (par *proxyParser) ID() uint {
	if par.p == nil {
		panic("nil parser")
	}

	return par.p.ID()
}

func (par *proxyParser) String() string {
	if par.p == nil {
		panic("nil parser")
	}

	return par.p.String()
}

func (par *proxyParser) SetString(nm string) {
	if par.p == nil {
		panic("nil parser")
	}

	par.p.SetString(nm)
}

func (par *proxyParser) SetParser(p parser) {
	if par.p != nil {
		panic("Trying to change parser in proxy object")
	}

	par.p = p
}

func (par *proxyParser) IsTerm() bool {
	if par.p == nil {
		panic("nil parser")
	}

	return par.p.IsTerm()
}

func (par *proxyParser) IsLRPossible(parsers []parser) (possible bool, canParseEmpty bool) {
	if par.p == nil {
		panic("nil parser")
	}

	return par.p.IsLRPossible(parsers)
}

func (par *proxyParser) IsLR() int {
	if par.p == nil {
		panic("nil parser")
	}

	return par.p.IsLR()
}

func (par *proxyParser) SetLR(v int) {
	if par.p == nil {
		panic("nil parser")
	}

	par.p.SetLR(v)
}

func appendField(typeOf reflect.Type, fields *[]field, idx int) error {
	fType := typeOf.Field(idx)

	ptag := fType.Tag.Get("parse")
	if ptag == "skip" {
		// Skipping
		return nil
	}

	fld := field{Name: fType.Name, Type: fType.Type}
	if fType.Name != "_" {
		r, l := utf8.DecodeRuneInString(fType.Name)
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

	fld.Set = fType.Tag.Get("set")

	p, err := compileInternal(fType.Type, fType.Tag)
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
var _lastID uint = 1
var _compileMutex sync.Mutex

// Compile parser for type. Only one compilation process is possible in the same time.
func compile(typeOf reflect.Type, tag reflect.StructTag) (parser, error) {
	_compileMutex.Lock()
	defer _compileMutex.Unlock()

	p, err := compileInternal(typeOf, tag)
	if err != nil {
		return nil, err
	}

	isLRPossible(p, nil)
	// Try to find all parsers with LR is not set:
	for _, par := range _compiledParsers {
		if par.IsLR() == 0 {
			isLRPossible(par, nil)
		}
	}

	return p, nil
}

func compileInternal(typeOf reflect.Type, tag reflect.StructTag) (parser, error) {
	key := typeAndTag{typeOf, tag}
	p, ok := _compiledParsers[key]
	if ok {
		return p, nil
	}

	proxy := &proxyParser{nil}
	_compiledParsers[key] = proxy

	p, err := compileType(typeOf, tag)
	if err != nil {
		delete(_compiledParsers, key)
		return nil, err
	}

	p.SetString(fmt.Sprintf("%v `%v`", typeOf, tag))
	p.SetID(_lastID)
	_lastID++
	proxy.SetParser(p)

	// It is Ok even if we used p while compiling:
	_compiledParsers[key] = p

	return p, nil
}

var _parserType = reflect.TypeOf((*Parser)(nil)).Elem()

func compileType(typeOf reflect.Type, tag reflect.StructTag) (p parser, err error) {
	// Check if field has type that implements parser:
	if typeOf.Implements(_parserType) {
		return &parserParser{ptr: false}, nil
	} else if typeOf.Kind() != reflect.Ptr && reflect.PtrTo(typeOf).Implements(_parserType) {
		return &parserParser{ptr: true}, nil
	}

	switch typeOf.Kind() {
	case reflect.Struct:
		if typeOf.NumField() == 0 { // Empty
			return &sequenceParser{Fields: nil}, nil
		}

		fields := []field{}
		if typeOf.Field(0).Type == reflect.TypeOf(FirstOf{}) { // FirstOf
			for i := 1; i < typeOf.NumField(); i++ {
				err = appendField(typeOf, &fields, i)
				if err != nil {
					return nil, err
				}
			}

			return &firstOfParser{Fields: fields}, nil
		}

		for i := 0; i < typeOf.NumField(); i++ {
			err = appendField(typeOf, &fields, i)

			if err != nil {
				return nil, err
			}
		}

		return &sequenceParser{Fields: fields}, nil

	case reflect.String:
		rx := tag.Get("regexp")
		if rx == "" {
			lit := tag.Get("literal")
			if lit == "" {
				return &stringParser{}, nil
			}

			return newLiteralParser(lit), nil
		}

		return newRegexpParser(rx)

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

		p, err := compileInternal(typeOf.Elem(), "")
		if err != nil {
			return nil, err
		}

		return &sliceParser{Min: min, Delimiter: delimiter, Parser: p}, nil

	case reflect.Ptr:
		p, err := compileInternal(typeOf.Elem(), tag)
		if err != nil {
			return nil, err
		}

		return &ptrParser{Parser: p, Optional: (tag.Get("parse") == "?")}, nil
	default:
		return nil, fmt.Errorf("Invalid argument for Compile: unsupported type '%v'", typeOf)
	}
}

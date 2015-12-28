// Easy to use PEG implementation with Go
package parse

import (
	"reflect"
	"fmt"
	"errors"
	"regexp"
	"bytes"
)

// Parse error representation. Here str is original string, location - location of error in source string, message is error message.
type Error struct {
	str []byte
	location int
	message string
}

// This is empty structure that indicates that we need to parse first expression of the fields of structure
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

	return fmt.Sprintf("Syntax error at line %d:%d: %s\n%s", lineno, col, self.message, self.str[start:i])
}

type context struct {
	str []byte
	skip_ws_f func (str []byte, loc int) int
}

func (ctx *context) parse_regexp(location int, rx string) (string, int, error) {
	r := regexp.MustCompile(rx)
	m := r.Find(ctx.str[location:])
	if m == nil {
		return "", location, Error{ctx.str, location, "Waiting for '" + rx + "'"}
	}

	// It is must be at start:
	if bytes.Compare(m, ctx.str[location: location + len(m)]) != 0 {
		return "", location, Error{ctx.str, location, "Waiting for '" + rx + "'"}
	}

	return string(m), location + len(m), nil
}

// Skip whitespace:
func (ctx *context) skip_ws(loc int) int {
	l := ctx.skip_ws_f(ctx.str, loc)
	if l >= loc {
		return l
	}
	return loc
}

// Internal parse function
func (ctx *context) parse_int(value_of reflect.Value, tag reflect.StructTag, location int) (new_loc int, err error) {
	type_of := value_of.Type()

	location = ctx.skip_ws(location)

	if !value_of.CanSet() {
		return -1, errors.New(fmt.Sprintf("Invalid argument for parse_int: can't set (%v: %v)", value_of, type_of))
	}

	switch value_of.Kind() {
	case reflect.Struct:
		if value_of.NumField() == 0 { // Empty
			return location, nil
		}

		first_of := false
		followed_by := false
		not_any := false

		idx_start := 0
		if type_of.Field(0).Type == reflect.TypeOf(FirstOf{}) {
			first_of = true
			idx_start = 1
		}

		l := location
		max_error := Error{ ctx.str, location - 1, "No choices in first of" }
		for i := idx_start; i < value_of.NumField(); i++ {
			f := value_of.Field(i)
			not_any = (type_of.Field(i).Tag.Get("not_any") == "true")
			followed_by = (type_of.Field(i).Tag.Get("followed_by") == "true")

			if !f.CanSet() {
				if type_of.Field(i).Name == "_" { // Ignorable value
					f = reflect.New(type_of.Field(i).Type).Elem()
				} else {
					return location, errors.New(fmt.Sprintf("Can't set field '%v.%s'", type_of, type_of.Field(i).Name))
				}
			}

			l, err = ctx.parse_int(f, type_of.Field(i).Tag, location)
			if first_of {
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
						return location, err
					}

					if i == value_of.NumField() - 1 {
						return location, max_error
					}
				}
			} else {
				if not_any {
					if err == nil {
						return location, Error{ctx.str, location, "Unexpected input"}
					}
					// Don't change the location
				} else if followed_by {
					if err != nil {
						return l, err
					}
					// Don't change the location
				} else {
					if err != nil {
						return l, err
					}

					location = ctx.skip_ws(l)
				}
			}

		}

		return l, nil

	case reflect.String:
		rx := tag.Get("regexp")
		if rx == "" {
			return -1, errors.New(fmt.Sprintf("String fields must contain regular expression (tag is '%s')", string(tag)))
		}

		var s string
		s, location, err = ctx.parse_regexp(location, rx)
		if err != nil {
			return location, err
		}

		value_of.SetString(s)

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
	type_of := reflect.TypeOf(result)
	value_of := reflect.ValueOf(result)

	if type_of.Kind() != reflect.Ptr {
		return -1, errors.New("Invalid argument for Parse: waiting for pointer")
	}

	ctx := context{ str, ignore }
	new_location, err = ctx.parse_int(value_of.Elem(), reflect.StructTag(""), 0)
	return
}


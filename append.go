package parse

import (
	"reflect"
	"io"
	"fmt"
	"errors"
	"regexp"
	"strconv"
	"unicode"
	"unicode/utf8"
)

// Output-related functions for parse library

func outValue(out io.Writer, value_of reflect.Value, tag reflect.StructTag) error {
	var err error
	type_of := value_of.Type()

	switch value_of.Kind() {
	case reflect.Struct:
		if value_of.NumField() == 0 { // Empty
			return nil
		}

		if type_of.Field(0).Type == reflect.TypeOf(FirstOf{}) {
			nm := value_of.Field(0).Field(0).String()
			if nm == "" {
				return errors.New("Field is not selected in FirstOf")
			}

			for i := 1; i < value_of.NumField(); i++ {
				field := type_of.Field(i)
				if field.Name == nm {
					return outField(out, value_of, i)
				}
			}

			return errors.New(fmt.Sprintf("Field `%s' is not present in %v", nm, type_of))
		} else {
			for i := 0; i < value_of.NumField(); i++ {
				err = outField(out, value_of, i)
				if err != nil {
					return err
				}
			}
		}

		return nil

	case reflect.String:
		lit := tag.Get("literal")
		if lit == "" {
			rx := tag.Get("regexp")
			if rx != "" {
				ok, err := regexp.MatchString(rx, value_of.String())
				if ok {
					_, err = out.Write([]byte(value_of.String()))
					return err
				} else if err != nil {
					return err
				} else {
					return errors.New("String doesn't match /" + rx + "/")
				}
			} else {
				_, err = out.Write(strconv.AppendQuote(nil, value_of.String()))
				return err
			}
		} else {
			_, err = out.Write([]byte(lit))
			return err
		}

		return nil // Not reached

	case reflect.Int, reflect.Int8, reflect.Int16, reflect.Int32, reflect.Int64:
		_, err = out.Write(strconv.AppendInt(nil, value_of.Int(), 10))
		return err

	case reflect.Uint, reflect.Uint8, reflect.Uint16, reflect.Uint32, reflect.Uint64:
		_, err = out.Write(strconv.AppendUint(nil, value_of.Uint(), 10))
		return err

	case reflect.Bool:
		if value_of.Bool() {
			_, err = out.Write([]byte("true"))
		} else {
			_, err = out.Write([]byte("false"))
		}

		return err

	case reflect.Float32, reflect.Float64:
		_, err = out.Write(strconv.AppendFloat(nil, value_of.Float(), 'e', -1, type_of.Bits()))
		return err

	/* TODO: complex numbers */

	case reflect.Slice:
		min := 0

		tmp := tag.Get("repeat")
		if tmp == "*" {
			min = 0
		} else if tmp == "+" {
			min = 1
		}

		if value_of.Len() < min {
			return errors.New("Not enought elements in slice")
		}

		delimiter := tag.Get("delimiter")

		for i := 0; i < value_of.Len(); i++ {
			if i > 0 && len(delimiter) > 0 {
				_, err = out.Write([]byte(delimiter))
				if err != nil {
					return err
				}
			}

			v := value_of.Index(i)
			err = outValue(out, v, "")
			if err != nil {
				return err
			}
		}

		return nil

	case reflect.Ptr:
		if value_of.IsNil() {
			if tag.Get("optional") != "true" {
				return errors.New("Not optional value is nil")
			}
			return nil
		}

		return outValue(out, value_of.Elem(), tag)
	default:
		return errors.New(fmt.Sprintf("Invalid argument for outValue: unsupported type '%v'", type_of))
	}

	return nil
}

func outField(out io.Writer, value_of reflect.Value, idx int) error {
	type_of := value_of.Type()
	f_type := type_of.Field(idx)

	if f_type.Tag.Get("skip") == "true" {
		// Skip this field
		return nil
	}

	var f reflect.Value
	if f_type.Name != "_" {
		r, l := utf8.DecodeRuneInString(f_type.Name)
		if l == 0 || !unicode.IsUpper(r) { // Private field
			return nil
		}

		f = value_of.Field(idx)
	} else {
		f = reflect.New(f_type.Type).Elem()
	}

	if f_type.Tag.Get("not_any") == "true" {
		return nil
	}

	if f_type.Tag.Get("followed_by") == "true" {
		return nil
	}

	if f_type.Name == "_" {
		t := f_type.Type
		for t.Kind() == reflect.Ptr {
			t = t.Elem()
		}

		if t.Kind() == reflect.String {
			lit := f_type.Tag.Get("literal")
			if len(lit) > 0 {
				_, err := out.Write([]byte(lit))
				return err
			} else {
				return errors.New("Can't out anonymous field without literal tag")
			}
		} else {
			return errors.New("Can't out anonymous field of non-string type")
		}
	} else {
		return outValue(out, f, f_type.Tag)
	}

	return nil
}

// Write encoded value into output stream.
func Write(out io.Writer, value interface{}) error {
	return outValue(out, reflect.ValueOf(value), "")
}

type appender struct {
	buf []byte
}

func (self *appender) Write(data []byte) (int, error) {
	self.buf = append(self.buf, data...)
	return len(data), nil
}

// Append encoded value to slice.
// Function returns new slice.
func Append(array []byte, value interface{}) ([]byte, error) {
	x := &appender{array}
	err := Write(x, value)
	if err != nil {
		return nil, err
	}

	return x.buf, nil
}



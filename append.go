package parse

import (
	"io"
	"reflect"
)

// Write encoded value into output stream.
func Write(out io.Writer, value interface{}) error {
	valueOf := reflect.ValueOf(value)
	typeOf := valueOf.Type()

	p, err := compile(typeOf, reflect.StructTag(""))
	if err != nil {
		return err
	}

	return p.WriteValue(out, valueOf)
}

type appender struct {
	buf []byte
}

func (a *appender) Write(data []byte) (int, error) {
	a.buf = append(a.buf, data...)
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

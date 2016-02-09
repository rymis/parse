package parse

import (
	"io"
	"reflect"
)

// Write encoded value into output stream.
func Write(out io.Writer, value interface{}) error {
	value_of := reflect.ValueOf(value)
	type_of := value_of.Type()

	p, err := compile(type_of, reflect.StructTag(""))
	if err != nil {
		return err
	}

	return p.WriteValue(out, value_of)
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

parse is Go implementation of PEG parser.
=========================================

[![Go Reference](https://pkg.go.dev/badge/github.com/rymis/parse.svg)](https://pkg.go.dev/github.com/rymis/parse)

This is simple Go parser that uses mapping from Go types to PEG language definitions.

Simple example:
``` Go
type Hello struct {
	Hello string `regexp:"[hH]ello"`
	_     string `literal:","`
	Name  string `regexp:"[a-zA-Z]+"`
}
...
var hello Hello
new_location, err := parse.Parse(&hello, []byte("Hello, user"), nil)
```

Documentation is here: https://godoc.org/github.com/rymis/parse
And user-friendly examples and book are placed [here](https://github.com/rymis/parse_examples/blob/master/book.md).

[![Go Report Card](https://goreportcard.com/badge/github.com/rymis/parse)](https://goreportcard.com/report/github.com/rymis/parse)


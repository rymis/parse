parse is Go implementation of PEG parser.
=========================================

This is simple Go parser that uses mapping from Go types to PEG language definitions.

Simple example:
```
type Hello struct {
	Hello string `regexp:"[hH]ello"`
	_     string `literal:","`
	Name  string `regexp:"[a-zA-Z]+"`
}
...
var hello Hello
new_location, err := parse.Parse(&hello, []byte("Hello, user"), nil)
```


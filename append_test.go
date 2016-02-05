package parse

import (
	"testing"
	"fmt"
)

type spaces struct {
	Spaces string `regexp:"[ \\n\\t\\r]*"`
}

type white struct {
	White string `regexp:"([ \\t\\r\\n]*|#[^\n]*\n)*"`
}

type config struct {
	Sections []section `parse:"*"`
	W          white
//	Eof        string `parse:"!" regexp:".|\\n"`
}

type section struct {
	White      white
	Name       string `regexp:"[a-zA-Z][a-zA-Z0-9_]*"`
	W1         white
	_          string `literal:"{"`
	Pairs    []pair
	W2         spaces
	_          string `literal:"}"`
}

type pair struct {
	White      white
	Name       string `regexp:"[a-zA-Z][a-zA-Z0-9_]*"`
	W1         spaces
	_          string `literal:"="`
	W2         spaces
	Value      value
	W3         spaces
}

type value struct {
	FirstOf
	Int        int64
	String     string
	Bool       bool
	Array      array
	RawString  string `regexp:"[^\n]*\n"`
}

type array struct {
	_          string `literal:"["`
	Values   []value  `delimiter:","`
	W2         white
	_          string `literal:"]"`
}

func skip(str []byte, loc int) int {
	for i := loc; i < len(str); i++ {
		if str[i] != ' ' && str[i] != '\t' && str[i] != '\r' {
			return i
		}
	}

	return len(str)
}

var test1 string = `
Section {
	name = 1
	name = "String"
	name = true
	name = Raw string
	name = [ 1, 2 ]
}

Section0 {
	name1 = [ 1   ,  2 , 3 ]
	name2 = 2
	name3 = "String"
	name4 = true
}

Section1 {
	int = -5
	bool = false
	string = "Hello, world!"
	raw_string = this is raw string
	array = [ 1, 2, false ]
}

Section2 {
	test = [ 1, true, [ 2, false ], "The End!" ]
}

`

func TestAppend(t *testing.T) {
	var cfg config
	nl, err := Parse(&cfg, []byte(test1), &Options{Debug:true})
	if err != nil {
		fmt.Printf("Error(%d): %v\n", nl, err)
		return
	}
	println(nl)

	res, err := Append(nil, cfg)
	if err != nil {
		fmt.Printf("Error: %v\n", err)
		return
	}

	fmt.Printf("CONFIG:\n%s\n", string(res))
}


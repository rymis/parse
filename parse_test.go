package parse

import (
	"errors"
	"fmt"
	"io"
	"testing"
)

type myEOF struct {
}

func (eof *myEOF) ParseValue(str []byte, loc int) (int, error) {
	if loc < len(str) {
		fmt.Printf("[ERROR] ##### EOF checker!\n")
		return -1, errors.New("Waiting for end of file")
	}
	fmt.Printf("[OK] ##### EOF checker!\n")

	return 0, nil
}

func (eof *myEOF) WriteValue(out io.Writer) error {
	fmt.Printf("##### EOF writer!\n")
	return nil
}

type tmp struct {
	A   string  `regexp:"[hH]ello"`
	_   string  `literal:","`
	_   *string `set:"SetW" parse:"?" regexp:"[wW]orld"`
	Loc int     `parse:"#"`
	w   string
	EOF myEOF
}

func (t *tmp) SetW(v *string) error {
	t.w = *v
	fmt.Printf("SET: %s\n", t.w)
	return nil
}

/* Simple arithmetic expressions grammar:

EXPR <- MUL ([+-] MUL)*
MUL  <- ATOM ([/%*] ATOM)*
ATOM <- '(' EXPR ')' / NUMBER
NUMBER <- [1-9][0-9]*
*/

type bracedExpression struct {
	_    string `regexp:"\\("`
	Expr *expression
	_    string `regexp:"\\)"`
}

type atom struct {
	FirstOf

	Expr   bracedExpression
	Number int64
}

func (a atom) p() {
	if a.Field == "Number" {
		print(a.Number)
	} else {
		a.Expr.Expr.p()
	}
}

type mexpression struct {
	First atom
	Rest  []struct {
		Op  string `regexp:"[*%/]"`
		Arg atom
	} `parse:"*"`
}

func (m mexpression) p() {
	m.First.p()
	print(" ")

	for i := 0; i < len(m.Rest); i++ {
		m.Rest[i].Arg.p()
		print(" ")
		print(m.Rest[i].Op)
	}
}

type expression struct {
	First mexpression
	Rest  []struct {
		Op  string `regexp:"[-+]"`
		Arg mexpression
	} `parse:"*"`
}

func (e expression) p() {
	e.First.p()
	print(" ")

	for i := 0; i < len(e.Rest); i++ {
		print(" ")
		e.Rest[i].Arg.p()
		print(" ")
		print(e.Rest[i].Op)
	}
}

/* Example from Wikipedia:
S ← &(A 'c') 'a'+ B !('a'/'b'/'c')
A ← 'a' A? 'b'
B ← 'b' B? 'c'
*/

type abcS struct {
	_ struct {
		A abcA
		C string `regexp:"c"`
	} `parse:"&"`
	A []struct {
		A string `regexp:"a"`
	} `parse:"+"`
	B abcB
	_ struct {
		FirstOf
		A string `regexp:"a"`
		B string `regexp:"b"`
		C string `regexp:"c"`
	} `parse:"!"`
}

type abcA struct {
	A  string `regexp:"a"`
	A1 *abcA  `parse:"?"`
	B  string `regexp:"b"`
}

type abcB struct {
	B  string `regexp:"b"`
	B1 *abcB  `parse:"?"`
	C  string `regexp:"c"`
}

func TestParse(t *testing.T) {
	var x tmp
	l, e := Parse(&x, []byte("Hello    , \n\tworld"), nil)
	if e != nil {
		fmt.Printf("Error: %v\n", e)
	} else {
		fmt.Printf("New location: %d\n", l)
		fmt.Println(x)
	}

	var ex expression
	l, e = Parse(&ex, []byte("12 + (56 * 3) % 10"), nil)

	if e != nil {
		fmt.Printf("Error: %v\n", e)
	} else {
		fmt.Printf("New location: %d\n", l)
		fmt.Println(ex)
		ex.p()
		println("")
	}

	for _, s := range []string{"aabbcc", "", "abc", "aabbc", "aabcc"} {
		var g abcS
		_, e = Parse(&g, []byte(s), &Options{PackratEnabled: true})
		fmt.Printf("EXPR: %s\n", s)
		if e != nil {
			fmt.Printf("ERROR: %s\n", e.Error())
		}
	}
}

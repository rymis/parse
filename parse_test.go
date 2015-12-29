package parse

import (
	"testing"
	"fmt"
)

type tmp struct {
	A string `regexp:"[hH]ello"`
	_ string `regexp:","`
	_ *string `set:"Set_w" optional:"true" regexp:"[wW]orld"`
	w string
}

func (self *tmp) Set_w(v *string) error {
	self.w = *v
	fmt.Printf("SET: %s\n", self.w)
	return nil
}

/* Simple arithmetic expressions grammar:

EXPR <- MUL ([+-] MUL)*
MUL  <- ATOM ([/%*] ATOM)*
ATOM <- '(' EXPR ')' / NUMBER
NUMBER <- [1-9][0-9]*
*/

type braced_expression struct {
	_ string `regexp:"\\("`
	Expr *expression
	_ string `regexp:"\\)"`
}

type atom struct {
	FirstOf

	Expr braced_expression
	Number string `regexp:"[1-9][0-9]*"`
}

func (self atom) p() {
	if self.Field == "Number" {
		print(self.Number)
	} else {
		self.Expr.Expr.p()
	}
}

type mexpression struct {
	First atom
	Rest []struct {
		Op string `regexp:"[*%/]"`
		Arg atom
	} `repeat: "*"`
}

func (self mexpression) p() {
	self.First.p()
	print(" ")

	for i := 0; i < len(self.Rest); i++ {
		self.Rest[i].Arg.p()
		print(" ")
		print(self.Rest[i].Op)
	}
}

type expression struct {
	First mexpression
	Rest []struct {
		Op string `regexp:"[-+]"`
		Arg mexpression
	} `repeat: "*"`
}

func (self expression) p() {
	self.First.p()
	print(" ")

	for i := 0; i < len(self.Rest); i++ {
		print(" ")
		self.Rest[i].Arg.p()
		print(" ")
		print(self.Rest[i].Op)
	}
}

/* Example from Wikipedia:
S ← &(A 'c') 'a'+ B !('a'/'b'/'c')
A ← 'a' A? 'b'
B ← 'b' B? 'c'
*/

type abc_S struct {
	_ struct {
		A abc_A
		C string `regexp:"c"`
	} `followed_by:"true"`
	A []struct {
		A string `regexp:"a"`
	} `repeat:"+"`
	B abc_B
	_ struct {
		FirstOf
		A string `regexp:"a"`
		B string `regexp:"b"`
		C string `regexp:"c"`
	} `not_any:"true"`
}

type abc_A struct {
	A string `regexp:"a"`
	A1 *abc_A `optional:"true"`
	B string `regexp:"b"`
}

type abc_B struct {
	B string `regexp:"b"`
	B1 *abc_B `optional:"true"`
	C string `regexp:"c"`
}

func TestParse(t *testing.T) {
	var x tmp
	l, e := Parse(&x, []byte("Hello    , \n\tworld"))
	if e != nil {
		fmt.Printf("Error: %v\n", e)
	} else {
		fmt.Printf("New location: %d\n", l)
		fmt.Println(x)
	}

	var ex expression
	l, e = Parse(&ex, []byte("12 + (56 * 3) % 10"))

	if e != nil {
		fmt.Printf("Error: %v\n", e)
	} else {
		fmt.Printf("New location: %d\n", l)
		fmt.Println(ex)
		ex.p()
		println("")
	}

	for _, s := range([]string{"aabbcc", "", "abc", "aabbc", "aabcc"}) {
		var g abc_S
		_, e = Parse(&g, []byte(s))
		fmt.Printf("EXPR: %s\n", s)
		if e != nil {
			fmt.Printf("ERROR: %s\n", e.Error())
		}
	}
}


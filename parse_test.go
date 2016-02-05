package parse

import (
	"testing"
	"fmt"
)

type tmp struct {
	A string `regexp:"[hH]ello"`
	_ string `literal:","`
	_ *string `set:"Set_w" parse:"?" regexp:"[wW]orld"`
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
	Number int64
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
	} `parse: "*"`
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
	} `parse: "*"`
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
	} `parse:"&"`
	A []struct {
		A string `regexp:"a"`
	} `parse:"+"`
	B abc_B
	_ struct {
		FirstOf
		A string `regexp:"a"`
		B string `regexp:"b"`
		C string `regexp:"c"`
	} `parse:"!"`
}

type abc_A struct {
	A string `regexp:"a"`
	A1 *abc_A `parse:"?"`
	B string `regexp:"b"`
}

type abc_B struct {
	B string `regexp:"b"`
	B1 *abc_B `parse:"?"`
	C string `regexp:"c"`
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

	for _, s := range([]string{"aabbcc", "", "abc", "aabbc", "aabcc"}) {
		var g abc_S
		_, e = Parse(&g, []byte(s), &Options{PackratEnabled: true})
		fmt.Printf("EXPR: %s\n", s)
		if e != nil {
			fmt.Printf("ERROR: %s\n", e.Error())
		}
	}
}


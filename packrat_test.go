package parse

import (
	"fmt"
	"testing"
)

/* Left recursion:
Expression <- Expression ([+-] MultiplicativeExpression)? / MultiplicativeExpression
MultiplicativeExpression  <- MultiplicativeExpression ([*%/] Atom)? / Atom
Atom <- '(' Expression ')' / Int64
*/

type BracedExpression struct {
	_    string `regexp:"\\("`
	Expr Expression
	_    string `regexp:"\\)"`
}

type Atom struct {
	FirstOf
	Expr BracedExpression
	Val  int64
}

type Mul1 struct {
	Mul MultiplicativeExpression
	Arg *struct {
		Op   string `regexp:"[/%*]"`
		Atom Atom
	} `parse:"?"`
}

type MultiplicativeExpression struct {
	FirstOf
	Mul  *Mul1
	Atom Atom
}

func printMul(m *MultiplicativeExpression) {
	if m.Field == "Mul" {
		if m.Mul.Arg != nil {
			fmt.Printf("%s(", m.Mul.Arg.Op)
			printMul(&m.Mul.Mul)

			if m.Mul.Arg.Atom.Field == "Val" {
				fmt.Printf("%d ", m.Mul.Arg.Atom.Val)
			} else {
				printExpression(&m.Mul.Arg.Atom.Expr.Expr)
			}
			fmt.Print(")")
		} else {
			printMul(&m.Mul.Mul)
		}
	} else {
		if m.Atom.Field == "Val" {
			fmt.Printf("%d ", m.Atom.Val)
		} else {
			printExpression(&m.Atom.Expr.Expr)
		}
	}
}

type Expression1 struct {
	Expr Expression
	Arg  *struct {
		Op  string `regexp:"[-+]"`
		Mul MultiplicativeExpression
	} `parse:"?"`
}

type Expression struct {
	FirstOf
	Expr *Expression1
	Mul  *MultiplicativeExpression
}

func printExpression(e *Expression) {
	if e.Field == "Expr" {
		if e.Expr.Arg != nil {
			fmt.Printf("%s(", e.Expr.Arg.Op)
			printExpression(&e.Expr.Expr)
			printMul(&e.Expr.Arg.Mul)
			fmt.Print(")")
		} else {
			printExpression(&e.Expr.Expr)
		}
	} else {
		printMul(e.Mul)
	}
}

/* Direct recursion test: */
type Rt struct {
	FirstOf

	A *struct {
		A Rt
		_ string `regexp:"-"`
		N string `regexp:"[0-9]+"`
	}
	N string `regexp:"[0-9]+"`
}

func (r Rt) Print() {
	if r.Field == "A" {
		r.A.A.Print()
		fmt.Print("-", r.A.N)
	} else {
		fmt.Print(r.N)
	}
}

/* Indirect recursion test: */
type Xt struct {
	E *Et
}

type Et struct {
	FirstOf
	M struct {
		X Xt
		_ string `regexp:"-"`
		N string `regexp:"[0-9]+"`
	}
	N string `regexp:"[0-9]+"`
}

func (x Xt) Print() {
	x.E.Print()
}

func (e Et) Print() {
	if e.Field == "M" {
		e.M.X.Print()
		fmt.Print("-", e.M.N)
	} else {
		fmt.Print(e.N)
	}
}

func TestPackrat(t *testing.T) {
	params := NewOptions()
	params.PackratEnabled = true
	params.Debug = true

	var l int
	var e error

	if true {
		var expr Expression
		l, e = Parse(&expr, []byte("  10 + 5 - 3 * 2 % 2"), params)

		fmt.Printf("New location: %d, error: %v\n", l, e)
		printExpression(&expr)
		println("")
	}

	if true {
		var expr Expression
		l, e = Parse(&expr, []byte("1 * 2 * 3 * 4 * 5 + 2 * 3 * 4 * 5 * 6 + 3 * 4 * 5 * 6 * 7 + 4 * 5 * 6 * 7 * 8 + 5 * 6 * 7 * 8 * 9"), params)

		fmt.Printf("New location: %d, error: %v\n", l, e)
		printExpression(&expr)
		println("")
	}

	if true {
		var x Xt
		Parse(&x, []byte("  1 - 2 - 3 - 4 - 5"), params)
		fmt.Printf("New location: %d, error: %v\n", l, e)
		x.Print()
		fmt.Println("")
	}

	if true {
		var r Rt
		Parse(&r, []byte("1 - 2 - 3 - 4 - 5"), params)
		fmt.Printf("New location: %d, error: %v\n", l, e)
		r.Print()
		fmt.Println("")
	}
}

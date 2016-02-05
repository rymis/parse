package parse

import (
	"testing"
	"fmt"
)

/* Left recursion:
EXPR <- EXPR ([+-] MUL)? / MUL
MUL  <- MUL ([*%/] ATOM)? / ATOM
ATOM <- '(' EXPR ')' / Int64
*/

type BEXPR struct {
	_ string `regexp:"\\("`
	Expr EXPR
	_ string `regexp:"\\)"`
}

type ATOM struct {
	FirstOf
	Expr BEXPR
	Val  int64
}

type MUL_1 struct {
	Mul MUL
	Arg *struct {
		Op   string `regexp:"[/%*]"`
		Atom ATOM
	} `parse:"?"`
}

type MUL struct {
	FirstOf
	Mul *MUL_1
	Atom ATOM
}

func print_mul(m *MUL) {
	if m.Field == "Mul" {
		if m.Mul.Arg != nil {
			fmt.Printf("%s(", m.Mul.Arg.Op)
			print_mul(&m.Mul.Mul)

			if m.Mul.Arg.Atom.Field == "Val" {
				fmt.Printf("%d ", m.Mul.Arg.Atom.Val)
			} else {
				print_expr(&m.Mul.Arg.Atom.Expr.Expr)
			}
			fmt.Print(")")
		} else {
			print_mul(&m.Mul.Mul)
		}
	} else {
		if m.Atom.Field == "Val" {
			fmt.Printf("%d ", m.Atom.Val)
		} else {
			print_expr(&m.Atom.Expr.Expr)
		}
	}
}

type EXPR_1 struct {
	Expr EXPR
	Arg *struct {
		Op   string `regexp:"[-+]"`
		Mul  MUL
	} `parse:"?"`
}

type EXPR struct {
	FirstOf
	Expr *EXPR_1
	Mul  *MUL
}

func print_expr(e *EXPR) {
	if e.Field == "Expr" {
		if e.Expr.Arg != nil {
			fmt.Printf("%s(", e.Expr.Arg.Op)
			print_expr(&e.Expr.Expr)
			print_mul(&e.Expr.Arg.Mul)
			fmt.Print(")")
		} else {
			print_expr(&e.Expr.Expr)
		}
	} else {
		print_mul(e.Mul)
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
		var expr EXPR
		l, e = Parse(&expr, []byte("  10 + 5 - 3 * 2 % 2"), params)

		fmt.Printf("New location: %d, error: %v\n", l, e)
		print_expr(&expr)
		println("")
	}

	if true {
		var expr EXPR
		l, e = Parse(&expr, []byte("1 * 2 * 3 * 4 * 5 + 2 * 3 * 4 * 5 * 6 + 3 * 4 * 5 * 6 * 7 + 4 * 5 * 6 * 7 * 8 + 5 * 6 * 7 * 8 * 9"), params)

		fmt.Printf("New location: %d, error: %v\n", l, e)
		print_expr(&expr)
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


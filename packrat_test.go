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
	} `optional:"true"`
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
	} `optional:"true"`
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

func TestPackrat(t *testing.T) {
	ctx := New()
	ctx.SetPackrat(true)
	ctx.SetDebug(true)

	var expr EXPR
	l, e := ctx.Parse(&expr, []byte("  10 + 5 - 3 * 2 % 2"))

	fmt.Printf("New location: %d, error: %v\n", l, e)
	print_expr(&expr)
	println("")
}


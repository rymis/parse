package parse

import (
	"testing"
)

/* Left recursion:
EXPR <- EXPR [+-] MUL / MUL
MUL  <- ATOM [*%/] MUL / ATOM
ATOM <- '(' EXPR ')' / Int64
*/
type BEXPR struct {
	_ string `regexp:"\\("`
	expr *EXPR
	_ string `regexp:"\\)"`
}

type ATOM struct {
	FirstOf
	expr BEXPR
	val  int64
}

type MUL struct {

}

func TestPackrat(t *testing.T) {
	ctx := New()
	ctx.SetPackrat(true)

}


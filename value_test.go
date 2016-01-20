package parse

import (
	"testing"
	"fmt"
)

type s_tst struct {
	input, result string
	ok bool
}

var s_tests []s_tst = []s_tst {
	{ "`abc`", "abc", true },
	{ "`\\n\n\\n`", "\\n\n\\n", true },
	{ "\"\\\"\"", "\"", true },
	{ `"Hello, world!\n"`, "Hello, world!\n", true },
	{ `"日本語"`, "日本語", true },
	{ "\"\\u65e5本\\U00008a9e\"", "\u65e5本\U00008a9e", true },
	{ "\"\\xff\\u00FF\"", "\xff\u00FF", true },
	{ "\"\\uD800\"", "", false },            // illegal: surrogate half
	{ "\"\\U00110000\"", "", false },         // illegal: invalid Unicode code point
	{ "\"日本語\"", "日本語", true },                                 // UTF-8 input text
	{ "`日本語`", "日本語", true },                                 // UTF-8 input text as a raw literal
	{ "\"\\u65e5\\u672c\\u8a9e\"", "日本語", true },                    // the explicit Unicode code points
	{ "\"\\U000065e5\\U0000672c\\U00008a9e\"", "日本語", true },        // the explicit Unicode code points
	{ "\"\\xe6\\x97\\xa5\\xe6\\x9c\\xac\\xe8\\xaa\\x9e\"", "日本語", true },  // the explicit UTF-8 bytes
	{ "\"\\xzz\"", "", false },
	{ "\"....", "", false },
	{ "`.......", "", false },
}

func TestString(t *testing.T) {
	fmt.Println("Test string parsers")

	for i, t := range(s_tests) {
		var s string

		fmt.Printf("TEST [%d] ", i)
		l, err := Parse(&s, []byte(t.input), nil)
		if err != nil {
			if t.ok {
				fmt.Printf("ERROR: `%s` %s\n", t.input, err.Error())
			} else {
				fmt.Printf("OK (%d): %s\n", l, err.Error())
			}
		} else {
			if !t.ok {
				fmt.Printf("ERROR (%d): parsed %s\n", l, s)
			} else {
				if s == t.result {
					fmt.Printf("OK (%d): %s\n", l, s)
				} else {
					fmt.Printf("ERROR (%d): '%s' != '%s'\n", l, s, t.result)
				}
			}
		}
	}
}

type i_tst struct {
	input string
	sresult int64
	uresult uint64
	ok bool
}

var i_tests []i_tst = []i_tst {
	{ "0", 0, 0, true },
	{ "1233", 1233, 0, true },
	{ "-5", -5, 0, true },
	{ "0x666", 0, 0x666, true },
	{ "077", 0, 077, true },
	{ "-abc", 0, 0, false },
}

func TestInt(t *testing.T) {
	fmt.Println("Test int parsers")

	for i, t := range(i_tests) {
		var iv int64
		var uv uint64
		var ok bool
		var err error
		var l int

		fmt.Printf("TEST [%d] ", i)
		if t.sresult == 0 {
			l, err = Parse(&uv, []byte(t.input), nil)
			ok = uv == t.uresult
		} else {
			l, err = Parse(&iv, []byte(t.input), nil)
			ok = iv == t.sresult
		}

		if err != nil {
			if t.ok {
				fmt.Printf("ERROR: `%s` %s\n", t.input, err.Error())
			} else {
				fmt.Printf("OK (%d): %s\n", l, err.Error())
			}
		} else {
			if !t.ok {
				fmt.Printf("ERROR (%d): parsed %s\n", l, t.input)
			} else {
				if ok {
					fmt.Printf("OK (%d): %s\n", l, t.input)
				} else {
					fmt.Printf("ERROR (%d): %s != %d or %d\n", l, t.input, uv, iv)
				}
			}
		}
	}
}

func TestBool(t *testing.T) {
	var b bool
	_, err := Parse(&b, []byte("false"), nil)
	if err != nil {
		fmt.Printf("ERROR: %v\n", err)
	} else {
		fmt.Printf("OK: parsed\n")
	}

	_, err = Parse(&b, []byte("true"), nil)
	if err != nil {
		fmt.Printf("ERROR: %v\n", err)
	} else {
		fmt.Printf("OK: parsed\n")
	}

	_, err = Parse(&b, []byte("YES"), nil)
	if err != nil {
		fmt.Printf("OK: %v\n", err)
	} else {
		fmt.Printf("ERROR: parsed invalid boolean\n")
	}

}

type f_tst struct {
	input string
	result float64
	ok bool
}

var f_tests []f_tst = []f_tst {
	{ "0.1", 0.1, true },
	{ "-0.1", -0.1, true },
	{ "0.1e2", 0.1e2, true },
	{ "0.1e-4", 0.1e-4, true },
	{ "-.1", -.1, true },
	{ "100", 100.0, true },
	{ "-100e-2", -100e-2, true },
	{ ".", 0, false },
}

func TestFloat(t *testing.T) {
	fmt.Println("Test float parsers")

	for i, t := range(f_tests) {
		var f float64

		fmt.Printf("TEST [%d] ", i)
		l, err := Parse(&f, []byte(t.input), nil)
		if err != nil {
			if t.ok {
				fmt.Printf("ERROR: `%s` %s\n", t.input, err.Error())
			} else {
				fmt.Printf("OK (%d): %s\n", l, err.Error())
			}
		} else {
			if !t.ok {
				fmt.Printf("ERROR (%d): parsed %s == %f\n", l, t.input, f)
			} else {
				fmt.Printf("OK (%d): %s == %f\n", l, t.input, f)
			}
		}
	}
}


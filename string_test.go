package parse

import (
	"testing"
	"fmt"
)

type tst struct {
	input, result string
	ok bool
}

var tests []tst = []tst {
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

	for i, t := range(tests) {
		var s string

		fmt.Printf("TEST [%d] ", i)
		l, err := Parse(&s, []byte(t.input))
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


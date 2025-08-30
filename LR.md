The idea of LR parsing described in [1]

My implementation is simple.

1. Left Recursion in Parsing Expression Grammars (http://arxiv.org/pdf/1207.0443.pdf)
	S´ergio Medeiros
	Department of Computer Science – UFS – Aracaju – Brazil
	Fabio Mascarenhas
	Department of Computer Science – UFRJ – Rio de Janeiro – Brazil
	Roberto Ierusalimschy
	Department of Computer Science – PUC-Rio – Rio de Janeiro – Brazil

[OVERVIEW]
Let us take this simple grammar as example:
	X <- E
	E <- X '-' N / N
	N <- [0-9]

This grammar couldn't be parsed using standard PEG parsers because of left recursion. But...

I'll try to parse string "1-2-3" with this grammar. At first imaging this parse process:
	X(0) {
		E(0) {
			X(0) { // recursion detected
				E(0) {
					X(0) {
						E(0) {
							X(0) {
								return fail
							}
							N(0) {
								return 1
							}
							return 1
						}
						return 1
					}
					'-'(1) {
						return 2
					}
					N(2) {
						return 3
					}
					return 3
				}
				return 3
			}
			'-'(3) {
				return 4
			}
			N(4) {
				return 5
			}
			return 5
		}
		return 5
	}

Looks good but how could we determine that we are in recursion process and how could we stop one? This is not as
hard as looks at first.

1. Recursion detection.

Recursion could be simply detected if we are using memoization. Let's declare hash table L (historically it is named L)
with key (RULE, LOCATION). Before we started to parse rule r we will add value (true, ...) to memoization table and
after parsing we will delete it or put (false, ...) into. In second variant we will have packrat table for our parser
but large memory footprint.

So when before call X(location) we will search (X, location) in memoization table. If it was found we will assume that
rule is recursive.

2. Stop the recursion

Now we must know when to make recursive call and when to stop. Let's parse string '1-2' with previous grammar.
	X(0) {
		E(0) {
			X(0) { // recursion detected
				E(0) {
					X(0) { // At this point we must stop the process!
						E(0) {
							X(0) {
								return fail
							}
							N(0) {
								return 1
							}
							return 1
						}
						return 1
					}
					'-'(1) {
						return 2
					}
					N(2) {
						return 3
					}
					return 3
				}
				return 3
			}
			'-'(3) {
				return fail
			}
			N(0) {
				return 1
			}
		}
		return 1
	}

So parser will parse only first '1' but not '-'. So if we are growing recursion level we will see shorter results or
errors. It gives us the way to determine when last recursion level is reached. But we must not call all the levels of
recursion in the same time. Let our functions take second argument: recursion level. Look into the process:
	X(0, 0) {
		return fail
	}

	X(0, 1) {
		E(0, 0) {
			X(0, 0) { // This X is exactly the same as in previous backtrace
				return fail
			}
			N(0, 0) {
				return 1
			}
		}
		return 1
	}

	X(0, 2) {
		E(0, 0) {
			X(0, 1) { // This X is exactly the same as in previous backtrace
				E(0, 0) { // Here is the problem, but I'll ignore it until the next section
					X(0, 0) {
						return fail
					}
					N(0, 0) {
						return 1
					}
					return 1
				}
				return 1
			}
			'-'(1, 0) {
				return 2
			}
			N(2, 0) {
				return 3
			}
			return 3
		}
		return 3
	}
	...

As you can see here we can simply take results from previous step and not call all the stack again. So we can simply loop
over the recursion level and return previous value in recursive calls.

But here is another problem. When we will call E(0, 0) second time it will be detected as recursive. So what will going on?
The parser will try to call E recursive with recursion level set. And it will try to call X(0, 0) again and it will find that
X is also recursive and et.cetera. But if we are using memoization of the previous result there must not be recursive calls
in the same recursion path.

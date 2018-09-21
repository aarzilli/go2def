package testfixture1

import (
	"github.com/aarzilli/go2def/internal/testfixture2"
)

func callable(x int) int {
	return x+3
}

func caller(x int, y int) {
	var b testfixture2.Bstruct
	callable(/*a*/callable/*b*/(y+1) + /*c*/callable2/*d*/(x-y) + /*e*/testfixture2.Callable3/*f*/(10))
	println(/*g*/b/*h*/.Xmember)
	/*i*/
}

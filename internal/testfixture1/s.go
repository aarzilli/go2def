package testfixture1

import (
	"strconv"
)

func example() {
	a := &Astruct{}
	b := &Astruct{}
	
	/*a*/a.Method2/*b*/(/*e*/b/*f*/)
	
	/*c*/strconv.Atoi/*d*/("10")
	
	/*i*/a._/*j*/
}

type Astruct struct {
	Xmember int
	Ymember int
}

func (a *Astruct) Method1(x int) int {
	return x+/*g*/a.Xmember/*h*/
}

func (a *Astruct) Method2(b *Astruct) int {
	return a.Ymember + b.Ymember
}

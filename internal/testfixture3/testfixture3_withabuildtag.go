// +build buildtag

package testfixture3

func samefilefn3() {
}

func main4() {
	/*a*/samefilefn3/*b*/()
	/*c*/otherfilefn3/*d*/()
}

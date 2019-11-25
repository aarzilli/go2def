package testfixture3

func samefilefn() {
}

func main() {
	/*a*/samefilefn/*b*/ ()
	/*c*/otherfilefn/*d*/ ()
}

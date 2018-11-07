package testfixture1

import "testing"

func somefn() {
}

func TestSomething(t *testing.T) {
	/*a*/t.Fatalf/*b*/("blah")
	/*c*/somefn/*d*/()
}

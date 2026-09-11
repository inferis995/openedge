package edgesync

import "testing"

func TestEmptyArrayLiteral(t *testing.T) {
	v, err := pqInt64Array(nil).Value()
	if err != nil || v != "{}" {
		t.Fatalf("empty array = %v, %v; an empty list must still delete everything", v, err)
	}
	v, _ = pqInt64Array([]int64{3, 1, 2}).Value()
	if v != "{3,1,2}" {
		t.Fatalf("array = %v", v)
	}
}

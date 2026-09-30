package handlers

import (
	"reflect"
	"testing"
)

func TestNumbers(t *testing.T) {
	got := numbers("Revenue 4,500,000 in 2026-08, up 12.50% in Q3; see run 7.")
	want := []string{"4500000", "2026", "8", "12.5", "7"}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("numbers = %v, want %v", got, want)
	}
}

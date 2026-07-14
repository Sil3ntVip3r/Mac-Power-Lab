package main

import "testing"

func TestPositiveInt(t *testing.T) {
	if v, err := parsePositiveInt("12"); err != nil || v != 12 {
		t.Fatal(v, err)
	}
	if _, err := parsePositiveInt("0"); err == nil {
		t.Fatal("expected validation error")
	}
}

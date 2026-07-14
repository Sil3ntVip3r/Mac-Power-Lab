package plist

import "testing"

func TestParseXML(t *testing.T) {
	input := []byte(`<?xml version="1.0"?><plist version="1.0"><dict><key>a</key><integer>3</integer><key>b</key><array><string>x</string><true/></array></dict></plist>`)
	value, err := Parse(input)
	if err != nil {
		t.Fatal(err)
	}
	m := value.(map[string]any)
	if m["a"].(int64) != 3 {
		t.Fatalf("a=%v", m["a"])
	}
	if len(m["b"].([]any)) != 2 {
		t.Fatal("array not parsed")
	}
}

func TestParseNUL(t *testing.T) {
	one := []byte(`<?xml version="1.0"?><plist version="1.0"><string>one</string></plist>`)
	two := []byte(`<?xml version="1.0"?><plist version="1.0"><string>two</string></plist>`)
	data := append(append(append([]byte{}, one...), 0), two...)
	values, err := ParseNUL(data)
	if err != nil {
		t.Fatal(err)
	}
	if len(values) != 2 || values[0] != "one" || values[1] != "two" {
		t.Fatalf("values=%v", values)
	}
}

func TestParseRejectsDuplicateDictionaryKey(t *testing.T) {
	_, err := Parse([]byte(`<plist><dict><key>a</key><integer>1</integer><key>a</key><integer>2</integer></dict></plist>`))
	if err == nil {
		t.Fatal("expected duplicate-key error")
	}
}

func TestParseRejectsMultipleRootValues(t *testing.T) {
	_, err := Parse([]byte(`<plist><string>one</string><string>two</string></plist>`))
	if err == nil {
		t.Fatal("expected multiple-root error")
	}
}

func TestParseRejectsExcessiveNesting(t *testing.T) {
	input := `<plist>`
	for i := 0; i < maxNestingDepth+1; i++ {
		input += `<array>`
	}
	input += `<string>x</string>`
	for i := 0; i < maxNestingDepth+1; i++ {
		input += `</array>`
	}
	input += `</plist>`
	if _, err := Parse([]byte(input)); err == nil {
		t.Fatal("expected depth-limit error")
	}
}

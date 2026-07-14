// Package plist implements a dependency-free, bounded parser for XML property lists.
package plist

import (
	"bytes"
	"encoding/base64"
	"encoding/xml"
	"errors"
	"fmt"
	"io"
	"strconv"
	"strings"
	"time"
)

const (
	maxNestingDepth = 128
	maxValueNodes   = 1_000_000
)

type parser struct {
	decoder *xml.Decoder
	nodes   int
}

// Parse decodes exactly one XML plist into Go maps, slices, and primitive
// values. Nesting and node limits protect callers that parse externally supplied
// or malformed diagnostic files.
func Parse(data []byte) (any, error) {
	data = bytes.TrimSpace(data)
	if len(data) == 0 {
		return nil, errors.New("empty plist")
	}
	p := &parser{decoder: xml.NewDecoder(bytes.NewReader(data))}
	for {
		token, err := p.decoder.Token()
		if err != nil {
			return nil, fmt.Errorf("parse plist: %w", err)
		}
		start, ok := token.(xml.StartElement)
		if !ok || start.Name.Local != "plist" {
			continue
		}
		value, err := p.parseRoot()
		if err != nil {
			return nil, fmt.Errorf("parse plist: %w", err)
		}
		// Reject a second top-level XML element or non-whitespace character data.
		for {
			token, trailingErr := p.decoder.Token()
			if errors.Is(trailingErr, io.EOF) {
				return value, nil
			}
			if trailingErr != nil {
				return nil, fmt.Errorf("parse plist trailing data: %w", trailingErr)
			}
			switch item := token.(type) {
			case xml.StartElement:
				return nil, fmt.Errorf("unexpected trailing element %q", item.Name.Local)
			case xml.CharData:
				if len(bytes.TrimSpace(item)) > 0 {
					return nil, errors.New("unexpected trailing character data")
				}
			}
		}
	}
}

func (p *parser) parseRoot() (any, error) {
	var value any
	seen := false
	for {
		token, err := p.decoder.Token()
		if err != nil {
			return nil, err
		}
		switch item := token.(type) {
		case xml.StartElement:
			if seen {
				return nil, fmt.Errorf("plist has multiple root values; second is %q", item.Name.Local)
			}
			value, err = p.parseValue(item, 1)
			if err != nil {
				return nil, err
			}
			seen = true
		case xml.CharData:
			if len(bytes.TrimSpace(item)) > 0 {
				return nil, errors.New("unexpected character data inside plist")
			}
		case xml.EndElement:
			if item.Name.Local == "plist" {
				if !seen {
					return nil, errors.New("plist has no value")
				}
				return value, nil
			}
		}
	}
}

func (p *parser) enter(depth int) error {
	if depth > maxNestingDepth {
		return fmt.Errorf("plist nesting exceeds %d", maxNestingDepth)
	}
	p.nodes++
	if p.nodes > maxValueNodes {
		return fmt.Errorf("plist value count exceeds %d", maxValueNodes)
	}
	return nil
}

func (p *parser) parseValue(start xml.StartElement, depth int) (any, error) {
	if err := p.enter(depth); err != nil {
		return nil, err
	}
	switch start.Name.Local {
	case "dict":
		out := make(map[string]any)
		var key string
		for {
			token, err := p.decoder.Token()
			if err != nil {
				return nil, err
			}
			switch item := token.(type) {
			case xml.StartElement:
				if item.Name.Local == "key" {
					if key != "" {
						return nil, fmt.Errorf("dict key %q has no value", key)
					}
					var decoded string
					if err := p.decoder.DecodeElement(&decoded, &item); err != nil {
						return nil, err
					}
					key = decoded
					if _, exists := out[key]; exists {
						return nil, fmt.Errorf("duplicate dict key %q", key)
					}
					continue
				}
				if key == "" {
					return nil, fmt.Errorf("dict value %s has no key", item.Name.Local)
				}
				decoded, err := p.parseValue(item, depth+1)
				if err != nil {
					return nil, err
				}
				out[key] = decoded
				key = ""
			case xml.CharData:
				if len(bytes.TrimSpace(item)) > 0 {
					return nil, errors.New("unexpected character data inside dict")
				}
			case xml.EndElement:
				if item.Name.Local == "dict" {
					if key != "" {
						return nil, fmt.Errorf("dict key %q has no value", key)
					}
					return out, nil
				}
			}
		}
	case "array":
		var out []any
		for {
			token, err := p.decoder.Token()
			if err != nil {
				return nil, err
			}
			switch item := token.(type) {
			case xml.StartElement:
				decoded, err := p.parseValue(item, depth+1)
				if err != nil {
					return nil, err
				}
				out = append(out, decoded)
			case xml.CharData:
				if len(bytes.TrimSpace(item)) > 0 {
					return nil, errors.New("unexpected character data inside array")
				}
			case xml.EndElement:
				if item.Name.Local == "array" {
					return out, nil
				}
			}
		}
	case "string", "key":
		var value string
		if err := p.decoder.DecodeElement(&value, &start); err != nil {
			return nil, err
		}
		return value, nil
	case "integer":
		var value string
		if err := p.decoder.DecodeElement(&value, &start); err != nil {
			return nil, err
		}
		integer, err := strconv.ParseInt(strings.TrimSpace(value), 0, 64)
		if err != nil {
			return nil, fmt.Errorf("integer %q: %w", value, err)
		}
		return integer, nil
	case "real":
		var value string
		if err := p.decoder.DecodeElement(&value, &start); err != nil {
			return nil, err
		}
		real, err := strconv.ParseFloat(strings.TrimSpace(value), 64)
		if err != nil {
			return nil, fmt.Errorf("real %q: %w", value, err)
		}
		return real, nil
	case "true":
		if err := p.decoder.Skip(); err != nil {
			return nil, err
		}
		return true, nil
	case "false":
		if err := p.decoder.Skip(); err != nil {
			return nil, err
		}
		return false, nil
	case "date":
		var value string
		if err := p.decoder.DecodeElement(&value, &start); err != nil {
			return nil, err
		}
		parsed, err := time.Parse(time.RFC3339, strings.TrimSpace(value))
		if err != nil {
			return value, nil
		}
		return parsed, nil
	case "data":
		var value string
		if err := p.decoder.DecodeElement(&value, &start); err != nil {
			return nil, err
		}
		decoded, err := base64.StdEncoding.DecodeString(strings.Join(strings.Fields(value), ""))
		if err != nil {
			return []byte(value), nil
		}
		return decoded, nil
	default:
		var value string
		if err := p.decoder.DecodeElement(&value, &start); err != nil && !errors.Is(err, io.EOF) {
			return nil, err
		}
		return value, nil
	}
}

// SplitNUL separates the NUL-delimited plist stream emitted by powermetrics.
func SplitNUL(data []byte) [][]byte {
	raw := bytes.Split(data, []byte{0})
	out := make([][]byte, 0, len(raw))
	for _, part := range raw {
		part = bytes.TrimSpace(part)
		if len(part) > 0 {
			out = append(out, part)
		}
	}
	return out
}

// ParseNUL decodes every plist in a NUL-delimited stream.
func ParseNUL(data []byte) ([]any, error) {
	parts := SplitNUL(data)
	out := make([]any, 0, len(parts))
	for index, part := range parts {
		value, err := Parse(part)
		if err != nil {
			return nil, fmt.Errorf("sample %d: %w", index, err)
		}
		out = append(out, value)
	}
	return out, nil
}

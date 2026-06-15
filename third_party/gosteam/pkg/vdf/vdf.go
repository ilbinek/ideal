// Package vdf parses Valve's binary KeyValues format, which is how Steam's PICS
// product-info responses encode app metadata (the "appinfo" tree containing the
// "depots" section, including "workshopdepot"). Only the subset needed to read
// that metadata is implemented.
package vdf

import (
	"encoding/binary"
	"fmt"
	"io"
	"strconv"
	"strings"
)

// Binary KeyValues type tags.
const (
	typeNone       = 0  // nested object
	typeString     = 1  // null-terminated UTF-8
	typeInt32      = 2  // int32 LE
	typeFloat32    = 3  // float32 LE
	typePointer    = 4  // int32 LE
	typeWideString = 5  // unsupported
	typeColor      = 6  // int32 LE
	typeUInt64     = 7  // uint64 LE
	typeEnd        = 8  // end of object
	typeInt64      = 10 // int64 LE
	typeEnd2       = 11 // alternate end marker
)

// ParseBinary decodes a binary KeyValues blob into a nested map. Values are
// stored as string, uint32, or uint64; nested objects as map[string]any.
func ParseBinary(b []byte) (map[string]any, error) {
	r := &reader{b: b}
	root := map[string]any{}
	if err := r.readObject(root); err != nil {
		return nil, err
	}
	return root, nil
}

type reader struct {
	b   []byte
	pos int
}

func (r *reader) byte() (byte, error) {
	if r.pos >= len(r.b) {
		return 0, io.EOF
	}
	c := r.b[r.pos]
	r.pos++
	return c, nil
}

func (r *reader) cstring() (string, error) {
	start := r.pos
	for r.pos < len(r.b) && r.b[r.pos] != 0 {
		r.pos++
	}
	if r.pos >= len(r.b) {
		return "", io.ErrUnexpectedEOF
	}
	s := string(r.b[start:r.pos])
	r.pos++ // skip null
	return s, nil
}

func (r *reader) take(n int) ([]byte, error) {
	if r.pos+n > len(r.b) {
		return nil, io.ErrUnexpectedEOF
	}
	v := r.b[r.pos : r.pos+n]
	r.pos += n
	return v, nil
}

func (r *reader) readObject(into map[string]any) error {
	for {
		t, err := r.byte()
		if err == io.EOF {
			return nil // top-level objects may end at EOF rather than an end marker
		}
		if err != nil {
			return err
		}
		if t == typeEnd || t == typeEnd2 {
			return nil
		}

		name, err := r.cstring()
		if err != nil {
			return err
		}

		switch t {
		case typeNone:
			child := map[string]any{}
			if err := r.readObject(child); err != nil {
				return err
			}
			into[name] = child
		case typeString:
			s, err := r.cstring()
			if err != nil {
				return err
			}
			into[name] = s
		case typeInt32, typePointer, typeColor, typeFloat32:
			v, err := r.take(4)
			if err != nil {
				return err
			}
			into[name] = binary.LittleEndian.Uint32(v)
		case typeUInt64, typeInt64:
			v, err := r.take(8)
			if err != nil {
				return err
			}
			into[name] = binary.LittleEndian.Uint64(v)
		case typeWideString:
			return fmt.Errorf("vdf: widestring type not supported")
		default:
			return fmt.Errorf("vdf: unknown type tag %d at offset %d", t, r.pos-1)
		}
	}
}

// ParseText decodes a text KeyValues blob (the format Steam uses for app
// product-info buffers) into a nested map. Scalar values are stored as strings
// and nested objects as map[string]any. Conditional tokens like [$WIN64] are
// ignored, as are // line comments.
func ParseText(b []byte) (map[string]any, error) {
	t := &textReader{b: b}
	return t.readObject()
}

type textReader struct {
	b   []byte
	pos int
}

func (t *textReader) readObject() (map[string]any, error) {
	obj := map[string]any{}
	for {
		key, ok, err := t.next()
		if err != nil {
			return nil, err
		}
		if !ok || key == "}" { // EOF at top level, or end of this object
			return obj, nil
		}
		if key == "{" {
			return nil, fmt.Errorf("vdf: unexpected '{' where a key was expected")
		}

		val, ok, err := t.next()
		if err != nil {
			return nil, err
		}
		if !ok {
			return nil, io.ErrUnexpectedEOF
		}
		if val == "{" {
			child, err := t.readObject()
			if err != nil {
				return nil, err
			}
			obj[key] = child
		} else {
			obj[key] = val
		}
	}
}

// next returns the next token: a quoted/unquoted string, "{", or "}". Whitespace,
// // comments, and [conditional] tokens are skipped. ok is false at EOF.
func (t *textReader) next() (string, bool, error) {
	for {
		for t.pos < len(t.b) && isSpace(t.b[t.pos]) {
			t.pos++
		}
		if t.pos >= len(t.b) {
			return "", false, nil
		}
		c := t.b[t.pos]

		switch {
		case c == '/' && t.pos+1 < len(t.b) && t.b[t.pos+1] == '/':
			for t.pos < len(t.b) && t.b[t.pos] != '\n' {
				t.pos++
			}
			continue
		case c == '{' || c == '}':
			t.pos++
			return string(c), true, nil
		case c == '[': // conditional, e.g. [$WIN64]; skip to ']'
			for t.pos < len(t.b) && t.b[t.pos] != ']' {
				t.pos++
			}
			if t.pos < len(t.b) {
				t.pos++
			}
			continue
		case c == '"':
			return t.quoted()
		default:
			start := t.pos
			for t.pos < len(t.b) {
				ch := t.b[t.pos]
				if isSpace(ch) || ch == '{' || ch == '}' || ch == '"' {
					break
				}
				t.pos++
			}
			return string(t.b[start:t.pos]), true, nil
		}
	}
}

func (t *textReader) quoted() (string, bool, error) {
	t.pos++ // opening quote
	var sb []byte
	for t.pos < len(t.b) {
		ch := t.b[t.pos]
		t.pos++
		if ch == '\\' && t.pos < len(t.b) {
			e := t.b[t.pos]
			t.pos++
			switch e {
			case 'n':
				sb = append(sb, '\n')
			case 't':
				sb = append(sb, '\t')
			default:
				sb = append(sb, e) // covers \" and \\
			}
			continue
		}
		if ch == '"' {
			return string(sb), true, nil
		}
		sb = append(sb, ch)
	}
	return "", false, io.ErrUnexpectedEOF
}

func isSpace(c byte) bool {
	return c == ' ' || c == '\t' || c == '\r' || c == '\n'
}

// FindUint32 recursively searches tree for the first value under key (case
// insensitive) that can be interpreted as a uint32.
func FindUint32(tree map[string]any, key string) (uint32, bool) {
	for k, v := range tree {
		if strings.EqualFold(k, key) {
			if u, ok := toUint32(v); ok {
				return u, true
			}
		}
		if child, ok := v.(map[string]any); ok {
			if u, ok := FindUint32(child, key); ok {
				return u, true
			}
		}
	}
	return 0, false
}

func toUint32(v any) (uint32, bool) {
	switch t := v.(type) {
	case uint32:
		return t, true
	case uint64:
		return uint32(t), true
	case string:
		n, err := strconv.ParseUint(strings.TrimSpace(t), 10, 64)
		if err != nil {
			return 0, false
		}
		return uint32(n), true
	default:
		return 0, false
	}
}

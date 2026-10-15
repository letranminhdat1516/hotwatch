package apps

import (
	"bytes"
	"context"
	"encoding/xml"
	"errors"
	"fmt"
	"io"

	"hotwatch/mac/exec"
)

// parsePlist returns the top-level string values of an XML property list
// (nested dictionaries and arrays are skipped). It never runs anything: a
// binary plist is converted by the caller first.
func parsePlist(src []byte) (map[string]string, error) {
	dec := xml.NewDecoder(bytes.NewReader(src))
	dec.Strict = false
	out := map[string]string{}
	depth := 0 // dict nesting below the top-level dict
	key := ""
	wantValue := false
	for {
		tok, err := dec.Token()
		if err == io.EOF {
			break
		}
		if err != nil {
			return nil, fmt.Errorf("plist: %w", err)
		}
		switch t := tok.(type) {
		case xml.StartElement:
			switch t.Name.Local {
			case "dict", "array":
				depth++
				wantValue = false
			case "key":
				if depth == 1 {
					var k string
					if err := dec.DecodeElement(&k, &t); err != nil {
						return nil, fmt.Errorf("plist: %w", err)
					}
					key, wantValue = k, true
				}
			case "string", "integer", "real", "date":
				if depth == 1 && wantValue {
					var v string
					if err := dec.DecodeElement(&v, &t); err != nil {
						return nil, fmt.Errorf("plist: %w", err)
					}
					out[key] = v
				}
				wantValue = false
			case "true", "false":
				if depth == 1 && wantValue {
					out[key] = t.Name.Local
				}
				wantValue = false
			}
		case xml.EndElement:
			if t.Name.Local == "dict" || t.Name.Local == "array" {
				depth--
			}
		}
	}
	return out, nil
}

var errBinaryPlist = errors.New("plist: binary")

// readPlist parses an Info.plist, converting a binary one through plutil
// (argv, bounded by run's timeout).
func readPlist(ctx context.Context, run exec.Runner, path string, src []byte) (map[string]string, error) {
	if bytes.HasPrefix(src, []byte("bplist")) {
		if run == nil {
			return nil, errBinaryPlist
		}
		out, err := run(ctx, "plutil", "-convert", "xml1", "-o", "-", path)
		if err != nil {
			return nil, err
		}
		src = out
	}
	return parsePlist(src)
}

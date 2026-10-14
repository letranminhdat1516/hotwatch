//go:build darwin

package darwin

import (
	"bytes"
	"context"
	"encoding/xml"
	"errors"
	"io"
	"os/exec"
	"sort"
	"strconv"
	"time"
)

// APFS volumes live in a synthesized container disk (disk3) that has no I/O
// counters; the traffic is counted on the physical store (disk0). All volumes
// of one container therefore show the same rate, the whole store's.

// runDiskutilAPFS returns `diskutil apfs list -plist` (argv, no shell, 3 s).
func runDiskutilAPFS(ctx context.Context) ([]byte, error) {
	ctx, cancel := context.WithTimeout(ctx, 3*time.Second)
	defer cancel()
	return exec.CommandContext(ctx, "diskutil", "apfs", "list", "-plist").Output()
}

// parseAPFSList maps each APFS container disk name (disk3) to the whole-disk
// names of its physical stores (disk0), sorted and de-duplicated.
func parseAPFSList(data []byte) (map[string][]string, error) {
	root, err := parsePlist(data)
	if err != nil {
		return nil, err
	}
	top, _ := root.(map[string]any)
	containers, _ := top["Containers"].([]any)
	out := map[string][]string{}
	for _, c := range containers {
		cm, _ := c.(map[string]any)
		ref, _ := cm["ContainerReference"].(string)
		if ref == "" {
			continue
		}
		set := map[string]bool{}
		stores, _ := cm["PhysicalStores"].([]any)
		for _, s := range stores {
			sm, _ := s.(map[string]any)
			id, _ := sm["DeviceIdentifier"].(string)
			if n := bsdName(id); n != "" {
				set[n] = true
			}
		}
		if len(set) == 0 {
			continue
		}
		names := make([]string, 0, len(set))
		for n := range set {
			names = append(names, n)
		}
		sort.Strings(names)
		out[ref] = names
	}
	return out, nil
}

// parsePlist decodes an XML property list into map[string]any, []any, string,
// int64, float64, bool, []byte-free generic values (data/date kept as string).
func parsePlist(data []byte) (any, error) {
	d := xml.NewDecoder(bytes.NewReader(data))
	d.Strict = false
	d.Entity = xml.HTMLEntity
	for {
		t, err := d.Token()
		if err != nil {
			return nil, errors.New("plist: no root value")
		}
		if se, ok := t.(xml.StartElement); ok && se.Name.Local == "plist" {
			break
		}
	}
	return plistValue(d)
}

func plistValue(d *xml.Decoder) (any, error) {
	for {
		t, err := d.Token()
		if err != nil {
			return nil, err
		}
		if se, ok := t.(xml.StartElement); ok {
			return plistStarted(d, se)
		}
	}
}

// plistStarted decodes the value whose start element has just been read.
func plistStarted(d *xml.Decoder, se xml.StartElement) (any, error) {
	switch se.Name.Local {
	case "dict":
		m := map[string]any{}
		var key string
		for {
			t, err := d.Token()
			if err != nil {
				return nil, err
			}
			switch e := t.(type) {
			case xml.EndElement:
				return m, nil
			case xml.StartElement:
				if e.Name.Local == "key" {
					if key, err = plistText(d); err != nil {
						return nil, err
					}
					continue
				}
				v, err := plistStarted(d, e)
				if err != nil {
					return nil, err
				}
				m[key] = v
			}
		}
	case "array":
		arr := []any{}
		for {
			t, err := d.Token()
			if err != nil {
				return nil, err
			}
			switch e := t.(type) {
			case xml.EndElement:
				return arr, nil
			case xml.StartElement:
				v, err := plistStarted(d, e)
				if err != nil {
					return nil, err
				}
				arr = append(arr, v)
			}
		}
	case "true":
		return true, d.Skip()
	case "false":
		return false, d.Skip()
	}
	txt, err := plistText(d)
	if err != nil {
		return nil, err
	}
	switch se.Name.Local {
	case "integer":
		return strconv.ParseInt(txt, 10, 64)
	case "real":
		return strconv.ParseFloat(txt, 64)
	}
	return txt, nil // string, data, date
}

// plistText reads character data up to the matching end element.
func plistText(d *xml.Decoder) (string, error) {
	var b bytes.Buffer
	for {
		t, err := d.Token()
		if err != nil {
			if errors.Is(err, io.EOF) {
				err = io.ErrUnexpectedEOF
			}
			return "", err
		}
		switch e := t.(type) {
		case xml.CharData:
			b.Write(e)
		case xml.EndElement:
			return b.String(), nil
		}
	}
}

package linux

import (
	"bufio"
	"os"
	"strconv"
	"strings"

	"hotwatch/model"
)

// readListeners maps socket inode to the listening port it represents:
// TCP rows in state LISTEN (0A) and UDP rows in state 07. Missing files
// (for example no IPv6) are skipped.
func readListeners(procRoot string) map[uint64][]model.Port {
	out := map[uint64][]model.Port{}
	for _, t := range []struct{ file, proto, state string }{
		{"net/tcp", "tcp", "0A"}, {"net/tcp6", "tcp", "0A"},
		{"net/udp", "udp", "07"}, {"net/udp6", "udp", "07"},
	} {
		readSockTable(procRoot+"/"+t.file, t.proto, t.state, out)
	}
	return out
}

func readSockTable(path, proto, state string, out map[uint64][]model.Port) {
	f, err := os.Open(path)
	if err != nil {
		return
	}
	defer f.Close()
	sc := bufio.NewScanner(f)
	sc.Scan() // header
	for sc.Scan() {
		fields := strings.Fields(sc.Text())
		if len(fields) < 10 || fields[3] != state {
			continue
		}
		_, hexPort, ok := cutLast(fields[1], ':')
		if !ok {
			continue
		}
		port, err1 := strconv.ParseUint(hexPort, 16, 16)
		inode, err2 := strconv.ParseUint(fields[9], 10, 64)
		if err1 != nil || err2 != nil || inode == 0 {
			continue
		}
		out[inode] = append(out[inode], model.Port{Number: uint16(port), Proto: proto})
	}
}

func cutLast(s string, sep byte) (before, after string, ok bool) {
	i := strings.LastIndexByte(s, sep)
	if i < 0 {
		return s, "", false
	}
	return s[:i], s[i+1:], true
}

// socketInode parses a readlink result of the form "socket:[12345]".
func socketInode(link string) (uint64, bool) {
	const pre = "socket:["
	if !strings.HasPrefix(link, pre) || !strings.HasSuffix(link, "]") {
		return 0, false
	}
	n, err := strconv.ParseUint(link[len(pre):len(link)-1], 10, 64)
	return n, err == nil
}

// processPorts returns the listening ports owned by the socket fds of dir
// (a /proc/<pid>/fd directory), deduplicated and sorted. An unreadable
// directory yields nil.
func processPorts(fdDir string, listeners map[uint64][]model.Port) []model.Port {
	if len(listeners) == 0 {
		return nil
	}
	d, err := os.Open(fdDir)
	if err != nil {
		return nil
	}
	names, _ := d.Readdirnames(-1)
	d.Close()
	var ports []model.Port
	for _, n := range names {
		link, err := os.Readlink(fdDir + "/" + n)
		if err != nil {
			continue
		}
		inode, ok := socketInode(link)
		if !ok {
			continue
		}
		for _, p := range listeners[inode] {
			dup := false
			for _, q := range ports {
				if q == p {
					dup = true
					break
				}
			}
			if !dup {
				ports = append(ports, p)
			}
		}
	}
	sortPorts(ports)
	return ports
}

func sortPorts(p []model.Port) {
	for i := 1; i < len(p); i++ {
		for j := i; j > 0 && portLess(p[j], p[j-1]); j-- {
			p[j], p[j-1] = p[j-1], p[j]
		}
	}
}

func portLess(a, b model.Port) bool {
	if a.Number != b.Number {
		return a.Number < b.Number
	}
	return a.Proto < b.Proto
}

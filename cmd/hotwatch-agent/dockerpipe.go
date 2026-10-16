package main

import (
	"strings"
)

// dockerServers are the programs that serve Docker's named pipe: Docker
// Engine's daemon and Docker Desktop's backend and proxy.
var dockerServers = map[string]bool{"dockerd.exe": true, "com.docker.backend.exe": true, "com.docker.proxy.exe": true}

// dockerServerImage reports whether exe is one of Docker's pipe servers
// installed under programFiles\Docker, a folder only administrators can
// write. A path with "." or ".." parts is refused rather than resolved.
func dockerServerImage(exe, programFiles string) bool {
	root := strings.ToLower(strings.TrimRight(programFiles, `\`)) + `\docker\`
	p := strings.ToLower(exe)
	rest, ok := strings.CutPrefix(p, root)
	if !ok || programFiles == "" {
		return false
	}
	parts := strings.Split(rest, `\`)
	for _, part := range parts {
		if part == "" || part == "." || part == ".." {
			return false
		}
	}
	return dockerServers[parts[len(parts)-1]]
}

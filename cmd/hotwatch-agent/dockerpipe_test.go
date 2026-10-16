package main

import "testing"

func TestDockerServerImage(t *testing.T) {
	pf := `C:\Program Files`
	for exe, want := range map[string]bool{
		`C:\Program Files\Docker\Docker\resources\com.docker.backend.exe`: true,
		`C:\Program Files\Docker\Docker\resources\com.docker.proxy.exe`:   true,
		`C:\PROGRAM FILES\DOCKER\dockerd.exe`:                             true,
		`C:\Program Files\nodejs\node.exe`:                                false, // an interpreter can serve any pipe
		`C:\Program Files\Docker\Docker\resources\node.exe`:               false,
		`C:\Program Files\Dockerish\dockerd.exe`:                          false,
		`C:\Program Files (x86)\Docker\dockerd.exe`:                       false,
		`C:\Users\me\dockerd.exe`:                                         false,
		`C:\Program Files\Docker\..\Evil\dockerd.exe`:                     false,
	} {
		if got := dockerServerImage(exe, pf); got != want {
			t.Errorf("%s: %v, want %v", exe, got, want)
		}
	}
}

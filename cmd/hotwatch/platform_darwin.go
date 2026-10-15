package main

import (
	"hotwatch/source"
	"hotwatch/source/local"
	"hotwatch/store"
)

func newLocalSource() source.Source { return local.New() }

func newSecrets() (store.Secrets, error) { return store.NewKeychain("Hotwatch") }

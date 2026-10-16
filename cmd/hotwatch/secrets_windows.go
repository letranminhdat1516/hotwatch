package main

import "hotwatch/store"

// newSecrets keeps host tokens in the user's Windows Credential Manager.
func newSecrets(paths) (store.Secrets, error) { return store.NewCredentialManager("Hotwatch"), nil }

package main

import "testing"

func TestIsDistrustedEmSignRoot(t *testing.T) {
	for _, name := range []string{
		"emSign ECC Root CA - G3",
		"CN=emSign ECC Root CA - G3; OU=emSign PKI",
	} {
		if !isDistrustedEmSignRoot(name) {
			t.Errorf("%q was not blocked", name)
		}
	}
	for _, name := range []string{
		"emSign Root CA - G1",
		"emSign ECC Root CA - C3",
		"Example Root CA",
	} {
		if isDistrustedEmSignRoot(name) {
			t.Errorf("%q was unexpectedly blocked", name)
		}
	}
}

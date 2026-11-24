package link

import "testing"

func TestPublicDownloadAddress(t *testing.T) {
	tests := []struct {
		address string
		allowed bool
	}{
		{"1.1.1.1:443", true},
		{"[2606:4700:4700::1111]:443", true},
		{"127.0.0.1:443", false},
		{"100.64.0.1:443", false},
		{"192.168.0.1:443", false},
		{"[::ffff:10.0.0.1]:443", false},
		{"[fe80::1]:443", false},
		{"[::1]:443", false},
	}
	for _, test := range tests {
		if allowed := checkPublicDownloadAddress("tcp", test.address, nil) == nil; allowed != test.allowed {
			t.Errorf("address %s allowed = %v, want %v", test.address, allowed, test.allowed)
		}
	}
}

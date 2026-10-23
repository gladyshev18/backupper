//go:build !windows

package desktop

// The panel targets Windows; elsewhere the file is stored as is.
func protect(plain []byte) ([]byte, error) { return plain, nil }
func unprotect(enc []byte) ([]byte, error) { return enc, nil }

//go:build !unix

package sinkpush

// The non-Unix implementation treats every regular file as executable and
// lets exec.Command surface platform-specific failures later.
var testOtherOnlyExecutable = true

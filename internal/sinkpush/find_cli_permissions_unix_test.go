//go:build unix

package sinkpush

import "os"

var testOtherOnlyExecutable = os.Geteuid() == 0

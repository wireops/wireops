package logger

import (
	"fmt"
	"log"
	"strings"
)

// SafeLogf sanitizes the formatted string to prevent log forging vulnerabilities.
func SafeLogf(format string, v ...interface{}) {
	msg := fmt.Sprintf(format, v...)
	msg = strings.ReplaceAll(msg, "\n", " ")
	msg = strings.ReplaceAll(msg, "\r", " ")
	log.Print(msg)
}

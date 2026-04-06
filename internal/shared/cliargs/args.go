package cliargs

import (
	"fmt"
	"strings"
)

func RejectSingleDashLongFlags(args []string) error {
	for _, arg := range args {
		if arg == "--" {
			return nil
		}
		if strings.HasPrefix(arg, "--") || !strings.HasPrefix(arg, "-") || arg == "-" {
			continue
		}
		if len(strings.TrimPrefix(arg, "-")) == 1 {
			continue
		}
		return fmt.Errorf("use GNU-style long flags with two dashes, for example --%s instead of %s", strings.TrimPrefix(arg, "-"), arg)
	}
	return nil
}

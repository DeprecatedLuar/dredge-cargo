package commands

import "fmt"

func HandleCat(args []string) error {
	if len(args) < 1 {
		return fmt.Errorf("usage: dredge cat <id>")
	}
	return HandleView(args, true)
}

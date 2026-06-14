package cli

import (
	"fmt"

	"github.com/alexander-danilenko/shipnotes/internal/infrastructure/config"
	"github.com/alexander-danilenko/shipnotes/internal/infrastructure/terminal"
)

// printConfigError shows environment-validation problems in a readable list.
func printConfigError(console *terminal.Console, err error) {
	validationErr, ok := config.AsValidationError(err)
	if !ok {
		console.Failure("💥 " + err.Error())

		return
	}

	console.Failure("\n💥 Environment validation failed")
	console.Dim("Missing or invalid environment variables:\n")

	for _, problem := range validationErr.Problems {
		console.Warn(fmt.Sprintf("  • %s: %s", problem.Field, problem.Message))
	}

	console.Dim("\nPlease check your .env file or copy .env.example")
}

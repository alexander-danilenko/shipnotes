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

	console.Failure("\n💥 Configuration validation failed")
	console.Dim("Missing or invalid configuration (set a flag or the environment variable):\n")

	for _, problem := range validationErr.Problems {
		console.Warn(fmt.Sprintf("  • %s: %s", problem.Field, problem.Message))
	}

	console.Dim("\nPass the values as flags (--jira-base-url, --jira-email, --jira-token) " +
		"or set them in your environment or .env file (copy .env.example).")
}

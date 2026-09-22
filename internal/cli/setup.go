package cli

import (
	"context"
	"encoding/json"
	"flag"
	"fmt"
	"io"

	"github.com/JAYY513/Workloom/internal/app"
)

// runSetup implements `workloom setup`: the product onboarding entry. It
// composes init, the starter workflow, and wire, then reports config, MCP,
// prime, blueprint, and doctor. It does not declare a blueprint and does
// not write an MCP client config.
func runSetup(stdout io.Writer, opts options, rest []string) error {
	fs := flag.NewFlagSet("setup", flag.ContinueOnError)
	fs.SetOutput(io.Discard)
	template := fs.String("template", app.DefaultSetupTemplate, "workflow template to install if missing")
	if err := fs.Parse(rest); err != nil || fs.NArg() != 0 {
		return errUsage("`workloom setup` [--template quick-fix]")
	}
	svc, err := appService()
	if err != nil {
		return err
	}
	view, err := svc.Setup(context.Background(), *template)
	if opts.json {
		payload := struct {
			OK bool `json:"ok"`
			app.SetupView
			Error *setupError `json:"error,omitempty"`
		}{OK: err == nil, SetupView: view}
		if err != nil {
			ce := toCoded(err)
			payload.Error = &setupError{Code: ce.code, Kind: ce.kind, Message: ce.msg}
		}
		if encErr := json.NewEncoder(stdout).Encode(payload); encErr != nil {
			return encErr
		}
		if err != nil {
			return exitWithCode(toCoded(err).code)
		}
		return nil
	}
	if !opts.quiet {
		for _, step := range view.Steps {
			mark := "x"
			if step.OK {
				mark = "v"
			}
			fmt.Fprintf(stdout, "[%s] %s: %s\n", mark, step.Name, step.Detail)
		}
		if view.Ready {
			fmt.Fprintln(stdout, "Workloom is ready.")
		}
		if view.Next != "" {
			fmt.Fprintf(stdout, "next: %s\n", view.Next)
		}
	}
	return err
}

type setupError struct {
	Code    int    `json:"code"`
	Kind    string `json:"kind"`
	Message string `json:"message"`
}

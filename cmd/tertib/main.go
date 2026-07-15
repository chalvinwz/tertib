// Command tertib checks code against team conventions defined in .tertib.yml,
// using an AI model, and reports violations with a CI-friendly exit code.
package main

import (
	"bytes"
	"context"
	"errors"
	"flag"
	"fmt"
	"io"
	"io/fs"
	"os"
	"time"

	"github.com/chalvinwz/tertib/internal/ci"
	"github.com/chalvinwz/tertib/internal/config"
	"github.com/chalvinwz/tertib/internal/dotenv"
	"github.com/chalvinwz/tertib/internal/engine"
	"github.com/chalvinwz/tertib/internal/findings"
	"github.com/chalvinwz/tertib/internal/gitdiff"
	"github.com/chalvinwz/tertib/internal/llm"
	"github.com/chalvinwz/tertib/internal/notify"
	"github.com/chalvinwz/tertib/internal/report"
	"github.com/chalvinwz/tertib/internal/secrets"
)

// version is set at build time via -ldflags "-X main.version=…".
var version = "dev"

// Exit codes. Separating "violations" from "tool error" lets a pipeline tell a
// broken convention apart from a broken run.
const (
	exitOK         = 0
	exitViolations = 1
	exitError      = 2
)

func main() {
	if len(os.Args) < 2 {
		usage(os.Stderr)
		os.Exit(exitError)
	}

	var code int
	switch os.Args[1] {
	case "init":
		code = cmdInit(os.Args[2:])
	case "validate":
		code = cmdValidate(os.Args[2:])
	case "check":
		code = cmdCheck(os.Args[2:])
	case "version", "--version", "-v":
		fmt.Println("tertib", version)
	case "help", "--help", "-h":
		usage(os.Stdout)
	default:
		fmt.Fprintf(os.Stderr, "unknown command %q\n\n", os.Args[1])
		usage(os.Stderr)
		code = exitError
	}
	os.Exit(code)
}

func usage(w *os.File) {
	fmt.Fprint(w, `tertib — enforce your team's code conventions in CI with AI

Usage:
  tertib init [--config PATH] [--force]     scaffold a .tertib.yml
  tertib validate [--config PATH]           validate a config file
  tertib check [flags]                      check code against conventions
  tertib version                            print version

Check flags:
  --config PATH     config file (default .tertib.yml)
  --all             scan all tracked files instead of the diff
  --base REF        diff base (default: CI target branch, else origin/main)
  --format FORMAT   markdown | json (default markdown)
  --output PATH     write the report to a file (default stdout)
  --fail-on LEVEL   error | warning | never (overrides config)
  --env-file PATH   load env vars from a file before resolving secrets

Exit codes:
  0  passed        1  violations at/above fail-on        2  config/runtime error
`)
}

func cmdInit(args []string) int {
	fset := flag.NewFlagSet("init", flag.ExitOnError)
	path := fset.String("config", config.DefaultPath, "config file path to create")
	force := fset.Bool("force", false, "overwrite an existing config")
	_ = fset.Parse(args)

	if !*force {
		if _, err := os.Stat(*path); err == nil {
			fmt.Fprintf(os.Stderr, "%s already exists (use --force to overwrite)\n", *path)
			return exitError
		}
	}
	if err := os.WriteFile(*path, config.Example, 0o644); err != nil {
		fmt.Fprintln(os.Stderr, "write config:", err)
		return exitError
	}
	fmt.Printf("wrote %s — edit it, then run `tertib validate`\n", *path)
	return exitOK
}

func cmdValidate(args []string) int {
	fset := flag.NewFlagSet("validate", flag.ExitOnError)
	path := fset.String("config", config.DefaultPath, "config file path")
	_ = fset.Parse(args)

	cfg, err := config.Load(*path)
	if err != nil {
		if errors.Is(err, fs.ErrNotExist) {
			fmt.Fprintf(os.Stderr, "%s not found — run `tertib init` to create one\n", *path)
			return exitError
		}
		fmt.Fprintln(os.Stderr, err)
		return exitError
	}
	fmt.Printf("%s is valid: %d rule(s)\n", *path, len(cfg.Rules))
	return exitOK
}

func cmdCheck(args []string) int {
	fset := flag.NewFlagSet("check", flag.ExitOnError)
	path := fset.String("config", config.DefaultPath, "config file path")
	all := fset.Bool("all", false, "scan all tracked files instead of the diff")
	base := fset.String("base", "", "diff base ref")
	format := fset.String("format", "markdown", "output format: markdown | json")
	output := fset.String("output", "", "write report to a file instead of stdout")
	failOn := fset.String("fail-on", "", "override config fail_on: error | warning | never")
	envFile := fset.String("env-file", "", "load environment variables from a file before resolving secrets")
	_ = fset.Parse(args)

	if *format != "markdown" && *format != "json" {
		fmt.Fprintf(os.Stderr, "invalid --format %q (want markdown or json)\n", *format)
		return exitError
	}

	cfg, err := config.Load(*path)
	if err != nil {
		if errors.Is(err, fs.ErrNotExist) {
			fmt.Fprintf(os.Stderr, "%s not found — run `tertib init` to create one\n", *path)
			return exitError
		}
		fmt.Fprintln(os.Stderr, err)
		return exitError
	}

	effectiveFailOn := cfg.Checks.FailOn
	if *failOn != "" {
		if *failOn != config.SeverityError && *failOn != config.SeverityWarning && *failOn != config.FailOnNever {
			fmt.Fprintf(os.Stderr, "invalid --fail-on %q (want error, warning, or never)\n", *failOn)
			return exitError
		}
		effectiveFailOn = *failOn
	}

	if err := loadEnvFile(*envFile, cfg.EnvFile); err != nil {
		fmt.Fprintln(os.Stderr, "load env file:", err)
		return exitError
	}

	redactor := secrets.NewRedactor()
	resolver := secrets.NewResolver(redactor)
	ctx := context.Background()

	apiKey, err := resolver.Resolve(ctx, cfg.Model.APIKey)
	if err != nil {
		fmt.Fprintln(os.Stderr, "resolve model API key:", redactor.Redact(err.Error()))
		return exitError
	}

	files, baseRef, err := collectFiles(*all, *base)
	if err != nil {
		fmt.Fprintln(os.Stderr, redactor.Redact(err.Error()))
		return exitError
	}

	client := &llm.Client{
		BaseURL:     cfg.Model.BaseURL,
		Model:       cfg.Model.Name,
		APIKey:      apiKey,
		Temperature: cfg.Model.Temperature,
		MaxTokens:   cfg.Model.MaxTokens,
		MaxRetries:  cfg.Model.MaxRetries,
		Timeout:     cfg.Model.Timeout.Duration(),
	}
	eng := engine.New(cfg, client)

	start := time.Now()
	res, err := eng.Run(ctx, files, !*all)
	if err != nil {
		fmt.Fprintln(os.Stderr, "check failed:", redactor.Redact(err.Error()))
		return exitError
	}
	// Scrub any resolved secret that leaked into a task warning before display.
	for i, w := range res.Warnings {
		res.Warnings[i] = redactor.Redact(w)
	}

	meta := report.Meta{
		Model:    cfg.Model.Name,
		BaseRef:  baseRef,
		AllMode:  *all,
		Files:    len(files),
		Duration: time.Since(start),
	}
	if err := writeReport(*format, *output, res, meta); err != nil {
		fmt.Fprintln(os.Stderr, "write report:", redactor.Redact(err.Error()))
		return exitError
	}

	failed := findings.FailsGate(res.Findings, effectiveFailOn)
	maybeNotify(ctx, cfg, resolver, redactor, res, meta, failed)

	if failed {
		return exitViolations
	}
	return exitOK
}

// loadEnvFile applies an env file before secret resolution. A path from the
// --env-file flag must exist (explicit request); a path from config is
// optional and silently skipped when absent, so a local .env is convenient
// without breaking CI where the file isn't present.
func loadEnvFile(flagPath, cfgPath string) error {
	switch {
	case flagPath != "":
		return dotenv.Load(flagPath)
	case cfgPath != "":
		if err := dotenv.Load(cfgPath); err != nil {
			if errors.Is(err, fs.ErrNotExist) {
				return nil
			}
			return err
		}
	}
	return nil
}

func collectFiles(all bool, base string) (files []gitdiff.File, baseRef string, err error) {
	if all {
		files, err = gitdiff.AllFiles()
		return files, "", err
	}
	baseRef = resolveBase(base)
	files, err = gitdiff.Changed(baseRef)
	return files, baseRef, err
}

// resolveBase picks the diff base: explicit flag wins, then the CI target
// branch, then the conventional default.
func resolveBase(flagBase string) string {
	if flagBase != "" {
		return flagBase
	}
	if b := ci.BaseRef(); b != "" {
		return b
	}
	return ci.DefaultBase
}

func writeReport(format, outPath string, res *engine.Result, meta report.Meta) error {
	var buf bytes.Buffer
	var err error
	if format == "json" {
		err = report.JSON(&buf, res, meta)
	} else {
		err = report.Markdown(&buf, res, meta)
	}
	if err != nil {
		return err
	}

	var w io.Writer = os.Stdout
	if outPath != "" {
		f, err := os.Create(outPath)
		if err != nil {
			return err
		}
		defer func() { _ = f.Close() }()
		w = f
	}
	_, err = w.Write(buf.Bytes())
	return err
}

// maybeNotify posts a summary to Discord when a webhook is configured. Any
// failure is reported to stderr but never changes the exit code.
func maybeNotify(ctx context.Context, cfg *config.Config, resolver *secrets.Resolver, redactor *secrets.Redactor, res *engine.Result, meta report.Meta, failed bool) {
	ref := cfg.Output.Notify.DiscordWebhook
	if ref.IsZero() {
		return
	}
	webhook, err := resolver.Resolve(ctx, ref)
	if err != nil {
		fmt.Fprintln(os.Stderr, "notify: resolve discord webhook:", redactor.Redact(err.Error()))
		return
	}
	if err := notify.Discord(ctx, webhook, discordSummary(res, meta, failed), 15*time.Second); err != nil {
		fmt.Fprintln(os.Stderr, "notify:", redactor.Redact(err.Error()))
	}
}

func discordSummary(res *engine.Result, meta report.Meta, failed bool) string {
	c := findings.Count(res.Findings)
	status := "✅ passed"
	if failed {
		status = "❌ failed"
	}
	scope := "diff"
	if meta.AllMode {
		scope = "all files"
	}
	return fmt.Sprintf("**tertib %s** — %d error(s), %d warning(s) across %d file(s) [%s]",
		status, c.Errors, c.Warnings, meta.Files, scope)
}

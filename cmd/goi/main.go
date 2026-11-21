//go:build !js && !wasip1

// Command goi starts, runs, builds and checks goi projects.
//
//	goi init --name myapp --id com.example.myapp --module example.com/myapp
//	goi run
//	goi build
//
// init prompts for the values a flag did not give, then writes a module, a
// cmd/main.go, a goi.yaml and an assets/ui/Main.goi holding the hello example:
// the goi logo above a greeting. run
// runs the entrypoint package with hot reload on, reading the build flags from
// goi.yaml. build writes dist/<os>-<arch>/<name> from that package with hot
// reload off. Both always add nofakecgo and set CGO_ENABLED=0.
package main

import (
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"unicode"

	"github.com/charmbracelet/huh"
	"github.com/spf13/cobra"
	"gopkg.in/yaml.v3"
)

const mainSource = `package main

import (
	"fmt"
	"os"

	"github.com/goi-gui/goi/ui"
)

func main() {
	app, err := ui.NewApp()
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
	defer app.Close()

	if _, err := app.LoadWindow("ui/Main.goi"); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
	if err := app.Run(); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}
`

const designSource = `Window {
	title:  "Hello"
	width:  640
	height: 400
	theme:  app.systemMode

	Column {
		width:          100%
		height:         100%
		crossAxisAlign: center
		mainAxisAlign:  center
		gap:            20

		SVG {
			src:    "goi://images/goi.svg"
			width:  100
			height: 100
		}

		Text {
			text: "Hello, World!"
		}
	}
}
`

// logoSVG is the mark the generated hello window shows, the same one the hello
// example uses.
const logoSVG = `<svg xmlns="http://www.w3.org/2000/svg" viewBox="0 0 200 200" width="200" height="200" role="img" aria-label="GOI logo">
  <title>GOI</title>
  <defs>
    <linearGradient id="goi-bg" x1="0" y1="0" x2="1" y2="1">
      <stop offset="0" stop-color="#2DD4F0"/>
      <stop offset="1" stop-color="#0284C7"/>
    </linearGradient>
  </defs>

  <rect x="20" y="20" width="160" height="160" rx="38" fill="url(#goi-bg)"/>

  <!-- GOI -->
  <g fill="none" stroke="#fff" stroke-width="7" stroke-linecap="round" stroke-linejoin="round">
    <path d="M81.32 87.14 A20 20 0 1 0 86 100 H73.2"/>
    <circle cx="120" cy="100" r="20"/>
    <line x1="154" y1="80" x2="154" y2="120"/>
  </g>
</svg>
`

// initOptions are the values goi init needs. A flag fills one; whatever is
// empty is asked for in the terminal.
type initOptions struct {
	Name   string
	ID     string
	Module string
	Assets string
	// Git initializes a repository; the prompt defaults it to No.
	Git bool
	// Overwrite allows the scaffold to replace files and skips the directory
	// prompt when -w is given.
	Overwrite bool
	// AskGit and AskOverwrite are set by the command for the values a flag did
	// not settle; tests can leave them false.
	AskGit       bool
	AskOverwrite bool
}

// configSource writes goi.yaml. The name and id lead because they name the app;
// buildFlags, assets and entrypoint follow the settings they belong to.
func configSource(o initOptions) string {
	return "# Application name and id, used by goi build and packaging.\n" +
		"name: " + yamlScalar(o.Name) + "\n" +
		"id: " + yamlScalar(o.ID) + "\n\n" +
		"# Build tags passed to goi run. nofakecgo and CGO_ENABLED=0 are always\n" +
		"# used; all turns on every optional feature.\n" +
		"runFlags: [all]\n\n" +
		"# Build tags passed to goi build.\n" +
		"buildFlags: [all]\n\n" +
		"# Where LoadWindow finds the design files and their assets.\n" +
		"assets: " + yamlScalar(o.Assets) + "\n\n" +
		"# Directory goi run and goi build execute.\n" +
		"entrypoint: cmd\n"
}

// yamlScalar is one value the way yaml.Marshal writes it, without the newline,
// so a name with a colon or a quote cannot break the generated file.
func yamlScalar(s string) string {
	b, _ := yaml.Marshal(s)
	return strings.TrimSuffix(string(b), "\n")
}

// config is goi.yaml. name, buildFlags and entrypoint are read by the commands;
// id and assets are for the app and LoadWindow.
type config struct {
	Name       string   `yaml:"name"`
	Entrypoint string   `yaml:"entrypoint"`
	Assets     string   `yaml:"assets"`
	RunFlags   []string `yaml:"runFlags"`
	BuildFlags []string `yaml:"buildFlags"`
}

// commandRunner runs one go command in dir. It is a value so tests can record
// the invocation instead of spawning a compiler.
type commandRunner func(dir string, args []string) error

func main() {
	if err := newRootCmd(execGo, execRun).Execute(); err != nil {
		fmt.Fprintln(os.Stderr, "goi:", err)
		os.Exit(1)
	}
}

// newRootCmd builds the command tree. The go runners are parameters so tests
// can record the invocations instead of spawning a compiler.
func newRootCmd(goCmd commandRunner, goRun runFunc) *cobra.Command {
	root := &cobra.Command{
		Use:           "goi",
		Short:         "start, run, build and check goi projects",
		SilenceErrors: true,
		SilenceUsage:  true,
	}
	root.SetOut(os.Stdout)
	root.SetErr(os.Stderr)
	root.AddCommand(newInitCmd(goCmd), newRunCmd(goRun), newBuildCmd(goRun), newSetupCmd(goRun), newUpdateCmd(goRun), newDoctorCmd(), newVersionCmd())
	return root
}

func newInitCmd(goCmd commandRunner) *cobra.Command {
	opts := initOptions{Assets: "assets"}
	cmd := &cobra.Command{
		Use:   "init",
		Short: "create a project in the current directory",
		Long:  "Create a project in the current directory. A missing name, id or module is asked for in the terminal; a non-empty directory asks before its files are replaced.",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			dir, err := os.Getwd()
			if err != nil {
				return err
			}
			// A fully flagged init is scriptable: the git question belongs to
			// the interactive flow, where another value is asked anyway.
			opts.AskGit = !cmd.Flags().Changed("git") &&
				(opts.Name == "" || opts.ID == "" || opts.Module == "")
			opts.AskOverwrite = !opts.Overwrite && !dirEmpty(dir)
			resolved, err := promptInit(opts)
			if err != nil {
				return err
			}
			if opts.AskOverwrite && !resolved.Overwrite {
				fmt.Fprintln(cmd.OutOrStdout(), "goi: init canceled")
				return nil
			}
			if err := initProject(dir, resolved, goCmd); err != nil {
				return err
			}
			if err := goCmd(dir, []string{"mod", "tidy"}); err != nil {
				// The files are usable; a missing dependency can be fetched later.
				fmt.Fprintln(cmd.ErrOrStderr(), "goi: project created, but go mod tidy failed; run it yourself:", err)
			}
			fmt.Fprintf(cmd.OutOrStdout(), "goi: created cmd/main.go, goi.yaml, .gitignore, %s/ui/Main.goi and %s/images/goi.svg\n", resolved.Assets, resolved.Assets)
			fmt.Fprintln(cmd.OutOrStdout(), "next: goi run")
			return nil
		},
	}
	f := cmd.Flags()
	f.StringVar(&opts.Name, "name", "", "application name, also the executable name")
	f.StringVar(&opts.ID, "id", "", "reverse-DNS application id")
	f.StringVar(&opts.Module, "module", "", "Go module path for go mod init")
	f.StringVar(&opts.Assets, "assets", "assets", "directory holding the ui files and their assets")
	f.BoolVarP(&opts.Git, "git", "g", false, "initialize a git repository")
	f.BoolVarP(&opts.Overwrite, "overwrite", "w", false, "overwrite files in a non-empty directory")
	return cmd
}

func newRunCmd(goRun runFunc) *cobra.Command {
	return &cobra.Command{
		Use:   "run",
		Short: "run the entrypoint package with hot reload",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			return runProject(".", goRun, cmd.OutOrStdout(), cmd.ErrOrStderr())
		},
	}
}

func newBuildCmd(goRun runFunc) *cobra.Command {
	var targetOS, targetArch string
	cmd := &cobra.Command{
		Use:   "build",
		Short: "build dist/<os>-<arch>/<name> without hot reload",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			out, err := buildProject(".", targetOS, targetArch, goRun, cmd.OutOrStdout(), cmd.ErrOrStderr())
			if err != nil {
				return err
			}
			fmt.Fprintln(cmd.OutOrStdout(), "goi: built", out)
			return nil
		},
	}
	cmd.Flags().StringVar(&targetOS, "os", runtime.GOOS, "target operating system (GOOS)")
	cmd.Flags().StringVar(&targetArch, "arch", runtime.GOARCH, "target architecture (GOARCH)")
	return cmd
}

// promptInit asks for the values the flags left empty. Nothing is asked when all
// values are already set, which keeps goi init scriptable.
func promptInit(opts initOptions) (initOptions, error) {
	var fields []huh.Field
	if opts.AskOverwrite {
		fields = append(fields, huh.NewConfirm().
			Title("Directory is not empty. Overwrite the project files?").
			Affirmative("Yes").
			Negative("No").
			Value(&opts.Overwrite))
	}
	if opts.AskGit {
		fields = append(fields, huh.NewConfirm().
			Title("Initialize a git repository?").
			Affirmative("Yes").
			Negative("No").
			Value(&opts.Git))
	}
	if opts.Name == "" {
		fields = append(fields, huh.NewInput().
			Title("Application name").
			Description("Used as the executable name by goi build.").
			Placeholder("myapp").
			Validate(required).
			Value(&opts.Name))
	}
	if opts.ID == "" {
		fields = append(fields, huh.NewInput().
			Title("Application id").
			Description("Reverse-DNS id, e.g. com.example.myapp.").
			Placeholder("com.example.myapp").
			Validate(required).
			Value(&opts.ID))
	}
	if opts.Module == "" {
		fields = append(fields, huh.NewInput().
			Title("Go module").
			Description("The module path passed to go mod init.").
			Placeholder("github.com/you/myapp").
			Validate(required).
			Value(&opts.Module))
	}
	if len(fields) == 0 {
		return opts, nil
	}
	if err := huh.NewForm(huh.NewGroup(fields...)).Run(); err != nil {
		return opts, err
	}
	return opts, nil
}

func required(s string) error {
	if strings.TrimSpace(s) == "" {
		return errors.New("required")
	}
	return nil
}

// initProject writes the scaffold and initializes the Go module. It refuses to
// overwrite a file it would create, so a second init cannot eat an app.
func initProject(dir string, opts initOptions, goRun commandRunner) error {
	opts.Name = strings.TrimSpace(opts.Name)
	opts.ID = strings.TrimSpace(opts.ID)
	opts.Module = strings.TrimSpace(opts.Module)
	opts.Assets = strings.TrimSpace(opts.Assets)
	if opts.Name == "" || opts.ID == "" || opts.Module == "" {
		return errors.New("init needs a name, id and module")
	}
	if opts.Assets == "" {
		opts.Assets = "assets"
	}
	if !opts.Overwrite {
		for _, name := range []string{"go.mod", filepath.Join("cmd", "main.go"), "goi.yaml", filepath.Join(opts.Assets, "ui", "Main.goi"), filepath.Join(opts.Assets, "images", "goi.svg")} {
			if _, err := os.Stat(filepath.Join(dir, name)); err == nil {
				return fmt.Errorf("%s already exists", name)
			} else if !errors.Is(err, os.ErrNotExist) {
				return err
			}
		}
	}
	// An existing go.mod belongs to the project being overwritten; running go
	// mod init again would fail, and tidy will still use it.
	_, modErr := os.Stat(filepath.Join(dir, "go.mod"))
	if errors.Is(modErr, os.ErrNotExist) {
		if err := goRun(dir, []string{"mod", "init", opts.Module}); err != nil {
			return err
		}
	} else if modErr != nil {
		return modErr
	}
	if err := os.MkdirAll(filepath.Join(dir, "cmd"), 0o755); err != nil {
		return err
	}
	if err := os.MkdirAll(filepath.Join(dir, opts.Assets, "ui"), 0o755); err != nil {
		return err
	}
	if err := os.MkdirAll(filepath.Join(dir, opts.Assets, "images"), 0o755); err != nil {
		return err
	}
	for name, body := range map[string]string{
		filepath.Join("cmd", "main.go"): mainSource,
		"goi.yaml":                      configSource(opts),
		filepath.Join(opts.Assets, "ui", "Main.goi"):    designSource,
		filepath.Join(opts.Assets, "images", "goi.svg"): logoSVG,
	} {
		if err := os.WriteFile(filepath.Join(dir, name), []byte(body), 0o644); err != nil {
			return err
		}
	}
	if err := ensureGitignore(dir); err != nil {
		return err
	}
	if opts.Git {
		if err := gitInit(dir); err != nil {
			return fmt.Errorf("git init: %w", err)
		}
	}
	return nil
}

// dirEmpty reports whether the directory has no entries at all.
func dirEmpty(dir string) bool {
	entries, err := os.ReadDir(dir)
	return err != nil || len(entries) == 0
}

// ensureGitignore writes dist/ into .gitignore without dropping entries the app
// already had.
func ensureGitignore(dir string) error {
	path := filepath.Join(dir, ".gitignore")
	b, err := os.ReadFile(path)
	if err != nil && !errors.Is(err, os.ErrNotExist) {
		return err
	}
	lines := strings.Split(string(b), "\n")
	for _, line := range lines {
		line = strings.TrimSpace(line)
		if line == "dist" || line == "dist/" || line == "/dist" || line == "/dist/" {
			return nil
		}
	}
	text := string(b)
	if text != "" && !strings.HasSuffix(text, "\n") {
		text += "\n"
	}
	text += "dist/\n"
	return os.WriteFile(path, []byte(text), 0o644)
}

// gitInit is a variable so a test can record the call without a git binary.
var gitInit = func(dir string) error {
	cmd := exec.Command("git", "init")
	cmd.Dir = dir
	cmd.Stdout, cmd.Stderr = os.Stdout, os.Stderr
	return cmd.Run()
}

// runFunc runs go with a project's working directory and environment. It is a
// value so tests can inspect the invocation without spawning a compiler.
type runFunc func(dir string, args []string, stdout, stderr io.Writer, env []string) error

// runProject loads goi.yaml from the project and runs cmd with the feature tags
// it lists.
func runProject(dir string, goRun runFunc, stdout, stderr io.Writer) error {
	cfg, err := loadConfig(dir)
	if err != nil {
		return err
	}
	if _, err := os.Stat(filepath.Join(dir, cfg.Entrypoint, "main.go")); err != nil {
		return fmt.Errorf("no %s/main.go in %s; run goi init first", cfg.Entrypoint, dir)
	}
	flags, err := resolveFlags(dir, cfg.Assets, cfg.RunFlags)
	if err != nil {
		return err
	}
	logTags(stdout, "run", flags)
	if flags.Auto {
		return runAutoProject(dir, cfg, flags, stdout, stderr)
	}
	return goRun(dir, runArgs(cfg, flags), stdout, stderr, runEnv())
}

// runArgs is the go run invocation for a resolved flag set.
func runArgs(cfg config, flags resolvedFlags) []string {
	args := []string{"run"}
	if tags := buildTags(flags.Tags); len(tags) > 0 {
		args = append(args, "-tags", strings.Join(tags, ","))
	}
	return append(args, "./"+filepath.ToSlash(cfg.Entrypoint))
}

// runEnv is the environment for goi run: the caller's, with CGO_ENABLED=0 and
// logfmt output, and GOI_DEBUG=TRUE unless the caller chose a value.
func runEnv() []string {
	env := envWith(os.Environ(), "CGO_ENABLED", "0")
	if _, ok := os.LookupEnv("GOI_DEBUG"); !ok {
		env = append(env, "GOI_DEBUG=TRUE")
	}
	return envWith(env, "LOGGER_OUTPUT_FORMAT", "logfmt")
}

// buildProject builds the entrypoint package for targetOS/targetArch into
// dist/<os>-<arch>/<name>. The design is copied into the package and embedded,
// so the binary is self-contained and does not watch files.
func buildProject(dir, targetOS, targetArch string, goRun runFunc, stdout, stderr io.Writer) (string, error) {
	if targetOS == "" {
		targetOS = runtime.GOOS
	}
	if targetArch == "" {
		targetArch = runtime.GOARCH
	}
	cfg, err := loadConfig(dir)
	if err != nil {
		return "", err
	}
	if _, err := os.Stat(filepath.Join(dir, cfg.Entrypoint, "main.go")); err != nil {
		return "", fmt.Errorf("no %s/main.go in %s; run goi init first", cfg.Entrypoint, dir)
	}
	if cfg.Name == "" {
		return "", errors.New("goi.yaml has no name; add name: myapp")
	}
	if cfg.Name == "." || cfg.Name == ".." || strings.ContainsAny(cfg.Name, `/\`) {
		return "", fmt.Errorf("goi.yaml name %q is not a file name", cfg.Name)
	}
	flags, err := resolveFlags(dir, cfg.Assets, cfg.BuildFlags)
	if err != nil {
		return "", err
	}
	logTags(stdout, "build", flags)
	cleanup, err := prepareEmbed(dir, cfg.Entrypoint, cfg.Assets)
	if err != nil {
		return "", err
	}
	defer cleanup()
	out := buildOutput(dir, targetOS, targetArch, cfg.Name)
	if err := os.MkdirAll(filepath.Dir(out), 0o755); err != nil {
		return "", err
	}
	pkg := "./" + filepath.ToSlash(cfg.Entrypoint)
	args := []string{"build", "-tags", strings.Join(buildTags(flags.Tags), ","),
		"-trimpath", "-ldflags", buildLDFlags(targetOS), "-o", out, pkg}
	env := envWith(os.Environ(), "CGO_ENABLED", "0")
	env = envWith(env, "GOOS", targetOS)
	env = envWith(env, "GOARCH", targetArch)
	if err := goRun(dir, args, stdout, stderr, env); err != nil {
		return "", err
	}
	return out, nil
}

// buildLDFlags strips the binary and keeps a Windows build from opening a
// console window.
func buildLDFlags(targetOS string) string {
	if targetOS == "windows" {
		return "-s -w -H windowsgui"
	}
	return "-s -w"
}

// buildOutput is dist/<os>-<arch>/<name>, with .exe only on Windows and .wasm
// for a browser target.
func buildOutput(dir, targetOS, targetArch, name string) string {
	switch {
	case targetOS == "windows" && !strings.HasSuffix(name, ".exe"):
		name += ".exe"
	case targetOS == "js" && targetArch == "wasm" && !strings.HasSuffix(name, ".wasm"):
		name += ".wasm"
	}
	return filepath.Join(dir, "dist", targetOS+"-"+targetArch, name)
}

func loadConfig(dir string) (config, error) {
	b, err := os.ReadFile(filepath.Join(dir, "goi.yaml"))
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return config{}, fmt.Errorf("no goi.yaml in %s; run goi init first", dir)
		}
		return config{}, err
	}
	var c config
	if err := yaml.Unmarshal(b, &c); err != nil {
		return config{}, fmt.Errorf("goi.yaml: %w", err)
	}
	// all is the default: a project starts with every optional feature, and
	// `buildFlags: []` is how it opts down to the core build.
	if c.RunFlags == nil {
		c.RunFlags = []string{"all"}
	}
	if c.BuildFlags == nil {
		c.BuildFlags = []string{"all"}
	}
	if c.Entrypoint == "" {
		c.Entrypoint = "cmd"
	}
	if c.Assets == "" {
		c.Assets = "assets"
	}
	return c, nil
}

// buildTags always includes nofakecgo, then every build flag, dropping empty
// values and duplicates. Commas and spaces inside one entry are split, so
// `buildFlags: ["emoji svg"]` works as well as a YAML list.
func buildTags(flags []string) []string {
	tags := []string{"nofakecgo"}
	seen := map[string]bool{"nofakecgo": true}
	for _, flag := range flags {
		for _, tag := range strings.FieldsFunc(flag, func(r rune) bool { return r == ',' || unicode.IsSpace(r) }) {
			if tag == "" || seen[tag] {
				continue
			}
			seen[tag] = true
			tags = append(tags, tag)
		}
	}
	return tags
}

// envWith replaces key in env, so a duplicate CGO_ENABLED from the caller
// cannot win over the one goi needs.
func envWith(env []string, key, value string) []string {
	out := make([]string, 0, len(env)+1)
	for _, e := range env {
		k, _, ok := strings.Cut(e, "=")
		if ok && strings.EqualFold(k, key) {
			continue
		}
		out = append(out, e)
	}
	return append(out, key+"="+value)
}

// execRun is the real go invocation for runProject and buildProject.
func execRun(dir string, args []string, stdout, stderr io.Writer, env []string) error {
	cmd := exec.Command("go", args...)
	cmd.Dir = dir
	cmd.Env = env
	cmd.Stdin, cmd.Stdout, cmd.Stderr = os.Stdin, stdout, stderr
	return cmd.Run()
}

func execGo(dir string, args []string) error {
	cmd := exec.Command("go", args...)
	cmd.Dir = dir
	cmd.Stdin, cmd.Stdout, cmd.Stderr = os.Stdin, os.Stdout, os.Stderr
	return cmd.Run()
}

// Command goi starts, runs and builds goi projects.
//
//	goi init --name myapp --id com.example.myapp --module example.com/myapp
//	goi run
//	goi build
//
// init prompts for the values a flag did not give, then writes a module, a
// cmd/main.go, a goi.yaml and a ui/Main.goi holding a centred hello world. run
// builds and runs cmd with hot reload on, reading the build flags from goi.yaml.
// build writes dist/<os>-<arch>/<name> with hot reload off. Both always add
// nofakecgo and set CGO_ENABLED=0.
package main

import (
	"errors"
	"flag"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"unicode"

	"github.com/charmbracelet/huh"
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

	if _, err := app.LoadWindow("Main.goi"); err != nil {
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
	width:  520
	height: 360
	theme:  light

	Column {
		mainAxisAlign:  center
		crossAxisAlign: center

		Text {
			text: "Hello, world!"
			role: headlineMedium
		}
	}
}
`

// initOptions are the values goi init needs. A flag fills one; whatever is
// empty is asked for in the terminal.
type initOptions struct {
	Name   string
	ID     string
	Module string
	UI     string
}

// configSource writes goi.yaml. The name and id lead because they name the app;
// buildFlags and ui follow the settings they belong to.
func configSource(o initOptions) string {
	return "# Application name and id, used by goi build and packaging.\n" +
		"name: " + yamlScalar(o.Name) + "\n" +
		"id: " + yamlScalar(o.ID) + "\n\n" +
		"# Build tags passed to goi run and goi build. nofakecgo and\n" +
		"# CGO_ENABLED=0 are always used; all turns on every optional feature.\n" +
		"buildFlags: [all]\n\n" +
		"# Where LoadWindow finds the design files.\n" +
		"ui: " + yamlScalar(o.UI) + "\n"
}

// yamlScalar is one value the way yaml.Marshal writes it, without the newline,
// so a name with a colon or a quote cannot break the generated file.
func yamlScalar(s string) string {
	b, _ := yaml.Marshal(s)
	return strings.TrimSuffix(string(b), "\n")
}

// config is goi.yaml. name and buildFlags are read by the commands; id and ui
// are for the app and LoadWindow.
type config struct {
	Name       string   `yaml:"name"`
	BuildFlags []string `yaml:"buildFlags"`
}

// commandRunner runs one go command in dir. It is a value so tests can record
// the invocation instead of spawning a compiler.
type commandRunner func(dir string, args []string) error

func main() {
	if err := run(os.Args[1:], os.Stdout, os.Stderr); err != nil {
		fmt.Fprintln(os.Stderr, "goi:", err)
		os.Exit(1)
	}
}

func run(args []string, stdout, stderr io.Writer) error {
	if len(args) == 0 {
		usage(stderr)
		return errors.New("missing command")
	}
	switch args[0] {
	case "init":
		opts, err := parseInit(args[1:], stderr)
		if err != nil {
			usage(stderr)
			return err
		}
		if opts, err = promptInit(opts); err != nil {
			return err
		}
		dir, err := os.Getwd()
		if err != nil {
			return err
		}
		if err := initProject(dir, opts, execGo); err != nil {
			return err
		}
		if err := execGo(dir, []string{"mod", "tidy"}); err != nil {
			fmt.Fprintln(stderr, "goi: project created, but go mod tidy failed; run it yourself:", err)
		}
		fmt.Fprintf(stdout, "goi: created cmd/main.go, goi.yaml and %s/Main.goi\n", opts.UI)
		fmt.Fprintln(stdout, "next: goi run")
		return nil
	case "run":
		if len(args) != 1 {
			usage(stderr)
			return errors.New("usage: goi run")
		}
		return runProject(".", execRun, stdout, stderr)
	case "build":
		if len(args) != 1 {
			usage(stderr)
			return errors.New("usage: goi build")
		}
		out, err := buildProject(".", execRun, stdout, stderr)
		if err != nil {
			return err
		}
		fmt.Fprintln(stdout, "goi: built", out)
		return nil
	case "help", "-h", "--help":
		usage(stdout)
		return nil
	default:
		usage(stderr)
		return fmt.Errorf("unknown command %q", args[0])
	}
}

func usage(w io.Writer) {
	fmt.Fprintln(w, "usage: goi <command>")
	fmt.Fprintln(w, "  init [flags]           create a project; missing values are prompted")
	fmt.Fprintln(w, "  run                    run cmd/main.go with hot reload")
	fmt.Fprintln(w, "  build                  build dist/<os>-<arch>/<name> without hot reload")
	fmt.Fprintln(w)
	fmt.Fprintln(w, "init flags: --name, --id, --module, --ui")
}

// parseInit reads the flags. A missing name, id or module is left empty for the
// prompt; --ui has a default because a project almost always keeps the files in
// ui/.
func parseInit(args []string, stderr io.Writer) (initOptions, error) {
	fs := flag.NewFlagSet("init", flag.ContinueOnError)
	fs.SetOutput(stderr)
	var opts initOptions
	fs.StringVar(&opts.Name, "name", "", "application name, also the executable name")
	fs.StringVar(&opts.ID, "id", "", "reverse-DNS application id")
	fs.StringVar(&opts.Module, "module", "", "Go module path for go mod init")
	fs.StringVar(&opts.UI, "ui", "ui", "directory holding the .goi files")
	if err := fs.Parse(args); err != nil {
		return opts, err
	}
	if fs.NArg() > 0 {
		return opts, fmt.Errorf("usage: goi init [--name] [--id] [--module] [--ui]")
	}
	if opts.UI == "" {
		opts.UI = "ui"
	}
	return opts, nil
}

// promptInit asks for the values the flags left empty. Nothing is asked when all
// three are already set, which keeps goi init scriptable.
func promptInit(opts initOptions) (initOptions, error) {
	var fields []huh.Field
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
	opts.UI = strings.TrimSpace(opts.UI)
	if opts.Name == "" || opts.ID == "" || opts.Module == "" {
		return errors.New("init needs a name, id and module")
	}
	if opts.UI == "" {
		opts.UI = "ui"
	}
	for _, name := range []string{"go.mod", filepath.Join("cmd", "main.go"), "goi.yaml", filepath.Join(opts.UI, "Main.goi")} {
		if _, err := os.Stat(filepath.Join(dir, name)); err == nil {
			return fmt.Errorf("%s already exists", name)
		} else if !errors.Is(err, os.ErrNotExist) {
			return err
		}
	}
	if err := goRun(dir, []string{"mod", "init", opts.Module}); err != nil {
		return err
	}
	if err := os.MkdirAll(filepath.Join(dir, "cmd"), 0o755); err != nil {
		return err
	}
	if err := os.MkdirAll(filepath.Join(dir, opts.UI), 0o755); err != nil {
		return err
	}
	for name, body := range map[string]string{
		filepath.Join("cmd", "main.go"):    mainSource,
		"goi.yaml":                         configSource(opts),
		filepath.Join(opts.UI, "Main.goi"): designSource,
	} {
		if err := os.WriteFile(filepath.Join(dir, name), []byte(body), 0o644); err != nil {
			return err
		}
	}
	return nil
}

// runFunc runs go with a project's working directory and environment. It is a
// value so tests can inspect the invocation without spawning a compiler.
type runFunc func(dir string, args []string, stdout, stderr io.Writer, env []string) error

// runProject loads goi.yaml from the project and runs cmd with the feature tags
// it lists.
func runProject(dir string, goRun runFunc, stdout, stderr io.Writer) error {
	if _, err := os.Stat(filepath.Join(dir, "cmd", "main.go")); err != nil {
		return fmt.Errorf("no cmd/main.go in %s; run goi init first", dir)
	}
	cfg, err := loadConfig(dir)
	if err != nil {
		return err
	}
	args := []string{"run"}
	if tags := buildTags(cfg.BuildFlags); len(tags) > 0 {
		args = append(args, "-tags", strings.Join(tags, ","))
	}
	args = append(args, "./cmd")
	return goRun(dir, args, stdout, stderr, envWith(os.Environ(), "CGO_ENABLED", "0"))
}

// buildProject builds cmd into dist/<os>-<arch>/<name>, with the build flags
// from goi.yaml and nohotreload so the binary does not watch its design files.
// GOOS and GOARCH choose the target; the host is the default when either is
// unset.
func buildProject(dir string, goRun runFunc, stdout, stderr io.Writer) (string, error) {
	if _, err := os.Stat(filepath.Join(dir, "cmd", "main.go")); err != nil {
		return "", fmt.Errorf("no cmd/main.go in %s; run goi init first", dir)
	}
	cfg, err := loadConfig(dir)
	if err != nil {
		return "", err
	}
	if cfg.Name == "" {
		return "", errors.New("goi.yaml has no name; add name: myapp")
	}
	if cfg.Name == "." || cfg.Name == ".." || strings.ContainsAny(cfg.Name, `/\`) {
		return "", fmt.Errorf("goi.yaml name %q is not a file name", cfg.Name)
	}
	targetOS := os.Getenv("GOOS")
	if targetOS == "" {
		targetOS = runtime.GOOS
	}
	targetArch := os.Getenv("GOARCH")
	if targetArch == "" {
		targetArch = runtime.GOARCH
	}
	name := cfg.Name
	if targetOS == "windows" {
		name += ".exe"
	}
	out := filepath.Join(dir, "dist", targetOS+"-"+targetArch, name)
	if err := os.MkdirAll(filepath.Dir(out), 0o755); err != nil {
		return "", err
	}
	flags := append(append([]string{}, cfg.BuildFlags...), "nohotreload")
	args := []string{"build", "-tags", strings.Join(buildTags(flags), ","), "-o", out, "./cmd"}
	if err := goRun(dir, args, stdout, stderr, envWith(os.Environ(), "CGO_ENABLED", "0")); err != nil {
		return "", err
	}
	return out, nil
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
	if c.BuildFlags == nil {
		c.BuildFlags = []string{"all"}
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

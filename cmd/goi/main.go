//go:build !js && !wasip1

// Command goi starts, runs, builds and checks goi projects.
//
//	goi init --name myapp --id com.example.myapp --module example.com/myapp
//	goi run
//	goi build
//
// init prompts for the values a flag did not give, then writes a module, a
// cmd/main.go, a goi.yaml and a ui/Main.goi holding a centred hello world. run
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
// buildFlags, ui and entrypoint follow the settings they belong to.
func configSource(o initOptions) string {
	return "# Application name and id, used by goi build and packaging.\n" +
		"name: " + yamlScalar(o.Name) + "\n" +
		"id: " + yamlScalar(o.ID) + "\n\n" +
		"# Build tags passed to goi run and goi build. nofakecgo and\n" +
		"# CGO_ENABLED=0 are always used; all turns on every optional feature.\n" +
		"buildFlags: [all]\n\n" +
		"# Where LoadWindow finds the design files.\n" +
		"ui: " + yamlScalar(o.UI) + "\n\n" +
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
// id and ui are for the app and LoadWindow.
type config struct {
	Name       string   `yaml:"name"`
	Entrypoint string   `yaml:"entrypoint"`
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
	root.AddCommand(newInitCmd(goCmd), newRunCmd(goRun), newBuildCmd(goRun), newUpdateCmd(goRun), newDoctorCmd())
	return root
}

func newInitCmd(goCmd commandRunner) *cobra.Command {
	opts := initOptions{UI: "ui"}
	cmd := &cobra.Command{
		Use:   "init",
		Short: "create a project in the current directory",
		Long:  "Create a project in the current directory. A missing name, id or module is asked for in the terminal.",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			resolved, err := promptInit(opts)
			if err != nil {
				return err
			}
			dir, err := os.Getwd()
			if err != nil {
				return err
			}
			if err := initProject(dir, resolved, goCmd); err != nil {
				return err
			}
			if err := goCmd(dir, []string{"mod", "tidy"}); err != nil {
				// The files are usable; a missing dependency can be fetched later.
				fmt.Fprintln(cmd.ErrOrStderr(), "goi: project created, but go mod tidy failed; run it yourself:", err)
			}
			fmt.Fprintf(cmd.OutOrStdout(), "goi: created cmd/main.go, goi.yaml and %s/Main.goi\n", resolved.UI)
			fmt.Fprintln(cmd.OutOrStdout(), "next: goi run")
			return nil
		},
	}
	f := cmd.Flags()
	f.StringVar(&opts.Name, "name", "", "application name, also the executable name")
	f.StringVar(&opts.ID, "id", "", "reverse-DNS application id")
	f.StringVar(&opts.Module, "module", "", "Go module path for go mod init")
	f.StringVar(&opts.UI, "ui", "ui", "directory holding the .goi files")
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
	return &cobra.Command{
		Use:   "build",
		Short: "build dist/<os>-<arch>/<name> without hot reload",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			out, err := buildProject(".", goRun, cmd.OutOrStdout(), cmd.ErrOrStderr())
			if err != nil {
				return err
			}
			fmt.Fprintln(cmd.OutOrStdout(), "goi: built", out)
			return nil
		},
	}
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
	cfg, err := loadConfig(dir)
	if err != nil {
		return err
	}
	pkg := "./" + filepath.ToSlash(cfg.Entrypoint)
	if _, err := os.Stat(filepath.Join(dir, cfg.Entrypoint, "main.go")); err != nil {
		return fmt.Errorf("no %s/main.go in %s; run goi init first", cfg.Entrypoint, dir)
	}
	args := []string{"run"}
	if tags := buildTags(cfg.BuildFlags); len(tags) > 0 {
		args = append(args, "-tags", strings.Join(tags, ","))
	}
	args = append(args, pkg)
	return goRun(dir, args, stdout, stderr, envWith(os.Environ(), "CGO_ENABLED", "0"))
}

// buildProject builds the entrypoint package into dist/<os>-<arch>/<name>, with
// the build flags from goi.yaml and nohotreload so the binary does not watch its
// design files. GOOS and GOARCH choose the target; the host is the default when
// either is unset.
func buildProject(dir string, goRun runFunc, stdout, stderr io.Writer) (string, error) {
	cfg, err := loadConfig(dir)
	if err != nil {
		return "", err
	}
	pkg := "./" + filepath.ToSlash(cfg.Entrypoint)
	if _, err := os.Stat(filepath.Join(dir, cfg.Entrypoint, "main.go")); err != nil {
		return "", fmt.Errorf("no %s/main.go in %s; run goi init first", cfg.Entrypoint, dir)
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
	if targetOS == "windows" && !strings.HasSuffix(name, ".exe") {
		name += ".exe"
	}
	out := filepath.Join(dir, "dist", targetOS+"-"+targetArch, name)
	if err := os.MkdirAll(filepath.Dir(out), 0o755); err != nil {
		return "", err
	}
	flags := append(append([]string{}, cfg.BuildFlags...), "nohotreload")
	args := []string{"build", "-tags", strings.Join(buildTags(flags), ","), "-o", out, pkg}
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
	if c.Entrypoint == "" {
		c.Entrypoint = "cmd"
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

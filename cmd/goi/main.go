// Command goi starts and runs goi projects.
//
//	goi init example.com/hello
//	goi run
//
// init writes a module, a cmd/main.go, a goi.yaml and a ui/Main.goi holding a
// centred hello world. run reads the build flags from goi.yaml and passes them
// as build tags, with nofakecgo always on and CGO_ENABLED=0 in the environment.
package main

import (
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"unicode"

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

const configSource = `# Build tags passed to goi run. nofakecgo and CGO_ENABLED=0 are always used.
# all turns on every optional feature: emoji, svg, mermaid, markdown, code,
# net and dialog.
buildFlags: [all]

# Where LoadWindow finds the design files.
ui: ui
`

// config is goi.yaml. Only the buildFlags list is read today.
type config struct {
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
		if len(args) != 2 || args[1] == "" {
			usage(stderr)
			return errors.New("usage: goi init <go-module-name>")
		}
		dir, err := os.Getwd()
		if err != nil {
			return err
		}
		if err := initProject(dir, args[1], execGo); err != nil {
			return err
		}
		if err := execGo(dir, []string{"mod", "tidy"}); err != nil {
			fmt.Fprintln(stderr, "goi: project created, but go mod tidy failed; run it yourself:", err)
		}
		fmt.Fprintln(stdout, "goi: created cmd/main.go, goi.yaml and ui/Main.goi")
		fmt.Fprintln(stdout, "next: goi run")
		return nil
	case "run":
		if len(args) != 1 {
			usage(stderr)
			return errors.New("usage: goi run")
		}
		return runProject(".", execRun, stdout, stderr)
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
	fmt.Fprintln(w, "  init <go-module-name>  create a project in the current directory")
	fmt.Fprintln(w, "  run                    run cmd/main.go, using its goi.yaml build flags")
}

// initProject writes the scaffold and initializes the Go module. It refuses to
// overwrite a file it would create, so a second init cannot eat an app.
func initProject(dir, module string, goRun commandRunner) error {
	for _, name := range []string{"go.mod", filepath.Join("cmd", "main.go"), "goi.yaml", filepath.Join("ui", "Main.goi")} {
		if _, err := os.Stat(filepath.Join(dir, name)); err == nil {
			return fmt.Errorf("%s already exists", name)
		} else if !errors.Is(err, os.ErrNotExist) {
			return err
		}
	}
	if err := goRun(dir, []string{"mod", "init", module}); err != nil {
		return err
	}
	if err := os.MkdirAll(filepath.Join(dir, "cmd"), 0o755); err != nil {
		return err
	}
	if err := os.MkdirAll(filepath.Join(dir, "ui"), 0o755); err != nil {
		return err
	}
	for name, body := range map[string]string{
		filepath.Join("cmd", "main.go"): mainSource,
		"goi.yaml":                      configSource,
		filepath.Join("ui", "Main.goi"): designSource,
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

// execRun is the real go invocation for runProject.
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

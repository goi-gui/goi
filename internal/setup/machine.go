package setup

import "github.com/traefik/yaegi/interp"

// machine is the one interpreter every setup file shares. Each file is its own
// Yaegi package, so its names stay private while fmt and its generic sources
// are compiled once instead of once per file.
type machine struct {
	i          *interp.Interpreter
	registered map[string]bool
	data       func(string) (any, bool)
	version    func() uint64
	call       func(string, []any) (any, error)
	set        func(string, any)
}

func newMachine() *machine {
	return &machine{
		i:          interp.New(interp.Options{}),
		registered: map[string]bool{},
	}
}

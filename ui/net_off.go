//go:build nonet

package ui

import (
	"errors"

	"github.com/goi-gui/goi/internal/img"
)

// errNoNetwork is what a remote src reports in a build without net/http.
var errNoNetwork = errors.New("ui: this build has no network support (-tags nonet)")

func fetchRemote(src string) ([]byte, error) { return nil, errNoNetwork }

func fetchRemoteImage(src string) (*img.Data, *img.Anim, error) {
	return nil, nil, errNoNetwork
}

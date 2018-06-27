package xrandr

import (
	"crypto/md5"
	"encoding/hex"
	"io"
	"os/exec"

	"github.com/seeruk/i3adc/xrandr/props"
)

// getProps is a reference to a function that fetches props (or at least props-like data). Can be
// swapped out in tests.
var getProps = getPropsWithExec

// getPropsWithExec uses the xrandr command, hopefully on the user's $PATH to fetch the props data
// as a byte representing the string of output.
func getPropsWithExec() ([]byte, error) {
	command := exec.Command("xrandr", "--props")
	return command.CombinedOutput()
}

// parseProps takes the binary props output and returns the parsed result, as a slice of output
// structs that we can pull display properties from.
func parseProps(rawProps []byte) ([]props.Output, error) {
	parser := props.NewParser(false)
	parsed, err := parser.ParseProps(rawProps)
	if err != nil {
		return nil, err
	}

	return parsed.Outputs, nil
}

// calculateHashForOutputs takes a set of outputs and produces an MD5 hash of all of the properties
// of the connected outputs. This serves as a way of uniquely identifying a set of outputs.
func calculateHashForOutputs(outputs []props.Output) (string, error) {
	sum := md5.New()

	for _, output := range outputs {
		if !output.IsConnected {
			continue
		}

		io.WriteString(sum, output.Name)
		io.WriteString(sum, output.Properties["EDID"])
	}

	return hex.EncodeToString(sum.Sum(nil)), nil
}

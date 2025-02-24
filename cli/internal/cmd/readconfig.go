/*
Copyright (c) Edgeless Systems GmbH

SPDX-License-Identifier: AGPL-3.0-only
*/

package cmd

import (
	"errors"
	"fmt"
	"io"

	"github.com/edgelesssys/constellation/internal/config"
	"github.com/edgelesssys/constellation/internal/file"
)

func readConfig(out io.Writer, fileHandler file.Handler, name string) (*config.Config, error) {
	if name == "" {
		return config.Default(), nil
	}

	cnf, err := config.FromFile(fileHandler, name)
	if err != nil {
		return nil, err
	}
	if err := validateConfig(out, cnf); err != nil {
		return nil, err
	}

	return cnf, nil
}

func validateConfig(out io.Writer, cnf *config.Config) error {
	msgs, err := cnf.Validate()
	if err != nil {
		return fmt.Errorf("performing config validation: %w", err)
	}

	if len(msgs) > 0 {
		fmt.Fprintln(out, "Invalid fields in config file:")
		for _, m := range msgs {
			fmt.Fprintln(out, "\t"+m)
		}
		fmt.Fprintln(out, "Fix the invalid entries or generate a new configuration using `constellation config generate`")
		return errors.New("invalid configuration")
	}

	return nil
}

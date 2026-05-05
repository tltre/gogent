package cli

import "github.com/spf13/cobra"

type CommandBuilder func(cli *DefaultCLI) *cobra.Command

type CommandEntry struct {
	Path  string
	Build CommandBuilder
}

func RegisterByPath(cli *DefaultCLI, entries []CommandEntry) {
	for _, entry := range entries {
		var cmd *cobra.Command
		if entry.Build != nil {
			cmd = entry.Build(cli)
		}
		if cmd != nil {
			cli.Register(entry.Path, cmd)
		} else {
			cli.Unregister(entry.Path)
		}
	}
}

package cli

import (
	"context"
	"fmt"
	"sort"
	"strings"

	"github.com/spf13/cobra"
	"github.com/tltre/gogent/pkg/component"
	"github.com/tltre/gogent/pkg/iface"
)

type DefaultCLI struct {
	Banner       string
	Prompt       string
	reg          *component.Registry
	commands     map[string]*cobra.Command
	unregistered map[string]bool
}

var _ iface.Interface = (*DefaultCLI)(nil)

func New(banner, prompt string) *DefaultCLI {
	if prompt == "" {
		prompt = "> "
	}
	return &DefaultCLI{
		Banner:       banner,
		Prompt:       prompt,
		commands:     make(map[string]*cobra.Command),
		unregistered: make(map[string]bool),
	}
}

func (c *DefaultCLI) Registry() *component.Registry {
	return c.reg
}

func (c *DefaultCLI) Register(path string, cmd *cobra.Command) {
	delete(c.unregistered, path)
	c.commands[path] = cmd
}

func (c *DefaultCLI) Unregister(path string) {
	for key := range c.commands {
		if key == path || strings.HasPrefix(key, path+".") {
			delete(c.commands, key)
		}
	}
	c.unregistered[path] = true
	for key := range c.unregistered {
		if key != path && strings.HasPrefix(key, path+".") {
			delete(c.unregistered, key)
		}
	}
}

func (c *DefaultCLI) EnsureDefaults() {
	if _, exists := c.commands["chat"]; !exists && !c.unregistered["chat"] {
		c.Register("chat", buildChat(c))
	}
	if _, exists := c.commands["run"]; !exists && !c.unregistered["run"] {
		c.Register("run", buildRun(c))
	}
	if _, exists := c.commands["version"]; !exists && !c.unregistered["version"] {
		c.Register("version", buildVersion(c))
	}
}

func (c *DefaultCLI) BuildRoot() *cobra.Command {
	root := &cobra.Command{
		Use:   "agent",
		Short: "Gogent agent CLI",
	}

	paths := make([]string, 0, len(c.commands))
	for p := range c.commands {
		paths = append(paths, p)
	}
	sort.Slice(paths, func(i, j int) bool {
		return strings.Count(paths[i], ".") < strings.Count(paths[j], ".")
	})

	nodes := make(map[string]*cobra.Command)

	// First pass: create all nodes (placeholders for intermediate, real for leaves)
	for _, path := range paths {
		parts := strings.Split(path, ".")
		for i := 0; i < len(parts); i++ {
			prefix := strings.Join(parts[:i+1], ".")
			if _, exists := nodes[prefix]; exists {
				continue
			}
			if i == len(parts)-1 {
				cmd := c.commands[path]
				cmd.Use = parts[i]
				nodes[prefix] = cmd
			} else {
				nodes[prefix] = &cobra.Command{Use: parts[i]}
			}
		}
	}

	// Second pass: wire parent-child relationships
	for prefix, node := range nodes {
		parts := strings.Split(prefix, ".")
		if len(parts) == 1 {
			root.AddCommand(node)
		} else {
			parentPath := strings.Join(parts[:len(parts)-1], ".")
			if parent, ok := nodes[parentPath]; ok {
				parent.AddCommand(node)
			}
		}
	}

	if chatCmd, exists := c.commands["chat"]; exists {
		root.RunE = chatCmd.RunE
	}

	return root
}

func (c *DefaultCLI) Run(ctx context.Context, reg *component.Registry) error {
	c.reg = reg
	c.EnsureDefaults()

	root := c.BuildRoot()

	if c.Banner != "" {
		fmt.Println(c.Banner)
	}

	root.SetArgs([]string{})
	return root.ExecuteContext(ctx)
}

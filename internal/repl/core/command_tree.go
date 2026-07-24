package core

import (
	"context"
	"flag"
	"fmt"
	"io"
)

// CommandHandler is the handler function type for executing a command
type CommandHandler func(ctx context.Context, args []string) error

// Middleware represents an interceptor function around a CommandHandler
type Middleware func(next CommandHandler) CommandHandler

// CommandNode represents a node in the composite command tree
type CommandNode struct {
	Name        string
	Description string
	Usage       string
	Flags       *flag.FlagSet
	Handler     CommandHandler
	Subcommands map[string]*CommandNode
	Middleware  []Middleware
}

// NewCommandNode creates a new command tree node
func NewCommandNode(name, description string) *CommandNode {
	return &CommandNode{
		Name:        name,
		Description: description,
		Subcommands: make(map[string]*CommandNode),
		Middleware:  make([]Middleware, 0),
	}
}

// AddSubcommand attaches a child command node
func (n *CommandNode) AddSubcommand(sub *CommandNode) {
	if n.Subcommands == nil {
		n.Subcommands = make(map[string]*CommandNode)
	}
	n.Subcommands[sub.Name] = sub
}

// Use adds a middleware interceptor to this command node
func (n *CommandNode) Use(m ...Middleware) {
	n.Middleware = append(n.Middleware, m...)
}

// Execute resolves subcommands and flags, wrapping with middleware before calling handler
func (n *CommandNode) Execute(ctx context.Context, args []string) error {
	if len(args) > 0 {
		subName := args[0]
		if sub, exists := n.Subcommands[subName]; exists {
			return sub.Execute(ctx, args[1:])
		}
	}

	if n.Handler == nil {
		return n.ShowHelp(ctx)
	}

	// Parse flags if present
	remainingArgs := args
	if n.Flags != nil {
		n.Flags.SetOutput(io.Discard)
		if err := n.Flags.Parse(args); err != nil {
			return fmt.Errorf("flag parse error for command '%s': %w", n.Name, err)
		}
		remainingArgs = n.Flags.Args()
	}

	// Chain middleware around handler
	finalHandler := n.Handler
	for i := len(n.Middleware) - 1; i >= 0; i-- {
		finalHandler = n.Middleware[i](finalHandler)
	}

	return finalHandler(ctx, remainingArgs)
}

// ShowHelp prints help for this command node
func (n *CommandNode) ShowHelp(ctx context.Context) error {
	fmt.Printf("Command: %s\nDescription: %s\n", n.Name, n.Description)
	if n.Usage != "" {
		fmt.Printf("Usage: %s\n", n.Usage)
	}
	if len(n.Subcommands) > 0 {
		fmt.Println("Available Subcommands:")
		for name, sub := range n.Subcommands {
			fmt.Printf("  %-15s - %s\n", name, sub.Description)
		}
	}
	return nil
}

// CommandRegistry manages top-level registered commands
type CommandRegistry struct {
	root  *CommandNode
	lexer *Lexer
}

// NewCommandRegistry creates a new command registry
func NewCommandRegistry() *CommandRegistry {
	root := NewCommandNode("root", "Root REPL command router")
	return &CommandRegistry{
		root:  root,
		lexer: NewLexer(),
	}
}

// Register adds a top-level command node
func (r *CommandRegistry) Register(node *CommandNode) {
	r.root.AddSubcommand(node)
}

// ExecuteLine parses and executes a raw line input
func (r *CommandRegistry) ExecuteLine(ctx context.Context, line string) error {
	args, err := r.lexer.Tokenize(line)
	if err != nil {
		return fmt.Errorf("syntax error in input: %w", err)
	}
	if len(args) == 0 {
		return nil
	}

	cmdName := args[0]
	sub, exists := r.root.Subcommands[cmdName]
	if !exists {
		return fmt.Errorf("unknown command '%s'. Type 'help' for available commands", cmdName)
	}

	return sub.Execute(ctx, args[1:])
}

// ListCommands returns descriptions of all registered top-level commands
func (r *CommandRegistry) ListCommands() map[string]string {
	res := make(map[string]string)
	for name, node := range r.root.Subcommands {
		res[name] = node.Description
	}
	return res
}

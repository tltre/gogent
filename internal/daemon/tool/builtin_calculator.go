package tool

import (
	"context"
	"fmt"
	"go/ast"
	"go/parser"
	"go/token"
	"strconv"
)

// calculator evaluates mathematical expressions using Go's stdlib AST parser.
// Zero external dependencies. Supports: +, -, *, /, parentheses, unary minus.
type calculator struct{}

func (c *calculator) exec(_ context.Context, params map[string]any) (Result, error) {
	expr, _ := params["expr"].(string)
	if expr == "" {
		return Result{IsError: true, ErrorMsg: "calculator: expr is required"}, nil
	}

	result, err := c.eval(expr)
	if err != nil {
		return Result{IsError: true, ErrorMsg: "calculator: " + err.Error()}, nil
	}
	return Result{Output: result, IsError: false}, nil
}

// eval parses and evaluates a mathematical expression string.
func (c *calculator) eval(input string) (float64, error) {
	tree, err := parser.ParseExpr(input)
	if err != nil {
		return 0, fmt.Errorf("invalid expression: %w", err)
	}
	return c.walk(tree)
}

// walk recursively evaluates an AST node.
func (c *calculator) walk(node ast.Expr) (float64, error) {
	switch n := node.(type) {
	case *ast.BasicLit:
		return strconv.ParseFloat(n.Value, 64)

	case *ast.ParenExpr:
		return c.walk(n.X)

	case *ast.UnaryExpr:
		v, err := c.walk(n.X)
		if err != nil {
			return 0, err
		}
		if n.Op == token.SUB {
			return -v, nil
		}
		return v, nil

	case *ast.BinaryExpr:
		left, err := c.walk(n.X)
		if err != nil {
			return 0, err
		}
		right, err := c.walk(n.Y)
		if err != nil {
			return 0, err
		}
		switch n.Op {
		case token.ADD:
			return left + right, nil
		case token.SUB:
			return left - right, nil
		case token.MUL:
			return left * right, nil
		case token.QUO:
			if right == 0 {
				return 0, fmt.Errorf("division by zero")
			}
			return left / right, nil
		default:
			return 0, fmt.Errorf("unsupported operator: %s", n.Op)
		}

	default:
		return 0, fmt.Errorf("unsupported expression")
	}
}

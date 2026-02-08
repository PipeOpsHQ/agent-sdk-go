package tools

import (
	"context"
	"encoding/json"
	"fmt"
	"go/ast"
	"go/parser"
	"go/token"
	"strconv"
)

type calculatorArgs struct {
	Expression string `json:"expression"`
}

func NewCalculator() Tool {
	schema := map[string]any{
		"type": "object",
		"properties": map[string]any{
			"expression": map[string]any{
				"type":        "string",
				"description": "Arithmetic expression to evaluate, e.g. (2+3)*4 or 10/5.",
			},
		},
		"required": []string{"expression"},
	}

	return NewFuncTool(
		"calculator",
		"Evaluate arithmetic expressions with +, -, *, /, and parentheses.",
		schema,
		func(ctx context.Context, args json.RawMessage) (any, error) {
			_ = ctx
			var in calculatorArgs
			if err := json.Unmarshal(args, &in); err != nil {
				return nil, fmt.Errorf("invalid calculator args: %w", err)
			}
			if in.Expression == "" {
				return nil, fmt.Errorf("expression is required")
			}

			val, err := evalArithmetic(in.Expression)
			if err != nil {
				return nil, err
			}
			return map[string]any{
				"result": strconv.FormatFloat(val, 'f', -1, 64),
			}, nil
		},
	)
}

func evalArithmetic(expression string) (float64, error) {
	parsed, err := parser.ParseExpr(expression)
	if err != nil {
		return 0, fmt.Errorf("failed to parse expression: %w", err)
	}
	return evalNode(parsed)
}

func evalNode(node ast.Expr) (float64, error) {
	switch n := node.(type) {
	case *ast.BasicLit:
		if n.Kind != token.INT && n.Kind != token.FLOAT {
			return 0, fmt.Errorf("unsupported literal: %s", n.Value)
		}
		v, err := strconv.ParseFloat(n.Value, 64)
		if err != nil {
			return 0, fmt.Errorf("invalid number %q: %w", n.Value, err)
		}
		return v, nil

	case *ast.ParenExpr:
		return evalNode(n.X)

	case *ast.UnaryExpr:
		v, err := evalNode(n.X)
		if err != nil {
			return 0, err
		}
		switch n.Op {
		case token.ADD:
			return v, nil
		case token.SUB:
			return -v, nil
		default:
			return 0, fmt.Errorf("unsupported unary operator: %s", n.Op)
		}

	case *ast.BinaryExpr:
		left, err := evalNode(n.X)
		if err != nil {
			return 0, err
		}
		right, err := evalNode(n.Y)
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
		return 0, fmt.Errorf("unsupported expression type: %T", node)
	}
}

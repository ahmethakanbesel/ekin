package main

import (
	"context"

	"kurgu/internal/agenttool"
)

// tool runs `kurgu tool ...`, the commands the authoring agent may call.
// It needs no database or configuration beyond the KURGU_* variables the
// worker sets for the agent.
func tool(ctx context.Context, args []string) error {
	env, err := agenttool.FromEnviron()
	if err != nil {
		return err
	}
	return agenttool.Run(ctx, env, args)
}

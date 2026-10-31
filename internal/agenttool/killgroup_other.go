//go:build !unix

package agenttool

import "os/exec"

func killGroup(*exec.Cmd) {}

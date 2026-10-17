package agent

import (
	"os/exec"

	"noe/internal/processscope"
)

type processScopeAdapter struct {
	scope processscope.Scope
}

func newProcessScope(cmd *exec.Cmd) (processScope, error) {
	scope, err := processscope.New(cmd)
	if err != nil {
		return nil, err
	}
	return &processScopeAdapter{scope: scope}, nil
}

func (s *processScopeAdapter) start() error     { return s.scope.Start() }
func (s *processScopeAdapter) terminate() error { return s.scope.Kill() }
func (s *processScopeAdapter) close() error     { return s.scope.Close() }

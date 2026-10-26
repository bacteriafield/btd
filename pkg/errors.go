package pkg

import "github.com/leonelquinteros/gotext"

type ErrPrivilegeElevatorNotFound struct {
	ConfValue string
}

func (e *ErrPrivilegeElevatorNotFound) Error() string {
	return gotext.Get("no privilege elevator found (tried %s, sudo, run0, doas and pkexec)", e.ConfValue)
}

package models

import "errors"

var ErrSourcePacingAtomic = errors.New("source scheduling did not finish atomically")

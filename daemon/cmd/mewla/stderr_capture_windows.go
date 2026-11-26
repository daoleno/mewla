package main

import (
	"errors"
	"os"
)

func redirectFD(*os.File, int) error { return errors.New("not on Windows") }

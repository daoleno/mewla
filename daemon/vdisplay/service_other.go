//go:build !linux

package vdisplay

import (
	"context"
	"errors"
	"image"
)

var errUnsupported = errors.New("virtual displays are only available on Linux")

// Service is not available off Linux; every call says so.
type Service struct{}

// NewService returns the stub.
func NewService(string) *Service { return &Service{} }

func (*Service) Act(context.Context, string, Action) (Result, error) { return Result{}, errUnsupported }
func (*Service) View(string) (*Viewer, error)                        { return nil, errUnsupported }
func (*Service) Halt(string) error                                   { return errUnsupported }
func (*Service) Resume(string) error                                 { return errUnsupported }
func (*Service) List() []Info                                        { return nil }
func (*Service) SetAlive(func(string) bool)                          {}
func (*Service) Forget(string)                                       {}
func (*Service) Close()                                              {}

func (*Service) Begin(string) error                              { return nil }
func (*Service) End(string) []string                             { return nil }
func (*Service) Windows(string, int)                             {}
func (*Service) DisplayEnv(context.Context) (int, string, error) { return 0, "", errUnsupported }
func (*Service) Monitor(context.Context, string) (image.Rectangle, error) {
	return image.Rectangle{}, errUnsupported
}

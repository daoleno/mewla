package screen

import (
	"log"
	"os/exec"
	"sync"
)

// Power is this computer's power state as a viewer needs it.
type Power struct {
	// Source is "ac", "battery" or empty when unknown.
	Source   string `json:"source,omitempty"`
	Percent  int    `json:"percent,omitempty"`
	Charging bool   `json:"charging,omitempty"`
	// LidClosed is set only on laptops; a closed lid without an external
	// display sleeps regardless of any keep-awake hold.
	LidClosed    *bool `json:"lid_closed,omitempty"`
	KeepingAwake bool  `json:"keeping_awake"`
}

// keeper owns one keep-awake child process.
type keeper struct {
	// command makes the hold; keepAwakeCommand when nil.
	command func() *exec.Cmd

	mu  sync.Mutex
	cmd *exec.Cmd
}

func (k *keeper) set(on bool) {
	k.mu.Lock()
	defer k.mu.Unlock()
	if on == (k.cmd != nil) {
		return
	}
	if !on {
		_ = k.cmd.Process.Kill()
		k.cmd = nil
		return
	}
	start := keepAwakeCommand
	if k.command != nil {
		start = k.command
	}
	cmd := start()
	if cmd == nil {
		return
	}
	if err := cmd.Start(); err != nil {
		log.Printf("screen: keep awake: %v", err)
		return
	}
	k.cmd = cmd
	go func() {
		_ = cmd.Wait()
		k.mu.Lock()
		if k.cmd == cmd {
			k.cmd = nil
		}
		k.mu.Unlock()
	}()
}

func (k *keeper) holding() bool {
	k.mu.Lock()
	defer k.mu.Unlock()
	return k.cmd != nil
}

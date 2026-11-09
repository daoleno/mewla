package platform

import (
	"encoding/xml"
	"strings"
	"unicode/utf16"
)

// LogonTask is a per-user Task Scheduler task that starts a program when this
// user logs in. A standard user can register it (no administrator): it runs
// with the user's own, least-privileged token in their interactive session.
type LogonTask struct {
	// Name is the task's name in Task Scheduler's root folder.
	Name string
	// User is DOMAIN\name and SID the user's SID; both name the one user
	// whose logon starts the task and whose token runs it.
	User, SID string
	// Command and Arguments are the program and its command line.
	Command, Arguments string
	WorkDir            string
	Description        string
}

func xmlText(value string) string {
	var b strings.Builder
	_ = xml.EscapeText(&b, []byte(value))
	return b.String()
}

// XML renders the task definition. The settings keep a daemon running: no
// time limit (the default stops a task after 72 hours), no stop on battery,
// and restart after a failure.
func (t LogonTask) XML() string {
	var b strings.Builder
	b.WriteString(`<?xml version="1.0" encoding="UTF-16"?>` + "\n")
	b.WriteString(`<Task version="1.2" xmlns="http://schemas.microsoft.com/windows/2004/02/mit/task">` + "\n")
	b.WriteString("  <RegistrationInfo><Description>" + xmlText(t.Description) + "</Description></RegistrationInfo>\n")
	b.WriteString("  <Triggers><LogonTrigger><Enabled>true</Enabled><UserId>" + xmlText(t.User) + "</UserId></LogonTrigger></Triggers>\n")
	b.WriteString("  <Principals><Principal id=\"Author\"><UserId>" + xmlText(t.SID) + "</UserId>")
	b.WriteString("<LogonType>InteractiveToken</LogonType><RunLevel>LeastPrivilege</RunLevel></Principal></Principals>\n")
	b.WriteString("  <Settings>\n")
	b.WriteString("    <MultipleInstancesPolicy>IgnoreNew</MultipleInstancesPolicy>\n")
	b.WriteString("    <DisallowStartIfOnBatteries>false</DisallowStartIfOnBatteries>\n")
	b.WriteString("    <StopIfGoingOnBatteries>false</StopIfGoingOnBatteries>\n")
	b.WriteString("    <AllowHardTerminate>true</AllowHardTerminate>\n")
	b.WriteString("    <StartWhenAvailable>true</StartWhenAvailable>\n")
	b.WriteString("    <IdleSettings><StopOnIdleEnd>false</StopOnIdleEnd><RestartOnIdle>false</RestartOnIdle></IdleSettings>\n")
	b.WriteString("    <AllowStartOnDemand>true</AllowStartOnDemand>\n")
	b.WriteString("    <Enabled>true</Enabled>\n")
	b.WriteString("    <Hidden>false</Hidden>\n")
	b.WriteString("    <ExecutionTimeLimit>PT0S</ExecutionTimeLimit>\n")
	b.WriteString("    <Priority>7</Priority>\n")
	b.WriteString("    <RestartOnFailure><Interval>PT1M</Interval><Count>3</Count></RestartOnFailure>\n")
	b.WriteString("  </Settings>\n")
	b.WriteString("  <Actions Context=\"Author\"><Exec>")
	b.WriteString("<Command>" + xmlText(t.Command) + "</Command>")
	if t.Arguments != "" {
		b.WriteString("<Arguments>" + xmlText(t.Arguments) + "</Arguments>")
	}
	if t.WorkDir != "" {
		b.WriteString("<WorkingDirectory>" + xmlText(t.WorkDir) + "</WorkingDirectory>")
	}
	b.WriteString("</Exec></Actions>\n")
	b.WriteString("</Task>\n")
	return b.String()
}

// UTF16File is the task XML as schtasks /xml reads it: UTF-16LE with a BOM,
// matching the encoding the declaration names.
func UTF16File(text string) []byte {
	units := utf16.Encode([]rune(text))
	out := make([]byte, 2, 2+2*len(units))
	out[0], out[1] = 0xff, 0xfe
	for _, unit := range units {
		out = append(out, byte(unit), byte(unit>>8))
	}
	return out
}

// QuoteArgs joins arguments into a Windows command line, quoting each as
// CommandLineToArgvW and the Go runtime parse it.
func QuoteArgs(args []string) string {
	quoted := make([]string, len(args))
	for i, arg := range args {
		quoted[i] = quoteArg(arg)
	}
	return strings.Join(quoted, " ")
}

func quoteArg(arg string) string {
	if arg != "" && !strings.ContainsAny(arg, " \t\n\v\"") {
		return arg
	}
	var b strings.Builder
	b.WriteByte('"')
	slashes := 0
	for _, r := range arg {
		switch r {
		case '\\':
			slashes++
		case '"':
			b.WriteString(strings.Repeat(`\`, 2*slashes+1))
			b.WriteRune(r)
			slashes = 0
			continue
		default:
			if slashes > 0 {
				b.WriteString(strings.Repeat(`\`, slashes))
				slashes = 0
			}
			b.WriteRune(r)
			continue
		}
	}
	b.WriteString(strings.Repeat(`\`, 2*slashes))
	b.WriteByte('"')
	return b.String()
}

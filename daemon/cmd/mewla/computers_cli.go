package main

import (
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"io"
	"os"
	"strings"
	"time"

	"github.com/daoleno/mewla/daemon/computers"
	"github.com/daoleno/mewla/daemon/control"
)

const computersUsage = `Usage: mewla computers <list|send|status|invite|add|remove> [flags]

Hand work to your other Mewla computers. Each computer's Brain does the work
with its own agents, files and logged-in apps; only messages cross.

  list                         Computers this one can reach, online or not
  send <computer> "<text>"     Send a message to that computer's Brain
       -kind request|reply|question|answer|cancel   (default request; reply with -reply-to)
       -reply-to <id>          The message this one answers
       -user-said "<words>"    The user's own words, quoted for the other Brain
  status [-id <id>]            Delivery of sent messages
  invite                       A one-time invite for another computer (15 minutes)
  add <invite> [-name NAME]    Pair with the computer that made the invite
  remove <computer>            Stop working with a computer

The app does invite and add in one tap: Settings → Working together.
`

func runComputersCommand(args []string, stderr io.Writer) error {
	if len(args) == 0 || isHelpArg(args[0]) {
		fmt.Fprint(stderr, computersUsage)
		return flag.ErrHelp
	}
	action := args[0]
	rest := args[1:]
	req := computers.Request{Action: action}
	// The computer (or invite) comes first so flags can follow it.
	if (action == "send" || action == "add" || action == "remove") && len(rest) > 0 && !strings.HasPrefix(rest[0], "-") {
		if action == "add" {
			req.Invite = rest[0]
		} else {
			req.Computer = rest[0]
		}
		rest = rest[1:]
	}
	fs := flag.NewFlagSet("mewla computers "+action, flag.ContinueOnError)
	fs.SetOutput(stderr)
	var stateDir string
	var outputJSON bool
	fs.StringVar(&stateDir, "state-dir", "", "state directory (default: ~/.mewla)")
	fs.BoolVar(&outputJSON, "json", false, "print JSON")
	switch action {
	case "list", "invite", "remove":
	case "send":
		fs.StringVar(&req.Kind, "kind", "", "request, reply, question, answer or cancel")
		fs.StringVar(&req.ReplyTo, "reply-to", "", "message id this answers")
		fs.StringVar(&req.UserSaid, "user-said", "", "the user's own words")
		fs.StringVar(&req.Text, "text", "", "message text")
	case "status":
		fs.StringVar(&req.ID, "id", "", "message id")
	case "add":
		fs.StringVar(&req.Name, "name", "", "name for the computer")
	default:
		return fmt.Errorf("unknown computers command %q; see mewla computers --help", action)
	}
	if err := fs.Parse(rest); err != nil {
		return err
	}
	if action == "send" && req.Text == "" {
		req.Text = strings.TrimSpace(strings.Join(fs.Args(), " "))
	} else if fs.NArg() != 0 {
		return fmt.Errorf("unexpected arguments: %s", strings.Join(fs.Args(), " "))
	}
	if action == "send" && req.Text == "" {
		return errors.New("nothing to send; give the message text")
	}
	cfg := cliConfig{stateDir: resolveCLIStateDir(stateDir)}
	resp, err := callControl(cfg, control.Request{Type: "computers", ComputersRequest: &req})
	if err != nil {
		return fmt.Errorf("Mewla is not running here; start it with mewla: %w", err)
	}
	if outputJSON || !resp.OK || resp.Computers == nil {
		return writeControlResponse(os.Stdout, resp, outputJSON)
	}
	printComputersResult(os.Stdout, action, *resp.Computers)
	return nil
}

func printComputersResult(w io.Writer, action string, result computers.Result) {
	switch action {
	case "list":
		if len(result.Computers) == 0 {
			fmt.Fprintln(w, "No other computers yet. In the app: Settings → Working together.")
		}
		for _, peer := range result.Computers {
			state := "offline"
			if peer.Online {
				state = "online"
			} else if peer.LastSeen != nil {
				state = "offline, last seen " + peer.LastSeen.Local().Format("Jan 2 15:04")
			}
			line := fmt.Sprintf("%s\t%s", peer.Name, state)
			if peer.Caps != nil {
				line += "\t" + peer.Caps.OS
				if len(peer.Caps.Agents) > 0 {
					line += "\tagents: " + strings.Join(peer.Caps.Agents, ", ")
				}
				if peer.Caps.BrowserUse {
					line += "\tagents can use its Chrome"
				}
			}
			if peer.Queued > 0 {
				line += fmt.Sprintf("\t%d waiting", peer.Queued)
			}
			if peer.LastError != "" && !peer.Online {
				line += "\t" + peer.LastError
			}
			fmt.Fprintln(w, line)
		}
	case "send":
		if result.Message != nil && result.Computer != nil {
			note := "sent"
			if !result.Computer.Online {
				note = "queued; " + result.Computer.Name + " is offline and gets it when it is back"
			}
			fmt.Fprintf(w, "%s\t%s\n", result.Message.ID, note)
		}
	case "status":
		messages := result.Messages
		if result.Message != nil {
			messages = append(messages, *result.Message)
		}
		for _, message := range messages {
			line := fmt.Sprintf("%s\t%s\t%s\t%s", message.ID, message.Computer, message.Kind, message.State)
			if message.Error != "" {
				line += "\t" + message.Error
			}
			fmt.Fprintln(w, line)
		}
	case "invite":
		fmt.Fprintln(w, result.Invite)
		if result.ExpiresAt != nil {
			fmt.Fprintf(os.Stderr, "On the other computer: mewla computers add <invite>. Expires %s.\n", result.ExpiresAt.Local().Format(time.Kitchen))
		}
	case "add", "remove":
		if result.Computer != nil {
			verb := "Added"
			if action == "remove" {
				verb = "Removed"
			}
			fmt.Fprintf(w, "%s %s\n", verb, result.Computer.Name)
		}
	default:
		_ = json.NewEncoder(w).Encode(result)
	}
}

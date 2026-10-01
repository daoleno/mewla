package work

import (
	"fmt"
	"regexp"
	"strconv"
	"strings"
)

var safeLaunchToken = regexp.MustCompile(`^[a-zA-Z0-9_./:@+=,-]+$`)

// CodexNativeResumeCommand leaves model and effort selection to the saved
// native Session. Executor defaults describe fresh launches, and may predate
// a user's in-session model switch. Connection and permission options remain.
func CodexNativeResumeCommand(command, sessionID string) (string, error) {
	args, _, _, err := codexCommandParts(command, false)
	if err != nil {
		return "", err
	}
	return WithProviderResumeToken(WorkerProviderCodex, joinLaunchTokens(args), sessionID)
}

func joinLaunchTokens(args []string) string {
	for i, v := range args {
		if !safeLaunchToken.MatchString(v) {
			args[i] = "'" + strings.ReplaceAll(v, "'", `'\''`) + "'"
		}
	}
	return strings.Join(args, " ")
}

// Normalize direct codex commands, including resume. Reject shell composition
// rather than pretending to enforce policy inside an arbitrary shell program.
func codexCommandParts(command string, autonomous bool) (out []string, model, reasoning string, err error) {
	args, ok := splitSupportedLaunchFields(command)
	opts, inspectable := inspectLaunchCommandOptions(command)
	if !ok || !inspectable || opts.executable != "codex" || opts.terminated {
		return nil, "", "", fmt.Errorf("delegated Codex requires a direct codex command (resume supported), without shell composition or -- terminator")
	}
	set := func(target *string, value, field string) error {
		if value == "" {
			return fmt.Errorf("Codex %s requires a value", field)
		}
		if *target != "" && *target != value {
			return fmt.Errorf("conflicting Codex %s values; specify one value", field)
		}
		*target = value
		return nil
	}
	for i := 0; i < len(args); i++ {
		arg := args[i]
		key, value, equals := strings.Cut(arg, "=")
		readValue := func() (string, error) {
			if equals {
				return value, nil
			}
			i++
			if i >= len(args) {
				return "", fmt.Errorf("Codex option %s requires a value", key)
			}
			return args[i], nil
		}
		switch key {
		case "--model", "-m":
			v, e := readValue()
			if e != nil {
				return nil, "", "", e
			}
			if e = set(&model, v, "model"); e != nil {
				return nil, "", "", e
			}
		case "--config", "-c":
			v, e := readValue()
			if e != nil {
				return nil, "", "", e
			}
			ck, cv, has := strings.Cut(v, "=")
			ck = strings.TrimSpace(ck)
			if has && (ck == "model" || ck == "model_reasoning_effort") {
				cv = strings.TrimSpace(cv)
				if uq, e := strconv.Unquote(cv); e == nil {
					cv = uq
				} else {
					cv = strings.Trim(cv, "'")
				}
				target := &model
				if ck == "model_reasoning_effort" {
					target = &reasoning
				}
				if e = set(target, cv, ck); e != nil {
					return nil, "", "", e
				}
			} else if autonomous && (ck == "approval_policy" || ck == "sandbox_mode" || ck == "permissions" || strings.HasPrefix(ck, "permissions.")) {
				return nil, "", "", fmt.Errorf("delegated Codex owns approval/sandbox policy; remove conflicting %s config", ck)
			} else {
				out = append(out, key, v)
			}
		case "--ask-for-approval", "-a", "--sandbox", "-s":
			v, e := readValue()
			if e != nil {
				return nil, "", "", e
			}
			if autonomous {
				want := "never"
				if key == "--sandbox" || key == "-s" {
					want = "danger-full-access"
				}
				if v != want {
					return nil, "", "", fmt.Errorf("%s conflicts with autonomous delegated execution; use a manual Session for interactive restrictions", key)
				}
			} else {
				out = append(out, key, v)
			}
		case "--full-auto":
			if autonomous {
				return nil, "", "", fmt.Errorf("--full-auto is restricted and conflicts with autonomous delegated execution")
			}
			out = append(out, arg)
		case CodexFullAuthorizationFlag:
			if equals {
				return nil, "", "", fmt.Errorf("Codex bypass option must be a boolean flag")
			}
			if !autonomous {
				out = append(out, arg)
			}
		default:
			if len(arg) > 2 && !strings.HasPrefix(arg, "--") && (strings.HasPrefix(arg, "-a") || strings.HasPrefix(arg, "-s") || strings.HasPrefix(arg, "-m") || strings.HasPrefix(arg, "-c")) {
				return nil, "", "", fmt.Errorf("use separated Codex short options for delegated launch")
			}
			out = append(out, arg)
		}
	}
	return
}
func codexWorkerCommand(command, model, reasoning string, autonomous bool) (string, error) {
	args, cm, cr, err := codexCommandParts(command, autonomous)
	if err != nil {
		return "", err
	}
	if model == "" {
		model = cm
	}
	if reasoning == "" {
		reasoning = cr
	}
	if strings.ContainsAny(model, "\x00\r\n") {
		return "", fmt.Errorf("invalid model id")
	}
	if reasoning != "" {
		switch reasoning {
		case "none", "minimal", "low", "medium", "high", "xhigh", "max", "ultra":
		default:
			return "", fmt.Errorf("unsupported Codex reasoning level %q", reasoning)
		}
	}
	if model != "" {
		args = append(args, "--model", model)
	}
	if reasoning != "" {
		args = append(args, "-c", `model_reasoning_effort="`+reasoning+`"`)
	}
	if autonomous {
		args = append(args, CodexFullAuthorizationFlag)
	}
	return joinLaunchTokens(args), nil
}

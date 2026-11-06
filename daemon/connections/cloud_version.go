package connections

import (
	"context"
	"encoding/json"
	"errors"
	"regexp"
	"strconv"
	"strings"
)

var cloudVersionNumber = regexp.MustCompile(`^\d+\.\d+\.\d+$`)

func cloudExecutableVersion(ctx context.Context, id, path string, run func(context.Context, string, []string, []string) (cloudOutput, error), env []string) (string, error) {
	args := []string{"--version"}
	if id == "gcp" {
		args = []string{"version", "--format=json"}
	}
	if id == "gws" {
		return gwsExecutableVersion(ctx, path, run, env)
	}
	out, err := run(ctx, path, args, env)
	if err != nil || out.ExitCode != 0 {
		return "", errors.New("Cloud CLI version check failed")
	}
	var version string
	if id == "aws" {
		match := awsVersion.FindString(strings.TrimSpace(out.Stdout))
		version = strings.TrimPrefix(match, "aws-cli/")
		if !validAWSVersion(out.Stdout) {
			return "", errors.New("AWS CLI 2.32 or later is required")
		}
	} else {
		var data map[string]string
		if json.Unmarshal([]byte(out.Stdout), &data) != nil {
			return "", errors.New("Invalid gcloud version")
		}
		version = data["Google Cloud SDK"]
		if !cloudVersionNumber.MatchString(version) {
			return "", errors.New("Invalid gcloud version")
		}
		major, _ := strconv.Atoi(strings.Split(version, ".")[0])
		if major < 400 {
			return "", errors.New("Google Cloud CLI 400 or later is required")
		}
	}
	return version, nil
}
func (gcloudCLI) validateExecutable(ctx context.Context, path string) error {
	_, err := cloudExecutableVersion(ctx, "gcp", path, runCloudProcess, gcloudCLI{}.environment())
	return err
}

// Version comparisons are numeric, never lexical (2.9 precedes 2.39).
func cloudVersionLess(a, b string) bool {
	left, right := strings.Split(a, "."), strings.Split(b, ".")
	if len(left) != 3 || len(right) != 3 {
		return false
	}
	for i := range left {
		x, _ := strconv.Atoi(left[i])
		y, _ := strconv.Atoi(right[i])
		if x != y {
			return x < y
		}
	}
	return false
}

var gwsVersionLine = regexp.MustCompile(`^gws (\d+\.\d+\.\d+)$`)

func gwsExecutableVersion(ctx context.Context, path string, run func(context.Context, string, []string, []string) (cloudOutput, error), env []string) (string, error) {
	out, err := run(ctx, path, []string{"--version"}, env)
	if err != nil || out.ExitCode != 0 {
		return "", errors.New("Cloud CLI version check failed")
	}
	m := gwsVersionLine.FindStringSubmatch(strings.TrimSpace(strings.SplitN(out.Stdout, "\n", 2)[0]))
	if m == nil {
		return "", errors.New("Invalid Google Workspace CLI version")
	}
	if cloudVersionLess(m[1], "0.22.0") {
		return "", errors.New("Google Workspace CLI 0.22 or later is required")
	}
	return m[1], nil
}
func (gwsCLI) validateExecutable(ctx context.Context, path string) error {
	_, err := gwsExecutableVersion(ctx, path, runCloudProcess, gwsCLI{}.environment())
	return err
}

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

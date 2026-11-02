package main

import (
	"reflect"
	"testing"
)

func TestCloudCLIArgsStayLiteral(t *testing.T) {
	q, err := cloudCLIRequest("aws", []string{"--profile", "my profile", "ec2", "describe-instances", "--filters", "Name=tag:Name,Values=$(touch /tmp/must-not-run)", "--mewla-authorize"})
	if err != nil || q.Profile != "my profile" || !q.Authorize || !reflect.DeepEqual(q.CLIArgs, []string{"ec2", "describe-instances", "--filters", "Name=tag:Name,Values=$(touch /tmp/must-not-run)"}) {
		t.Fatal("command argv changed")
	}
	if _, err = cloudCLIRequest("aws", []string{"--profile"}); err == nil {
		t.Fatal("missing profile accepted")
	}
}

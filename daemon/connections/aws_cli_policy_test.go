package connections

import "testing"

func TestAWSReadVerbsAcrossServices(t *testing.T) {
	for _, args := range [][]string{
		{"billing", "get-credits"}, {"freetier", "get-account-plan-state"}, {"invoicing", "list-invoice-summaries"},
		{"ce", "get-cost-and-usage"}, {"organizations", "describe-organization"}, {"new-service", "get-inventory"},
		{"new-service", "list-items"}, {"new-service", "describe-item"}, {"ssm", "get-parameters"}, {"ssm", "get-parameter", "--no-with-decryption"},
		{"s3", "ls"}, {"s3api", "head-object"}, {"lambda", "list-functions"},
	} {
		read, err := (awsCLI{}).classify(args)
		if err != nil || !read {
			t.Errorf("read %v: %v", args, err)
		}
	}
}
func TestAWSSecretReadersStayGated(t *testing.T) {
	for _, args := range [][]string{
		{"secretsmanager", "get-secret-value"}, {"secretsmanager", "batch-get-secret-value"},
		{"ssm", "get-parameter", "--with-decryption"}, {"ssm", "get-parameters-by-path", "--with-dec"},
		{"ssm", "get-parameter-history", "--cli-input-json", `{"WithDecryption":true}`},
		{"ssm", "get-parameters", "--cli-input-yaml=WithDecryption: true"},
		{"ssm", "get-parameters", "--cli-input-j", `{"WithDecryption":true}`},
		{"kms", "decrypt"}, {"sts", "get-session-token"}, {"sts", "get-federation-token"}, {"sts", "assume-role"}, {"iam", "create-access-key"},
		{"ecr", "get-authorization-token"}, {"ecr-public", "get-authorization-token"}, {"codeartifact", "get-authorization-token"},
		{"cognito-identity", "get-credentials-for-identity"}, {"redshift", "get-cluster-credentials"}, {"eks", "get-token"},
		{"cognito-idp", "describe-user-pool-client"}, {"ec2", "get-password-data"}, {"appsync", "list-api-keys"}, {"lambda", "get-function"}, {"s3api", "get-object"},
		{"billing", "put-credit-sharing-preference"}, {"freetier", "upgrade-account-plan"},
	} {
		read, err := (awsCLI{}).classify(args)
		if err != nil || read {
			t.Errorf("sensitive %v read=%v err=%v", args, read, err)
		}
	}
}

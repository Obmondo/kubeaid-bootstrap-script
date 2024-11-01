package utils

import (
	"bytes"
	"embed"
	"fmt"
	"log"
	"log/slog"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"text/template"
	"time"

	"github.com/Obmondo/kubeaid-bootstrap-script/constants"
	"github.com/go-sprout/sprout/sprigin"
)

func SetEnvs() {
	os.Setenv("KUBECONFIG", constants.OutputPathManagementClusterKubeconfig)

	// Cloud provider specific environment variables.
	switch {
	case constants.ParsedConfig.Cloud.AWS != nil:
		os.Setenv("AWS_REGION", constants.ParsedConfig.Cloud.AWS.Region)
		os.Setenv("AWS_ACCESS_KEY_ID", constants.ParsedConfig.Cloud.AWS.AccessKey)
		os.Setenv("AWS_SECRET_ACCESS_KEY", constants.ParsedConfig.Cloud.AWS.SecretKey)
		os.Setenv("AWS_SESSION_TOKEN", constants.ParsedConfig.Cloud.AWS.SessionToken)

		awsB64EncodedCredentials := strings.TrimSpace(
			strings.Split(
				ExecuteCommandOrDie("clusterawsadm bootstrap credentials encode-as-profile"),
				"WARNING: `encode-as-profile` should only be used for bootstrapping.",
			)[1],
		)
		os.Setenv(constants.EnvNameAWSB64EcodedCredentials, awsB64EncodedCredentials)

	default:
		Unreachable()
	}
}

func GetParentDirPath(filePath string) string {
	splitPosition := strings.LastIndex(filePath, "/")
	if splitPosition == -1 {
		return ""
	}
	return filePath[:splitPosition]
}

func ParseAndExecuteTemplate(embeddedFS *embed.FS, fileName string, values any) []byte {
	contentsAsBytes, err := embeddedFS.ReadFile(fileName)
	if err != nil {
		log.Fatalf("Failed getting template %s from embedded file-system : %v", fileName, err)
	}

	parsedTemplate, err := template.New(fileName).Funcs(sprigin.FuncMap()).Parse(string(contentsAsBytes))
	if err != nil {
		log.Fatalf("Failed parsing template %s : %v", fileName, err)
	}

	var executedTemplate bytes.Buffer
	if err = parsedTemplate.Execute(&executedTemplate, values); err != nil {
		log.Fatalf("Failed executing template %s : %v", fileName, err)
	}
	return executedTemplate.Bytes()
}

func executeCommand(command string, panicOnExecutionFailure bool) (string, error) {
	cmd := exec.Command("bash", "-c", command)

	slog.Info("Executing command", slog.String("command", cmd.String()))
	output, err := cmd.CombinedOutput()
	if err != nil && panicOnExecutionFailure {
		log.Fatalf("Command execution failed : %s\n %v", string(output), err)
	}
	slog.Debug("Command executed", slog.String("output", string(output)))
	return string(output), err
}

// Executes the given command. Doesn't panic and returns error (if occurred).
func ExecuteCommand(command string) (string, error) {
	return executeCommand(command, false)
}

// Executes the given command. Panics if the command execution fails.
func ExecuteCommandOrDie(command string) string {
	output, _ := executeCommand(command, true)
	return output
}

func Unreachable() { panic("unreachable") }

// Creates intermediate directories which don't exist for the given file path.
func CreateIntermediateDirectories(filePath string) {
	parentDir := filepath.Dir(filePath)
	if err := os.MkdirAll(parentDir, os.ModePerm); err != nil {
		log.Fatalf("Failed creating intermediate directories for file path %s : %v", filePath, err)
	}
}

// Creates a temp dir inside /tmp, where KubeAid Bootstrap Script will clone repos.
// Then sets the value of constants.TempDir as the temp dir path.
func InitTempDir() {
	name := fmt.Sprintf("kubeaid-bootstrap-script-%d", time.Now().Unix())
	path, err := os.MkdirTemp("/tmp", name)
	if err != nil {
		log.Fatalf("Failed creating temp dir : %v", err)
	}
	slog.Info("Created temp dir", slog.String("path", path))

	constants.TempDir = path
}

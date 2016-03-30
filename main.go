package main

import (
	"encoding/json"
	"flag"
	"fmt"
	"io/ioutil"
	"os"
	"path/filepath"

	"github.com/apex/log"
	"github.com/apex/log/handlers/text"
	"github.com/aws/aws-sdk-go/aws"
	"github.com/aws/aws-sdk-go/aws/session"
	"github.com/aws/aws-sdk-go/service/lambda"
	"github.com/chzyer/readline"

	// plugins
	_ "github.com/apex/apex/plugins/env"
	_ "github.com/apex/apex/plugins/golang"
	_ "github.com/apex/apex/plugins/hooks"
	_ "github.com/apex/apex/plugins/inference"
	_ "github.com/apex/apex/plugins/nodejs"
	_ "github.com/apex/apex/plugins/python"
	_ "github.com/apex/apex/plugins/shim"

	"github.com/apex/apex/colors"
	"github.com/apex/apex/function"
	"github.com/apex/apex/project"
)

func init() {
	log.SetHandler(text.Default)
}

// version of apex-shell.
var version = "1.0.0"

// prompt for shell.
var prompt = fmt.Sprintf("\033[%dmapex>\033[0m ", colors.Blue)

// source for the lambda function.
var functionSource = `
package main

import (
	"encoding/json"
	"os/exec"

	"github.com/apex/go-apex"
	"github.com/apex/log"
	"github.com/apex/log/handlers/logfmt"
)

type message struct {
	Command string
}

func init() {
	log.SetHandler(logfmt.Default)
}

func main() {
	log.Info("starting")

	apex.HandleFunc(func(event json.RawMessage, ctx *apex.Context) (interface{}, error) {
		var msg message

		if err := json.Unmarshal(event, &msg); err != nil {
			return nil, err
		}

		log.WithField("command", msg.Command).Info("exec")

		cmd := exec.Command("sh", "-c", msg.Command)
		out, err := cmd.CombinedOutput()
		return string(out), err
	})
}
`

// config for shell function.
var functionConfig = `{
  "description": "Apex generated REPL function",
  "runtime": "golang",
  "timeout": %d
}`

// event.
type event struct {
	Command string
}

// flags
var (
	chdir       = flag.String("chdir", "", "Working directory")
	logLevel    = flag.String("log-level", "info", "Log level")
	showVersion = flag.Bool("version", false, "Output version")
	timeout     = flag.Int("timeout", 60, "Timeout in seconds")
)

func main() {
	flag.Parse()

	if l, err := log.ParseLevel(*logLevel); err == nil {
		log.SetLevel(l)
	}

	if *showVersion {
		fmt.Println(version)
		return
	}

	if *chdir != "" {
		if err := os.Chdir(*chdir); err != nil {
			log.Fatalf("error: %s", err)
		}
	}

	if err := shell(); err != nil {
		log.Fatalf("error: %s", err)
	}
}

// shell sets up AWS credentials, deploys the function and starts the REPL.
func shell() error {
	config := aws.NewConfig()

	project := &project.Project{
		Service: lambda.New(session.New(config)),
		Log:     log.Log,
		Path:    ".",
	}

	if err := project.Open(); err != nil {
		return err
	}

	fn, err := deploy(project)
	if err != nil {
		return err
	}

	rl, err := readline.New(prompt)
	if err != nil {
		return err
	}
	defer rl.Close()

	for {
		line, err := rl.Readline()
		if err != nil {
			break
		}

		reply, _, err := fn.Invoke(event{line}, nil)
		if err != nil {
			return err
		}

		var s string

		if err := json.NewDecoder(reply).Decode(&s); err != nil {
			return err
		}

		os.Stdout.WriteString(s)
	}

	return fn.Delete()
}

// deploy shell function.
func deploy(project *project.Project) (*function.Function, error) {
	path := filepath.Join(os.TempDir(), "__apex_repl__")

	if err := os.MkdirAll(path, 0755); err != nil {
		return nil, err
	}

	if err := ioutil.WriteFile(filepath.Join(path, "main.go"), []byte(functionSource), 0755); err != nil {
		return nil, err
	}

	config := fmt.Sprintf(functionConfig, *timeout)
	if err := ioutil.WriteFile(filepath.Join(path, "function.json"), []byte(config), 0755); err != nil {
		return nil, err
	}

	fn, err := project.LoadFunctionByPath("repl", path)
	if err != nil {
		return nil, err
	}

	if err := fn.Deploy(); err != nil {
		return nil, err
	}

	return fn, nil
}

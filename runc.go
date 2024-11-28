package acbrun

import (
	"bytes"
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"strings"
)

type RuncState struct {
	Status string `json:"status"`
}

func IsContainerRunning(name string) (bool, error) {
	cmd := exec.Command("runc", "state", name)
	var outb, errb bytes.Buffer
	cmd.Stdout = &outb
	cmd.Stderr = &errb
	err := cmd.Run()
	stdoutStr := outb.String()
	stderrStr := errb.String()
	if err != nil {
		if strings.Contains(stderrStr, "\"container does not exist\"") {
			return false, nil
		}
		fmt.Fprintf(os.Stderr, "runc: %s\n", stderrStr)
		return false, err
	} else {
		var runcState RuncState
		err = json.Unmarshal([]byte(stdoutStr), &runcState)
		if err != nil {
			return false, err
		}
		if runcState.Status != "running" {
			return false, nil
		}
		return true, nil
	}
}
